package usage

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"strings"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/jackc/pgx/v5"
)

type Service struct {
	Store  *persistence.Store
	Access *access.Service
	key    []byte
}

func New(s *persistence.Store, a *access.Service) *Service {
	return &Service{s, a, []byte(rand.Text())}
}

type query interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}
type Write struct {
	apicontract.Mutation
	Value json.RawMessage `json:"value"`
}

func decode(raw []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		return ErrInvalid
	}
	return nil
}
func ObservationID(o Observation) string {
	key := o.Event
	if o.Mode == "cumulative" {
		key = o.Series
	}
	h := sha256.Sum256([]byte(o.Source + "\x00" + o.Mode + "\x00" + key))
	return "usage:" + hex.EncodeToString(h[:])
}
func kindOf(id string) string { k, _, _ := strings.Cut(id, ":"); return k }
func (s *Service) authorizeWrite(ctx context.Context, tx pgx.Tx, p access.Principal, w, id string) error {
	var q query = s.Store.Database()
	if tx != nil {
		q = tx
	}
	var exists bool
	if err := q.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM shared_versions WHERE workspace_id=$1 AND resource_id=$2)", w, id).Scan(&exists); err != nil {
		return err
	}
	target, action := "", "register"
	if exists {
		target, action = id, "update"
	}
	if kindOf(id) == "price" && p.OperationID != "" {
		action = "price_write"
	} else if p.OperationID != "" {
		return access.ErrForbidden
	}
	var err error
	if tx == nil {
		err = s.Access.Check(ctx, p, w, target, action)
	} else {
		err = s.Access.CheckTx(ctx, tx, p, w, target, action)
	}
	return err
}
func (s *Service) Put(ctx context.Context, p access.Principal, w, id string, req Write, within ...pgx.Tx) (apicontract.Response[apicontract.Reference], error) {
	return s.put(ctx, p, w, id, req, nil, within...)
}

// OwnerWrite adds the owning domain's current authorization and fixed input
// dependencies to the same transaction as the ordinary usage mutation.
type OwnerWrite struct {
	Actor        string
	Dependencies []Ref
	Check        func(context.Context, pgx.Tx) error
	// Reserved permits completing an already-recorded effect after its actor's
	// authority changes. Check must validate the immutable owning reservation;
	// dependencies remain attached for all current read/export authorization.
	Reserved bool
}

func (s *Service) PutFromOwner(ctx context.Context, p access.Principal, w, id string, req Write, guard OwnerWrite, within ...pgx.Tx) (apicontract.Response[apicontract.Reference], error) {
	if kindOf(id) != "usage" || guard.Check == nil || guard.Actor == "" || (guard.Reserved && req.ExpectedVersion == "0") {
		return apicontract.Response[apicontract.Reference]{}, access.ErrForbidden
	}
	var tx pgx.Tx
	if len(within) > 0 {
		tx = within[0]
	}
	if err := guard.Check(ctx, tx); err != nil {
		return apicontract.Response[apicontract.Reference]{}, err
	}
	return s.put(ctx, p, w, id, req, &guard, within...)
}

