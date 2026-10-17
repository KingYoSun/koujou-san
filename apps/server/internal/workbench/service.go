// Package workbench presents existing work and stores routing/return notes.
// It never starts a Run or duplicates Pause responses or execution state.
package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"github.com/KingYoSun/koujou-san/apps/server/internal/workflows"
	"github.com/jackc/pgx/v5"
)

type Ref = access.VersionRef

var ErrInvalid = errors.New("invalid_work_note")

type Note struct {
	Kind      string           `json:"kind"`
	Subject   Ref              `json:"subject"`
	Pause     *Ref             `json:"pause,omitempty"`
	Recipient string           `json:"recipient,omitempty"`
	Reason    string           `json:"reason"`
	Targets   []contexts.Fixed `json:"targets,omitempty"`
	Criteria  []string         `json:"criteria,omitempty"`
	Problem   string           `json:"problem,omitempty"`
	Expected  string           `json:"expected,omitempty"`
	By        string           `json:"by"`
	At        time.Time        `json:"at"`
}
type Record struct {
	Ref
	Owner    string `json:"owner_id"`
	Position string `json:"position"`
	Value    Note   `json:"value"`
	Refs     []Ref  `json:"-"`
}
type Write struct {
	apicontract.Mutation
	Note Note `json:"note"`
}
type Service struct {
	Store     *persistence.Store
	Access    *access.Service
	Runs      *runs.Service
	Workflows *workflows.Service
}

func New(store *persistence.Store, a *access.Service, r *runs.Service, f *workflows.Service) *Service {
	return &Service{store, a, r, f}
}
func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func fixed(r Ref) bool {
	return r.ID != "" && r.Version != "" && r.Version != "head" && r.Version != "0"
}
func unique(refs []Ref) []Ref {
	out := []Ref{}
	seen := map[Ref]bool{}
	for _, ref := range refs {
		if !seen[ref] {
			out = append(out, ref)
			seen[ref] = true
		}
	}
	return out
}

