package inspections

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"github.com/jackc/pgx/v5"
)

func (s *Service) launch(ctx context.Context, q query, w, run string) (Launch, Record, error) {
	var ref Ref
	err := q.QueryRow(ctx, `SELECT launch_id,version FROM inspection_launches WHERE workspace_id=$1 AND run_id=$2`, w, run).Scan(&ref.ID, &ref.Version)
	if err != nil {
		return Launch{}, Record{}, err
	}
	r, err := record(ctx, q, w, ref)
	var launch Launch
	if err == nil {
		err = decode(r.Value, &launch)
	}
	return launch, r, err
}

// Only the fixed target, criteria and required inputs affect applicability.
func basis(d Definition) []byte {
	d.Name = ""
	return raw(d)
}
func (s *Service) stale(ctx context.Context, q query, p access.Principal, w string, l Launch) (bool, error) {
	old, _, err := s.definition(ctx, q, p, w, l.Definition)
	if err != nil {
		return false, err
	}
	a, err := record(ctx, q, w, Ref{ID: "inspection-adoption:" + l.Definition.ID, Version: "head"})
	if err != nil {
		return false, err
	}
	if err = s.refs(ctx, q, p, w, append(a.Refs, a.Ref)); err != nil {
		return false, err
	}
	var adoption Adoption
	if decode(a.Value, &adoption) != nil {
		return false, ErrInvalid
	}
	current, _, err := s.definition(ctx, q, p, w, adoption.Definition)
	return !bytes.Equal(basis(old), basis(current)), err
}

func (s *Service) separation(ctx context.Context, q query, p access.Principal, w string, d Definition, capabilities []string) (map[string]Separation, error) {
	actual := separation(capabilities)
	for dimension, refs := range d.Observations {
		observed := true
		for _, ref := range refs {
			r, err := record(ctx, q, w, ref)
			if err != nil {
				return nil, err
			}
			if err = s.refs(ctx, q, p, w, append(r.Refs, r.Ref)); err != nil {
				return nil, err
			}
			var e Evidence
			if decode(r.Value, &e) != nil || e.Kind != "machine-observation" || !available(e, time.Now()) {
				observed = false
			}
			current, err := record(ctx, q, w, Ref{ID: ref.ID, Version: "head"})
			if err != nil {
				return nil, err
			}
			if err = s.refs(ctx, q, p, w, append(current.Refs, current.Ref)); err != nil {
				return nil, err
			}
			if decode(current.Value, &e) != nil || !available(e, time.Now()) {
				observed = false
			}
		}
		if observed && rank(actual[dimension].Level) < rank("observed") {
			actual[dimension] = Separation{Level: "observed", Origin: "observation", Method: "recorded-machine-log", Evidence: refs}
		}
	}
	return actual, nil
}

func (s *Service) beforeStart(ctx context.Context, tx pgx.Tx, w, run string) error {
	var q query = s.Store.Database()
	if tx != nil {
		q = tx
	}
	l, r, err := s.launch(ctx, q, w, run)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	p := access.Principal{UserID: l.By}
	if tx != nil {
		err = s.Access.CheckTx(ctx, tx, p, w, r.Owner, "execute")
	} else {
		err = s.Access.Check(ctx, p, w, r.Owner, "execute")
	}
	if err != nil {
		return err
	}
	if err = s.refs(ctx, q, p, w, append(r.Refs, r.Ref)); err != nil {
		return err
	}
	stale, err := s.stale(ctx, q, p, w, l)
	if err != nil {
		return err
	}
	if stale {
		return ErrState
	}
	d, _, err := s.definition(ctx, q, p, w, l.Definition)
	if err != nil {
		return err
	}
	var b []byte
	if err = q.QueryRow(ctx, `SELECT payload FROM run_runners WHERE workspace_id=$1 AND device_id=$2`, w, l.Device).Scan(&b); err != nil {
		return err
	}
	var profile runs.Runner
	if json.Unmarshal(b, &profile) != nil {
		return ErrInvalid
	}
	found := false
	for _, c := range profile.Connections {
		if c.Kind == l.Spec.Input.Command.Kind && c.Target == l.Spec.Input.Command.Target {
			actual, err := s.separation(ctx, q, p, w, d, c.Capabilities)
			if err != nil {
				return err
			}
			if !c.Confirmed || len(gaps(d.Required, actual)) > 0 {
				return ErrState
			}
			found = true
			break
		}
	}
	if !found {
		return ErrState
	}
	// A direct common-Run reassignment must not bypass the selected connection.
	err = q.QueryRow(ctx, `SELECT payload FROM run_records WHERE workspace_id=$1 AND id=$2 AND kind='run' ORDER BY run_records.position DESC LIMIT 1`, w, run).Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var state runs.Run
	_ = json.Unmarshal(b, &state)
	if state.Assignment != nil && (state.Assignment.DeviceID != l.Device || !bytes.Equal(raw(state.Assignment.Repository), raw(l.Repository))) {
		return ErrState
	}
	return nil
}

