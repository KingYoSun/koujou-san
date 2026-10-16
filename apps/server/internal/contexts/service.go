package contexts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sort"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/jackc/pgx/v5"
)

type query interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}
type Service struct {
	Store  *persistence.Store
	Access *access.Service
	Limits Limits
}

func New(store *persistence.Store, a *access.Service, limits Limits) *Service {
	return &Service{store, a, limits}
}

type Record struct {
	Ref      Ref             `json:"ref"`
	Value    json.RawMessage `json:"value"`
	Position string          `json:"-"`
}
type Write struct {
	apicontract.Mutation
	Value json.RawMessage `json:"value"`
}

func resource(i Identity) string {
	if unitLevel(i.Kind) > 0 {
		return "context:" + i.ID
	}
	return i.Kind + ":" + i.ID
}
func (s *Service) check(ctx context.Context, q query, p access.Principal, w, target, action string) error {
	var err error
	if tx, ok := q.(pgx.Tx); ok {
		err = s.Access.CheckTx(ctx, tx, p, w, target, action)
	} else {
		err = s.Access.Check(ctx, p, w, target, action)
	}
	if errors.Is(err, access.ErrForbidden) {
		return invalid("forbidden", nil)
	}
	return err
}
func (s *Service) read(ctx context.Context, q query, p access.Principal, w string, r Ref) (Record, error) {
	if r.Kind == "" || r.ID == "" || r.Revision == "" {
		return Record{}, invalid("invalid_reference", &r)
	}
	if err := s.check(ctx, q, p, w, "", "read"); err != nil {
		return Record{}, err
	}
	var actualKind, revision, position string
	var value json.RawMessage
	id := resource(r.Identity())
	err := q.QueryRow(ctx, `SELECT r.kind,r.revision,r.payload,s.position FROM context_records r JOIN shared_streams s USING(workspace_id) WHERE r.workspace_id=$1 AND r.resource_id=$2 AND r.revision=CASE WHEN $3='head' THEN (SELECT version FROM shared_versions WHERE workspace_id=$1 AND resource_id=$2) ELSE $3 END`, w, id, r.Revision).Scan(&actualKind, &revision, &value, &position)
	if errors.Is(err, pgx.ErrNoRows) {
		return Record{}, invalid("missing_reference", &r)
	}
	if err != nil {
		return Record{}, err
	}
	if err = s.check(ctx, q, p, w, id, "get"); err != nil {
		return Record{}, invalidWithRef(err, r)
	}
	var versionTx pgx.Tx
	if tx, ok := q.(pgx.Tx); ok {
		versionTx = tx
	}
	if err = s.Access.CheckVersion(ctx, versionTx, p, w, id, revision); err != nil {
		return Record{}, invalidWithRef(err, r)
	}
	if actualKind != r.Kind {
		return Record{}, invalid("invalid_type", &r)
	}
	return Record{Ref: Ref{actualKind, r.ID, revision}, Value: value, Position: position}, nil
}
func invalidWithRef(err error, r Ref) error {
	var f *failure
	if errors.As(err, &f) {
		return invalid(f.gap.Code, &r)
	}
	return err
}
func (s *Service) unit(ctx context.Context, q query, p access.Principal, w string, r Ref) (Unit, error) {
	rec, err := s.read(ctx, q, p, w, r)
	if err != nil {
		return Unit{}, err
	}
	var u Unit
	err = json.Unmarshal(rec.Value, &u)
	return u, err
}
func (s *Service) Read(ctx context.Context, p access.Principal, w string, r Ref) (Record, error) {
	return s.readChecked(ctx, s.Store.Database(), p, w, r)
}