// subject uses the existing domain's current dependency authorization, including
// restrictions added since the fixed subject was recorded.
func (s *Service) subject(ctx context.Context, p access.Principal, w string, ref Ref) ([]Ref, []string, error) {
	if strings.HasPrefix(ref.ID, "run:") {
		r, err := s.Runs.Read(ctx, p, w, ref)
		if err != nil {
			return nil, nil, err
		}
		var v runs.Run
		if r.Kind != "run" || json.Unmarshal(r.Value, &v) != nil {
			return nil, nil, ErrInvalid
		}
		i, err := s.Runs.Read(ctx, p, w, v.Input)
		return unique(append(append(r.Refs, i.Refs...), ref)), []string{r.ID}, err
	}
	if strings.HasPrefix(ref.ID, "workflow-execution:") {
		r, err := s.Workflows.Read(ctx, p, w, ref)
		if err != nil {
			return nil, nil, err
		}
		var v workflows.Execution
		if r.Kind != "execution" || json.Unmarshal(r.Value, &v) != nil {
			return nil, nil, ErrInvalid
		}
		return unique(append(r.Refs, ref)), []string{r.ID, v.Definition.ID}, nil
	}
	return nil, nil, ErrInvalid
}
func noteRecord(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, w string, ref Ref) (Record, error) {
	var r Record
	var value, refs []byte
	err := q.QueryRow(ctx, `SELECT id,version,owner_id,position::text,payload,refs FROM workbench_records WHERE workspace_id=$1 AND id=$2 AND ($3='head' OR version=$3) ORDER BY position DESC LIMIT 1`, w, ref.ID, ref.Version).Scan(&r.ID, &r.Version, &r.Owner, &r.Position, &value, &refs)
	if err == nil {
		err = json.Unmarshal(value, &r.Value)
	}
	if err == nil {
		err = json.Unmarshal(refs, &r.Refs)
	}
	return r, err
}
func (s *Service) Read(ctx context.Context, p access.Principal, w string, ref Ref) (Record, error) {
	r, err := noteRecord(ctx, s.Store.Database(), w, ref)
	if err != nil {
		return r, access.ErrForbidden
	}
	if _, _, err = s.subject(ctx, p, w, r.Value.Subject); err != nil {
		return Record{}, err
	}
	var allowed bool
	err = s.Store.Database().QueryRow(ctx, `SELECT `+access.ReadFilterSQL("$4::jsonb"), w, p.UserID, p.OperationID, raw(append(r.Refs, r.Ref))).Scan(&allowed)
	if err == nil && !allowed {
		err = access.ErrForbidden
	}
	return r, err
}
func (s *Service) Put(ctx context.Context, p access.Principal, w, id string, req Write) (apicontract.Response[apicontract.Reference], error) {
	empty := apicontract.Response[apicontract.Reference]{}
	n := req.Note
	if p.DeviceID != "" || p.OperationID != "" {
		return empty, access.ErrForbidden
	}
	if !strings.HasPrefix(id, "work-note:") || !fixed(n.Subject) || strings.TrimSpace(n.Reason) == "" {
		return empty, ErrInvalid
	}
	if n.Kind == "route" {
		if n.Recipient == "" || (n.Pause != nil && !fixed(*n.Pause)) {
			return empty, ErrInvalid
		}
	} else if n.Kind == "return" {
		if !strings.HasPrefix(n.Subject.ID, "run:") || len(n.Targets) == 0 || len(n.Criteria) == 0 || strings.TrimSpace(n.Problem) == "" || strings.TrimSpace(n.Expected) == "" {
			return empty, ErrInvalid
		}
		for _, a := range n.Targets {
			if a.URI == "" || a.Revision == "" {
				return empty, ErrInvalid
			}
		}
	} else {
		return empty, ErrInvalid
	}
	refs, targets, err := s.subject(ctx, p, w, n.Subject)
	if err != nil {
		return empty, err
	}
	if n.Pause != nil {
		refs = append(refs, *n.Pause)
	}
	check := func(tx pgx.Tx) error {
		for _, target := range targets {
			var err error
			if tx == nil {
				err = s.Access.Check(ctx, p, w, target, "update")
			} else {
				err = s.Access.CheckTx(ctx, tx, p, w, target, "update")
			}
			if err != nil {
				return err
			}
		}
		return nil
	}
	if err = check(nil); err != nil {
		return empty, err
	}
	scope := persistence.Scope{ActorID: "user:" + p.UserID, Grant: apicontract.Grant{WorkspaceID: w, CanRead: true, CanUpdate: true}, Recheck: func(tx pgx.Tx) *apicontract.Problem {
		if check(tx) != nil {
			return &apicontract.Problem{Code: "forbidden"}
		}
		return nil
	}}
	return s.Store.Apply(ctx, scope, persistence.Command{WorkspaceID: w, ResourceID: id, Kind: "work-note", Mutation: req.Mutation, Payload: raw(req.Note)}, func(tx pgx.Tx, ref apicontract.Reference) (json.RawMessage, error) {
		// Re-read the same subject/dependencies inside the serialized Workspace
		// mutation. Routing cannot be applied to a different active question.
		currentRefs, err := s.Runs.SourcesTx(ctx, tx, p, w, refs)
		if err != nil {
			return nil, err
		}
		refs = currentRefs
		if old, err := noteRecord(ctx, tx, w, Ref{ID: id, Version: "head"}); err == nil && (old.Owner != n.Subject.ID || old.Value.Kind != n.Kind) {
			return nil, ErrInvalid
		}
		if n.Kind == "route" {
			recipient := access.Principal{UserID: n.Recipient}
			if _, err := s.Runs.SourcesTx(ctx, tx, recipient, w, refs); err != nil {
				return nil, access.ErrForbidden
			}
			for _, target := range targets {
				if s.Access.CheckTx(ctx, tx, recipient, w, target, "update") != nil {
					return nil, access.ErrForbidden
				}
			}
			if n.Pause != nil {
				var payload []byte
				err := tx.QueryRow(ctx, `SELECT payload FROM run_records WHERE workspace_id=$1 AND id=$2 ORDER BY position DESC LIMIT 1`, w, n.Subject.ID).Scan(&payload)
				if err != nil {
					return nil, err
				}
				var v runs.Run
				_ = json.Unmarshal(payload, &v)
				if v.State != "paused" || v.ActivePause == nil || *v.ActivePause != *n.Pause {
					return nil, runs.ErrState
				}
				refs = append(refs, *n.Pause)
			}
		}
		n.By = p.UserID
		n.At = time.Now().UTC()
		r := Record{Ref: Ref{ID: ref.ID, Version: ref.Version}, Owner: n.Subject.ID, Value: n, Refs: unique(refs)}
		err = tx.QueryRow(ctx, `INSERT INTO workbench_records SELECT $1,$2,$3,$4,$5,$6,sequence+1,$7,$8 FROM shared_streams WHERE workspace_id=$1 RETURNING position::text`, w, id, r.Version, r.Owner, n.Kind, n.Recipient, raw(n), raw(r.Refs)).Scan(&r.Position)
		if err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO access_resources(workspace_id,id,restricted) VALUES($1,$2,false) ON CONFLICT DO NOTHING`, w, id)
		}
		return raw(r), err
	})
}