func (s *Service) put(ctx context.Context, p access.Principal, w, id string, req Write, guard *OwnerWrite, within ...pgx.Tx) (apicontract.Response[apicontract.Reference], error) {
	empty := apicontract.Response[apicontract.Reference]{}
	if id == "" || strings.TrimPrefix(id, kindOf(id)+":") == "" {
		return empty, ErrInvalid
	}
	var tx pgx.Tx
	if len(within) > 0 {
		tx = within[0]
	}
	reserved := guard != nil && guard.Reserved
	if !reserved {
		if err := s.authorizeWrite(ctx, tx, p, w, id); err != nil {
			return empty, err
		}
	}
	kind := kindOf(id)
	var value any
	switch kind {
	case "usage":
		var v Observation
		if decode(req.Value, &v) != nil || v.validate() != nil || id != ObservationID(v) {
			return empty, ErrInvalid
		}
		if strings.HasPrefix(v.Source, "server-model:") && guard == nil {
			return empty, access.ErrForbidden
		}
		value = v
	case "price":
		var v Price
		if decode(req.Value, &v) != nil || v.validate() != nil {
			return empty, ErrInvalid
		}
		value = v
	case "evaluation":
		var v Evaluation
		if decode(req.Value, &v) != nil || req.ExpectedVersion != "0" {
			return empty, ErrInvalid
		}
		value = v
	default:
		return empty, ErrInvalid
	}
	raw, _ := json.Marshal(value)
	actor := "user:" + p.UserID
	if guard != nil {
		actor = guard.Actor
	}
	scope := persistence.Scope{ActorID: actor, Grant: apicontract.Grant{WorkspaceID: w, CanRead: true, CanUpdate: true}, Recheck: func(tx pgx.Tx) *apicontract.Problem {
		if guard != nil {
			if err := guard.Check(ctx, tx); err != nil {
				return &apicontract.Problem{Code: "forbidden"}
			}
		}
		if !reserved {
			if err := s.authorizeWrite(ctx, tx, p, w, id); err != nil {
				return &apicontract.Problem{Code: "forbidden"}
			}
			if err := s.Access.CheckVersion(ctx, tx, p, w, id, req.ExpectedVersion); err != nil {
				return &apicontract.Problem{Code: "forbidden"}
			}
		}
		return nil
	}}
	return s.Store.Apply(ctx, scope, persistence.Command{WorkspaceID: w, ResourceID: id, Kind: "cost-" + kind, Mutation: req.Mutation, Payload: raw}, func(tx pgx.Tx, ref apicontract.Reference) (json.RawMessage, error) {
		var seq int64
		if err := tx.QueryRow(ctx, "SELECT sequence+1 FROM shared_streams WHERE workspace_id=$1", w).Scan(&seq); err != nil {
			return nil, err
		}
		deps := []Ref{}
		if guard != nil {
			deps = append(deps, guard.Dependencies...)
		}
		group := ""
		switch v := value.(type) {
		case Observation:
			group = v.Group
			if reserved {
				var previousDependencies []byte
				if err := tx.QueryRow(ctx, `SELECT dependencies FROM usage_records WHERE workspace_id=$1 AND id=$2 ORDER BY position DESC LIMIT 1`, w, id).Scan(&previousDependencies); err != nil {
					return nil, err
				}
				var previous []Ref
				if err := json.Unmarshal(previousDependencies, &previous); err != nil {
					return nil, err
				}
				deps = uniqueRefs(append(deps, previous...))
			}
			var previous []byte
			err := tx.QueryRow(ctx, `SELECT payload FROM usage_records WHERE workspace_id=$1 AND id=$2 ORDER BY position DESC LIMIT 1`, w, id).Scan(&previous)
			old := v
			if err == nil {
				if json.Unmarshal(previous, &old) != nil || old.Group != v.Group || old.Source != v.Source || old.Series != v.Series || old.Mode != v.Mode || (v.Mode == "cumulative" && (v.Sequence < old.Sequence || (v.Sequence == old.Sequence && v.Event != old.Event))) {
					return nil, ErrInvalid
				}
				if guard != nil && (old.Attribution.Kind != v.Attribution.Kind || old.Attribution.ID != v.Attribution.ID || old.Attribution.Resource != v.Attribution.Resource || old.Measurement == nil || old.Measurement.Generation != v.Measurement.Generation) {
					return nil, access.ErrForbidden
				}
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return nil, err
			}
			tag, eventErr := tx.Exec(ctx, `INSERT INTO usage_events VALUES($1,$2,$3,$4,$5) ON CONFLICT(workspace_id,source,event_id) DO UPDATE SET sequence=excluded.sequence WHERE usage_events.resource_id=excluded.resource_id AND usage_events.sequence=excluded.sequence`, w, v.Source, v.Event, id, v.Sequence)
			if eventErr != nil {
				return nil, eventErr
			}
			if tag.RowsAffected() != 1 {
				return nil, ErrInvalid
			}
			var eventID string
			if err = tx.QueryRow(ctx, "SELECT resource_id FROM usage_events WHERE workspace_id=$1 AND source=$2 AND event_id=$3", w, v.Source, v.Event).Scan(&eventID); err != nil || eventID != id {
				return nil, ErrInvalid
			}
			if v.Attribution.Resource != "" {
				deps = append(deps, Ref{ID: v.Attribution.Resource})
			}
			for _, scope := range v.Attribution.Scopes {
				deps = append(deps, Ref{ID: "scope:" + scope})
			}
			for _, r := range deps {
				if reserved {
					continue
				}
				if err = s.Access.CheckTx(ctx, tx, p, w, r.ID, "get"); err != nil {
					return nil, err
				}
			}
			from, until := old.At, v.At
			if from.After(until) {
				from, until = until, from
			}
			if _, err = tx.Exec(ctx, `INSERT INTO usage_group_updates VALUES($1,$2,$3,$4,$5,$6,$7)`, w, group, seq, from, until, id, ref.Version); err != nil {
				return nil, err
			}
		case Evaluation:
			group = v.Group
			prepared, refs, err := s.evaluate(ctx, tx, p, w, v)
			if err != nil {
				return nil, err
			}
			raw, _ = json.Marshal(prepared)
			deps = refs
		}
		depRaw, _ := json.Marshal(deps)
		if _, err := tx.Exec(ctx, `INSERT INTO usage_records VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, w, id, ref.Version, kind, seq, group, raw, depRaw); err != nil {
			return nil, err
		}
		if req.ExpectedVersion == "0" {
			if err := s.Access.PutResource(ctx, tx, access.Resource{WorkspaceID: w, ID: id}, nil); err != nil {
				return nil, err
			}
		}
		if kind == "usage" || kind == "evaluation" {
			c, err := s.group(ctx, tx, w, group)
			if err != nil {
				return nil, err
			}
			refs := append([]Ref{}, c.Refs...)
			for _, r := range c.Refs {
				var rd []byte
				if err = tx.QueryRow(ctx, "SELECT dependencies FROM usage_records WHERE workspace_id=$1 AND id=$2 AND version=$3", w, r.ID, r.Version).Scan(&rd); err != nil {
					return nil, err
				}
				var ds []Ref
				_ = json.Unmarshal(rd, &ds)
				refs = append(refs, ds...)
			}
			key, state, currency, amount, payload := group+":usage:", c.State, "", (*string)(nil), raw
			if kind == "usage" {
				payload, _ = json.Marshal(c.Observation)
			} else {
				var e Evaluation
				_ = json.Unmarshal(raw, &e)
				key = group + ":" + e.Kind + ":" + e.Currency
				state, currency, amount = e.State, e.Currency, e.Amount
				refs = append(refs, deps...)
				refs = append(refs, Ref{ID: id, Version: ref.Version})
			}
			parts, _, partErr := disjoint(c.Quantities)
			if partErr != nil {
				parts = map[string]Quantity{}
			}
			quantityRaw, _ := json.Marshal(parts)
			refsRaw, _ := json.Marshal(uniqueRefs(refs))
			model := ""
			if c.ActualModel != nil {
				model = *c.ActualModel
			}
			scopes := c.Attribution.Scopes
			if scopes == nil {
				scopes = []string{}
			}
			_, err = tx.Exec(ctx, `INSERT INTO usage_entries VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::numeric,$14,$15,$16,$17)`, w, key, seq, group, strings.TrimPrefix(strings.TrimSuffix(key, ":"+currency), group+":"), currency, c.At, c.Attribution.Kind, c.Attribution.ID, c.Attribution.Workflow, scopes, model, amount, state, refsRaw, payload, quantityRaw)
			if err != nil {
				return nil, err
			}
		}
		return json.Marshal(Record{Ref: Ref{ID: id, Version: ref.Version}, Kind: kind, Position: seq, Value: raw})
	}, within...)
}

func (s *Service) group(ctx context.Context, q query, w, g string) (consumption, error) {
	rows, err := q.Query(ctx, `SELECT DISTINCT ON(id) id,version,payload FROM usage_records WHERE workspace_id=$1 AND coverage_group=$2 AND kind='usage' ORDER BY id,position DESC`, w, g)
	if err != nil {
		return consumption{}, err
	}
	records := []Record{}
	for rows.Next() {
		var r Record
		if err = rows.Scan(&r.ID, &r.Version, &r.Value); err != nil {
			rows.Close()
			return consumption{}, err
		}
		records = append(records, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return consumption{}, err
	}
	if len(records) == 0 {
		return consumption{}, ErrInvalid
	}
	return combine(records)
}
func (s *Service) read(ctx context.Context, q query, p access.Principal, w string, ref Ref) (Record, error) {
	var out Record
	var deps []byte
	err := q.QueryRow(ctx, `SELECT id,version,kind,position,payload,dependencies FROM usage_records WHERE workspace_id=$1 AND id=$2 AND ($3='head' OR version=$3) ORDER BY position DESC LIMIT 1`, w, ref.ID, ref.Version).Scan(&out.ID, &out.Version, &out.Kind, &out.Position, &out.Value, &deps)
	if err != nil {
		return out, access.ErrForbidden
	}
	var refs []Ref
	_ = json.Unmarshal(deps, &refs)
	refs = append(refs, out.Ref)
	for _, r := range refs {
		var tx pgx.Tx
		if t, ok := q.(pgx.Tx); ok {
			tx = t
		}
		if tx != nil {
			err = s.Access.CheckTx(ctx, tx, p, w, r.ID, "get")
		} else {
			err = s.Access.Check(ctx, p, w, r.ID, "get")
		}
		if err != nil {
			return Record{}, err
		}
		if err = s.Access.CheckVersion(ctx, tx, p, w, r.ID, r.Version); err != nil {
			return Record{}, err
		}
	}
	return out, nil
}
func (s *Service) Read(ctx context.Context, p access.Principal, w string, ref Ref) (Record, error) {
	return s.read(ctx, s.Store.Database(), p, w, ref)
}
func (s *Service) evaluate(ctx context.Context, tx pgx.Tx, p access.Principal, w string, e Evaluation) (Evaluation, []Ref, error) {
	if e.Kind != "reported" && e.Kind != "estimated" && e.Kind != "api_equivalent" && e.Kind != "allocated" {
		return e, nil, ErrInvalid
	}
	c, err := s.group(ctx, tx, w, e.Group)
	if err != nil {
		return e, nil, err
	}
	refs := append([]Ref{}, c.Refs...)
	for _, r := range c.Refs {
		if _, err = s.read(ctx, tx, p, w, r); err != nil {
			return e, nil, err
		}
	}
	e.Usage = c.Refs
	e.Quantities = c.Quantities
	e.Adopted = c.Adopted
	e.State = c.State
	if e.Previous != nil {
		if e.Previous.Version == "" || e.Previous.Version == "head" {
			return e, nil, ErrInvalid
		}
		old, err := s.read(ctx, tx, p, w, *e.Previous)
		if err != nil {
			return e, nil, err
		}
		var prev Evaluation
		if old.Kind != "evaluation" || json.Unmarshal(old.Value, &prev) != nil || prev.Group != e.Group || prev.Kind != e.Kind {
			return e, nil, ErrInvalid
		}
		refs = append(refs, *e.Previous)
	}
	if e.Kind == "estimated" || e.Kind == "api_equivalent" {
		e.Amount = nil
		e.State = "unrated"
		if e.Price != nil {
			if e.Price.Version == "" || e.Price.Version == "head" {
				return e, nil, ErrInvalid
			}
			r, err := s.read(ctx, tx, p, w, *e.Price)
			if err != nil {
				return e, nil, err
			}
			var price Price
			if r.Kind != "price" || json.Unmarshal(r.Value, &price) != nil || price.Connection != c.Connection || (e.Kind == "estimated" && price.Contract != c.Contract) || c.ActualModel == nil || price.Model != *c.ActualModel || c.At.Before(price.From) || (price.Until != nil && !c.At.Before(*price.Until)) {
				return e, nil, ErrInvalid
			}
			e.Currency = price.Currency
			e.Scale = price.Scale
			e.Rounding = price.Rounding
			e.Source = price.Source
			refs = append(refs, *e.Price)
			if c.State == "confirmed" {
				e.Amount, e.State, err = calculate(c.Quantities, price)
				if err != nil {
					return e, nil, err
				}
			}
		}
	} else {
		if e.Price != nil || e.Source == "" || e.Currency == "" {
			return e, nil, ErrInvalid
		}
		if e.Amount == nil {
			e.State = "unrated"
		} else {
			n, err := number(*e.Amount)
			if err != nil {
				return e, nil, err
			}
			rounded, err := round(n, e.Scale, e.Rounding)
			if err != nil {
				return e, nil, err
			}
			e.Amount = &rounded
			e.State = "rated"
		}
	}
	if c.State != "confirmed" {
		e.State = "review_needed"
		if e.Kind == "estimated" || e.Kind == "api_equivalent" {
			e.Amount = nil
		}
	}
	if e.FX != nil {
		f := e.FX
		rate, err := number(f.Rate)
		if err != nil || rate.Sign() == 0 || f.At.IsZero() || f.Source == "" || f.From != e.Currency || f.To == "" {
			return e, nil, ErrInvalid
		}
		f.OriginalAmount = e.Amount
		if e.Amount != nil {
			amount, _ := number(*e.Amount)
			converted, err := round(new(big.Rat).Mul(amount, rate), e.Scale, e.Rounding)
			if err != nil {
				return e, nil, err
			}
			e.Amount = &converted
		}
		e.Currency = f.To
	}
	return e, refs, nil
}

func uniqueRefs(refs []Ref) []Ref {
	seen := map[Ref]bool{}
	result := make([]Ref, 0, len(refs))
	for _, ref := range refs {
		if !seen[ref] {
			seen[ref] = true
			result = append(result, ref)
		}
	}
	return result
}