func (s *Service) evidence(ctx context.Context, q query, p access.Principal, w string, refs []Ref, current bool) (map[Ref]Evidence, []Ref, error) {
	out := map[Ref]Evidence{}
	deps := []Ref{}
	for _, ref := range unique(refs) {
		if !fixed(ref) {
			continue
		}
		lookup := ref
		if current {
			lookup.Version = "head"
		}
		r, err := record(ctx, q, w, lookup)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		if r.Kind != "evidence" {
			continue
		}
		if err = s.refs(ctx, q, p, w, append(r.Refs, r.Ref)); err != nil {
			return nil, nil, err
		}
		var e Evidence
		if decode(r.Value, &e) != nil {
			return nil, nil, ErrInvalid
		}
		out[ref] = e
		deps = append(append(deps, r.Refs...), r.Ref)
	}
	return out, unique(deps), nil
}
func reportRefs(l Launch, b []byte, d Definition) []Ref {
	refs := []Ref{}
	r, err := reportValue(l, b)
	if err == nil {
		for _, c := range r.Results {
			refs = append(refs, c.Evidence...)
		}
	}
	if d.Reuse != nil {
		refs = append(refs, d.Reuse.Evidence...)
	}
	return unique(refs)
}
func (s *Service) assessResult(ctx context.Context, tx pgx.Tx, w, run string, state runs.Run, base *runs.Assessment) (*runs.Assessment, []Ref, error) {
	l, _, err := s.launch(ctx, tx, w, run)
	if errors.Is(err, pgx.ErrNoRows) {
		return base, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	p := access.Principal{UserID: l.By}
	d, _, err := s.definition(ctx, tx, p, w, l.Definition)
	if err != nil {
		return nil, nil, err
	}
	var output json.RawMessage
	if base != nil {
		output = base.Report
	}
	evidence, refs, err := s.evidence(ctx, tx, p, w, reportRefs(l, output, d), false)
	if err != nil {
		return nil, nil, err
	}
	currentEvidence, currentRefs, err := s.evidence(ctx, tx, p, w, reportRefs(l, output, d), true)
	if err != nil {
		return nil, nil, err
	}
	for ref, historical := range evidence {
		if e, ok := currentEvidence[ref]; !ok || !available(e, time.Now()) {
			historical.State = "unavailable"
			evidence[ref] = historical
		}
	}
	refs = unique(append(refs, currentRefs...))
	result := assess(d, l, output, base, evidence, time.Now())
	if state.ExitCode == nil || *state.ExitCode != 0 {
		if result.Verdict == "PASS" {
			result.Verdict = "INCONCLUSIVE"
		}
	}
	if d.Reuse != nil && result.Verdict == "PASS" {
		var priorPayload []byte
		if err = tx.QueryRow(ctx, `SELECT payload FROM run_records WHERE workspace_id=$1 AND id=$2 AND version=$3 AND kind='run'`, w, d.Reuse.Run.ID, d.Reuse.Run.Version).Scan(&priorPayload); err != nil {
			return nil, nil, err
		}
		var priorState runs.Run
		if json.Unmarshal(priorPayload, &priorState) != nil || priorState.Assessment == nil || priorState.Assessment.State != "valid" || priorState.Assessment.Verdict != "PASS" {
			result.Verdict = "INCONCLUSIVE"
		}
		prior, _, err := s.launch(ctx, tx, w, d.Reuse.Run.ID)
		if err != nil {
			return nil, nil, err
		}
		old, _, err := s.definition(ctx, tx, p, w, prior.Definition)
		if err != nil {
			return nil, nil, err
		}
		for _, id := range d.Reuse.Criteria {
			found := false
			for _, previous := range old.Criteria {
				for _, current := range d.Criteria {
					if previous.ID == id && current.ID == id && bytes.Equal(raw(previous), raw(current)) {
						found = true
					}
				}
			}
			if !found {
				result.Verdict = "INCONCLUSIVE"
			}
		}
	}
	return result, refs, nil
}

type Current struct {
	Launch   Record              `json:"launch"`
	Run      runs.Record         `json:"run"`
	Validity string              `json:"validity"`
	Evidence map[string]Evidence `json:"evidence_current"`
}

func (s *Service) Current(ctx context.Context, p access.Principal, w, run string) (Current, error) {
	run = "run:" + strings.TrimPrefix(run, "run:")
	l, r, err := s.launch(ctx, s.Store.Database(), w, run)
	if err != nil {
		return Current{}, err
	}
	r, err = s.Read(ctx, p, w, r.Ref)
	if err != nil {
		return Current{}, err
	}
	head, err := s.Runs.Read(ctx, p, w, Ref{ID: run, Version: "head"})
	if err != nil {
		return Current{}, err
	}
	stale, err := s.stale(ctx, s.Store.Database(), p, w, l)
	if err != nil {
		return Current{}, err
	}
	d, _, err := s.definition(ctx, s.Store.Database(), p, w, l.Definition)
	if err != nil {
		return Current{}, err
	}
	var state runs.Run
	_ = json.Unmarshal(head.Value, &state)
	var report []byte
	if state.Assessment != nil {
		report = state.Assessment.Report
	}
	evidence, _, err := s.evidence(ctx, s.Store.Database(), p, w, reportRefs(l, report, d), true)
	if err != nil {
		return Current{}, err
	}
	out := Current{Launch: r, Run: head, Validity: "current", Evidence: map[string]Evidence{}}
	if stale {
		out.Validity = "STALE"
	}
	for ref, e := range evidence {
		if !available(e, time.Now()) && e.State == "available" {
			e.State = "expired"
		}
		out.Evidence[ref.ID+"@"+ref.Version] = e
	}
	return out, nil
}

type Decide struct {
	apicontract.Mutation
	Judgment
}

func (s *Service) Decide(ctx context.Context, p access.Principal, w, id string, req Decide) (apicontract.Response[apicontract.Reference], error) {
	if !strings.HasPrefix(id, "inspection-judgment:") || !fixed(req.Launch) || !fixed(req.Source) || len(req.Criteria) == 0 || strings.TrimSpace(req.Reason) == "" || (req.Decision != "accept-with-exceptions" && req.Decision != "reject") {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	launch, err := s.Read(ctx, p, w, req.Launch)
	if err != nil {
		return apicontract.Response[apicontract.Reference]{}, err
	}
	var l Launch
	if launch.Kind != "launch" || decode(launch.Value, &l) != nil {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	return s.mutate(ctx, p, w, id, l.Run, "judgment", "update", req.Mutation, req, func(tx pgx.Tx, ref Ref) (Record, error) {
		source, err := s.Runs.Read(ctx, p, w, req.Source)
		if err != nil {
			return Record{}, err
		}
		if source.RunID != l.Run {
			return Record{}, ErrInvalid
		}
		var result runs.Run
		if (source.Kind != "run" && source.Kind != "result") || json.Unmarshal(source.Value, &result) != nil || result.Assessment == nil {
			return Record{}, ErrInvalid
		}
		d, _, err := s.definition(ctx, tx, p, w, l.Definition)
		if err != nil {
			return Record{}, err
		}
		for _, id := range req.Criteria {
			found := false
			for _, c := range d.Criteria {
				if c.ID == id {
					found = true
				}
			}
			if !found {
				return Record{}, ErrInvalid
			}
		}
		j := req.Judgment
		j.By = p.UserID
		j.At = time.Now().UTC()
		refs := append(append(append([]Ref{}, launch.Refs...), source.Refs...), launch.Ref, source.Ref)
		if err = s.refs(ctx, tx, p, w, refs); err != nil {
			return Record{}, err
		}
		return s.save(ctx, tx, w, ref, launch.Owner, "judgment", j, refs)
	})
}