// ReadTx reuses current Manifest/dependency authorization in the owning Run's
// acceptance transaction; it does not create another shared mutation.
func (s *Service) ReadTx(ctx context.Context, tx pgx.Tx, p access.Principal, w string, r Ref) (Record, error) {
	return s.readChecked(ctx, tx, p, w, r)
}
func (s *Service) readChecked(ctx context.Context, q query, p access.Principal, w string, r Ref) (Record, error) {
	rec, err := s.read(ctx, q, p, w, r)
	if err != nil {
		return rec, err
	}
	if r.Kind == "manifest" {
		var m Manifest
		if err = json.Unmarshal(rec.Value, &m); err != nil {
			return Record{}, err
		}
		if err = s.checkManifest(ctx, q, p, w, m); err != nil {
			return Record{}, err
		}
	}
	if r.Kind == "addition" {
		var a Addition
		if err = json.Unmarshal(rec.Value, &a); err != nil {
			return Record{}, err
		}
		if _, err = s.readChecked(ctx, q, p, w, a.Manifest); err != nil {
			return Record{}, err
		}
	}
	if p.OperationID != "" && unitLevel(r.Kind) > 1 {
		resolved := s.resolve(ctx, q, p, w, ResolveRequest{Selection: Selection{Mode: "select", Include: []Ref{rec.Ref}}})
		if !resolved.Ready {
			return Record{}, invalid("forbidden", nil)
		}
	}
	return rec, nil
}
func (s *Service) checkManifest(ctx context.Context, q query, p access.Principal, w string, m Manifest) error {
	if m.Baseline != nil {
		if _, err := s.read(ctx, q, p, w, *m.Baseline); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for _, v := range m.Placements {
		if seen[v.Ref.key()] {
			continue
		}
		seen[v.Ref.key()] = true
		if _, err := s.read(ctx, q, p, w, v.Ref); err != nil {
			return err
		}
	}
	return nil
}
func decodeValue(raw []byte, v any) error {
	if !json.Valid(raw) {
		return invalid("invalid_value", nil)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		return invalid("invalid_value", nil)
	}
	return nil
}
func (s *Service) authorizeWrite(ctx context.Context, q query, p access.Principal, w string, i Identity) error {
	if p.OperationID != "" && unitLevel(i.Kind) == 0 {
		return invalid("forbidden", nil)
	}
	var exists bool
	if err := q.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM shared_versions WHERE workspace_id=$1 AND resource_id=$2)", w, resource(i)).Scan(&exists); err != nil {
		return err
	}
	target, action := "", "register"
	if exists {
		target, action = resource(i), "update"
	}
	return s.check(ctx, q, p, w, target, action)
}
func (s *Service) Put(ctx context.Context, p access.Principal, w string, i Identity, request Write) (apicontract.Response[apicontract.Reference], error) {
	if i.ID == "" {
		return apicontract.Response[apicontract.Reference]{}, invalid("invalid_reference", nil)
	}
	if err := s.authorizeWrite(ctx, s.Store.Database(), p, w, i); err != nil {
		return apicontract.Response[apicontract.Reference]{}, err
	}
	scope := persistence.Scope{ActorID: "user:" + p.UserID, Grant: apicontract.Grant{WorkspaceID: w, CanRead: true, CanUpdate: true}, Recheck: func(tx pgx.Tx) *apicontract.Problem {
		if err := s.authorizeWrite(ctx, tx, p, w, i); err != nil {
			code := "unavailable"
			var f *failure
			if errors.As(err, &f) {
				code = f.gap.Code
			}
			return &apicontract.Problem{Code: code}
		}
		return nil
	}}
	return s.Store.Apply(ctx, scope, persistence.Command{WorkspaceID: w, ResourceID: resource(i), Kind: "context-" + i.Kind, Mutation: request.Mutation, Payload: request.Value}, func(tx pgx.Tx, version apicontract.Reference) (json.RawMessage, error) {
		value, err := s.prepare(ctx, tx, p, w, i, request.Value, request.OperationID)
		if err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, "INSERT INTO context_records SELECT $1,$2,$3,$4,$5,sequence+1 FROM shared_streams WHERE workspace_id=$1", w, resource(i), i.Kind, version.Version, value); err != nil {
			return nil, err
		}
		// Existing resource policy belongs to access. A content edit must not replace
		// restricted grants with the default shared policy.
		if request.ExpectedVersion == "0" {
			if err = s.Access.PutResource(ctx, tx, access.Resource{WorkspaceID: w, ID: resource(i)}, nil); err != nil {
				return nil, err
			}
		}
		return canonical(Record{Ref: Ref{i.Kind, i.ID, version.Version}, Value: value}), nil
	})
}
func (s *Service) prepare(ctx context.Context, tx pgx.Tx, p access.Principal, w string, i Identity, raw json.RawMessage, operation string) (json.RawMessage, error) {
	old, oldErr := s.read(ctx, tx, p, w, Ref{i.Kind, i.ID, "head"})
	var f *failure
	if oldErr != nil && (!errors.As(oldErr, &f) || f.gap.Code != "missing_reference") {
		return nil, oldErr
	}
	switch {
	case unitLevel(i.Kind) > 0:
		var u Unit
		if err := decodeValue(raw, &u); err != nil {
			return nil, err
		}
		if u.Kind != i.Kind {
			return nil, invalid("invalid_type", nil)
		}
		if p.OperationID != "" {
			var previous Unit
			if oldErr != nil || json.Unmarshal(old.Value, &previous) != nil || !slices.Equal(previous.Tags, u.Tags) {
				return nil, invalid("forbidden", nil)
			}
		}
		g := graph{ctx, func(ctx context.Context, r Ref) (Unit, error) { return s.unit(ctx, tx, p, w, r) }, map[string]Unit{}, map[string]bool{}}
		if err := validateUnit(u); err != nil {
			return nil, err
		}
		for _, r := range u.Children {
			if r.ID == i.ID {
				return nil, invalid("cycle", &r)
			}
			if err := g.load(r); err != nil {
				return nil, err
			}
		}
		return canonical(u), nil
	case i.Kind == "scope":
		var v Scope
		if err := decodeValue(raw, &v); err != nil {
			return nil, err
		}
		if scopeLevel(v.Kind) == 0 || v.Name == "" {
			return nil, invalid("invalid_scope", nil)
		}
		if oldErr == nil {
			var previous Scope
			_ = json.Unmarshal(old.Value, &previous)
			if previous.Kind != v.Kind {
				return nil, invalid("invalid_type", nil)
			}
		}
		if v.Parent != nil {
			if v.Parent.ID == i.ID || scopeLevel(v.Parent.Kind) != scopeLevel(v.Kind)-1 {
				return nil, invalid("invalid_scope", nil)
			}
			parent, err := s.read(ctx, tx, p, w, Ref{"scope", v.Parent.ID, "head"})
			if err != nil {
				return nil, err
			}
			var scope Scope
			_ = json.Unmarshal(parent.Value, &scope)
			if scope.Kind != v.Parent.Kind {
				return nil, invalid("invalid_type", nil)
			}
		}
		return canonical(v), nil
	case i.Kind == "membership":
		var v Membership
		if err := decodeValue(raw, &v); err != nil {
			return nil, err
		}
		if scopeLevel(v.Scope.Kind) == 0 || unitLevel(v.Context.Kind) == 0 {
			return nil, invalid("invalid_membership", nil)
		}
		scope, err := s.read(ctx, tx, p, w, Ref{"scope", v.Scope.ID, "head"})
		if err != nil {
			return nil, err
		}
		var actual Scope
		_ = json.Unmarshal(scope.Value, &actual)
		if actual.Kind != v.Scope.Kind {
			return nil, invalid("invalid_type", nil)
		}
		if _, err = s.read(ctx, tx, p, w, Ref{v.Context.Kind, v.Context.ID, "head"}); err != nil {
			return nil, err
		}
		if err = s.check(ctx, tx, p, w, resource(v.Context), "update"); err != nil {
			return nil, err
		}
		if err = s.check(ctx, tx, p, w, resource(Identity{"scope", v.Scope.ID}), "update"); err != nil {
			return nil, err
		}
		if oldErr == nil {
			var previous Membership
			_ = json.Unmarshal(old.Value, &previous)
			if previous.Scope != v.Scope || previous.Context != v.Context {
				return nil, invalid("membership_identity_changed", nil)
			}
		}
		return canonical(v), nil
	case i.Kind == "baseline":
		var b Baseline
		if err := decodeValue(raw, &b); err != nil {
			return nil, err
		}
		b.Previous = nil
		b.Changed = nil
		b.AffectedRunIDs = nil
		for _, r := range b.Contexts {
			if !validRef(r) || unitLevel(r.Kind) == 0 {
				return nil, invalid("invalid_type", &r)
			}
			if _, err := s.read(ctx, tx, p, w, r); err != nil {
				return nil, err
			}
		}
		for _, r := range b.Memberships {
			if !validRef(r) || r.Kind != "membership" {
				return nil, invalid("invalid_type", &r)
			}
			if _, err := s.read(ctx, tx, p, w, r); err != nil {
				return nil, err
			}
		}
		for _, input := range b.Inputs {
			if input.RunID == "" {
				return nil, invalid("invalid_run", nil)
			}
		}
		if oldErr == nil {
			var previous Baseline
			_ = json.Unmarshal(old.Value, &previous)
			b.Previous = &old.Ref
			b.Changed, b.AffectedRunIDs = baselineImpact(previous, b)
		}
		return canonical(b), nil
	case i.Kind == "manifest":
		if oldErr == nil {
			return nil, invalid("immutable_manifest", nil)
		}
		var request ResolveRequest
		if err := decodeValue(raw, &request); err != nil {
			return nil, err
		}
		if request.RunID == "" {
			return nil, invalid("invalid_run", nil)
		}
		resolved := s.resolve(ctx, tx, p, w, request)
		if !resolved.Ready {
			return nil, &failure{resolved.Missing[0]}
		}
		resolved.Manifest.OperationID = operation
		resolved.Manifest.AdoptedBy = p.UserID
		return canonical(resolved.Manifest), nil
	case i.Kind == "addition":
		if oldErr == nil {
			return nil, invalid("immutable_addition", nil)
		}
		var a Addition
		if err := decodeValue(raw, &a); err != nil {
			return nil, err
		}
		if !validRef(a.Manifest) || a.Manifest.Kind != "manifest" || a.PauseID == "" || a.ResponseID == "" {
			return nil, invalid("invalid_addition", nil)
		}
		rec, err := s.read(ctx, tx, p, w, a.Manifest)
		if err != nil {
			return nil, err
		}
		var m Manifest
		_ = json.Unmarshal(rec.Value, &m)
		if err = s.checkManifest(ctx, tx, p, w, m); err != nil {
			return nil, err
		}
		if _, err = directValue(a.Context); err != nil {
			return nil, err
		}
		a.ReadStatus = "unobserved"
		return canonical(a), nil
	}
	return nil, invalid("invalid_type", nil)
}
func baselineImpact(old, next Baseline) ([]Identity, []string) {
	before := map[Identity]string{}
	after := map[Identity]string{}
	for _, r := range append(append([]Ref{}, old.Contexts...), old.Memberships...) {
		before[r.Identity()] = r.Revision
	}
	for _, r := range append(append([]Ref{}, next.Contexts...), next.Memberships...) {
		after[r.Identity()] = r.Revision
	}
	changed := map[Identity]bool{}
	for id, v := range before {
		if after[id] != v {
			changed[id] = true
		}
	}
	for id, v := range after {
		if before[id] != v {
			changed[id] = true
		}
	}
	inputs := map[string]Selection{}
	for _, in := range old.Inputs {
		inputs[in.RunID] = in.Selection
	}
	affected := map[string]bool{}
	for _, in := range append(append([]PlannedInput{}, old.Inputs...), next.Inputs...) {
		if oldSelection, ok := inputs[in.RunID]; !ok || !bytes.Equal(canonical(oldSelection), canonical(in.Selection)) {
			affected[in.RunID] = true
		}
		for _, r := range append(append([]Ref{}, in.Selection.Include...), in.Selection.Exclude...) {
			if changed[r.Identity()] {
				affected[in.RunID] = true
			}
		}
		for id := range changed {
			if id.Kind == "membership" {
				affected[in.RunID] = true
			}
		}
	}
	list := []Identity{}
	for id := range changed {
		list = append(list, id)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Kind+":"+list[i].ID < list[j].Kind+":"+list[j].ID })
	runs := []string{}
	for id := range affected {
		runs = append(runs, id)
	}
	sort.Strings(runs)
	return list, runs
}
func (s *Service) resolve(ctx context.Context, q query, p access.Principal, w string, request ResolveRequest) Resolution {
	if err := s.check(ctx, q, p, w, "", "read"); err != nil {
		return unresolved(err)
	}
	var b Baseline
	if request.Baseline != nil {
		if request.Baseline.Kind != "baseline" || request.Baseline.Revision == "head" {
			return unresolved(invalid("fixed_baseline_required", request.Baseline))
		}
		rec, err := s.read(ctx, q, p, w, *request.Baseline)
		if err != nil {
			return unresolved(err)
		}
		if err = json.Unmarshal(rec.Value, &b); err != nil {
			return unresolved(err)
		}
		allowed := map[string]bool{}
		g := graph{ctx, func(ctx context.Context, r Ref) (Unit, error) { return s.unit(ctx, q, p, w, r) }, map[string]Unit{}, map[string]bool{}}
		// Candidate identities are fixed by the baseline. Load only candidate roots
		// needed to locate selected descendants, never inject their contents by default.
		for _, r := range b.Contexts {
			allowed[r.key()] = true
		}
		for _, r := range append(append([]Ref{}, request.Selection.Include...), request.Selection.Exclude...) {
			if allowed[r.key()] {
				continue
			}
			found := false
			for _, candidate := range b.Contexts {
				if g.load(candidate) == nil {
					g.descendants(candidate, func(ref Ref) { allowed[ref.key()] = true })
					if allowed[r.key()] {
						found = true
						break
					}
				}
			}
			if !found {
				return unresolved(invalid("outside_baseline", &r))
			}
		}
	}
	result := Resolve(ctx, func(ctx context.Context, r Ref) (Unit, error) {
		if r.Revision == "head" {
			return Unit{}, invalid("fixed_revision_required", &r)
		}
		return s.unit(ctx, q, p, w, r)
	}, w, request, s.Limits)
	if result.Manifest != nil {
		result.Manifest.Memberships = b.Memberships
	}
	return result
}
func (s *Service) Preview(ctx context.Context, p access.Principal, w string, r ResolveRequest) Resolution {
	return s.resolve(ctx, s.Store.Database(), p, w, r)
}
