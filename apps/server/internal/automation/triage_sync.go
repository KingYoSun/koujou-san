package automation

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/jackc/pgx/v5"
)

type LabelEvent struct {
	ID     int64  `json:"id"`
	Action string `json:"action"`
	Label  string `json:"label"`
	Own    bool   `json:"own"`
}
type ExternalSnapshot struct {
	Labels    []string     `json:"labels"`
	Events    []LabelEvent `json:"events"`
	More      bool         `json:"more"`
	Title     string       `json:"title"`
	Body      string       `json:"body"`
	State     string       `json:"state"`
	UpdatedAt time.Time    `json:"updated_at"`
}
type PriorityBackend interface {
	ReadPriority(context.Context, string, string, int64, int) (ExternalSnapshot, error)
	WritePriority(context.Context, string, string, int64, string, string) error
}
type PrioritySync struct {
	WriteAttempts     int         `json:"write_attempts"`
	ConfirmedDecision string      `json:"confirmed_decision,omitempty"`
	ConflictDecision  string      `json:"conflict_decision,omitempty"`
	Binding           string      `json:"binding"`
	Scope             Ref         `json:"scope"`
	Info              Ref         `json:"info"`
	Priority          *Ref        `json:"priority,omitempty"`
	Page              int         `json:"page"`
	Seen              []int64     `json:"seen"`
	Initialized       bool        `json:"initialized"`
	Human             bool        `json:"human"`
	Pending           *LabelEvent `json:"pending,omitempty"`
	Labels            []string    `json:"labels"`
	State             string      `json:"state"`
	Reason            string      `json:"reason"`
	At                time.Time   `json:"at"`
}

func priorityDecision(p Priority) string {
	policy := p.Policy
	if p.AdoptedPolicy != nil {
		policy = *p.AdoptedPolicy
	}
	return digest([]any{p.Adopted, p.Origin, p.AdoptedReason, policy})
}

func mapped(labels []string, mapping map[string]string) []string {
	out := []string{}
	for _, name := range labels {
		for _, label := range mapping {
			if name == label {
				out = append(out, name)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// SyncPriority processes at most one external events page and one label change.
// Every write is preceded by observation; a lost response is reconciled before
// any retry. It sends only configured labels, never a reason or evidence body.
func (s *Service) SyncPriority(ctx context.Context) (bool, error) {
	if s.PriorityBackend == nil {
		return false, nil
	}
	var w, scopeID, lease string
	err := pgx.BeginFunc(ctx, s.Store.Database(), func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT workspace_id,scope_id FROM triage_sync WHERE due_at<=now() AND (lease_until IS NULL OR lease_until<now()) ORDER BY due_at,workspace_id,scope_id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&w, &scopeID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		lease = rand.Text()
		_, err = tx.Exec(ctx, `UPDATE triage_sync SET lease=$3,lease_until=now()+interval '120 seconds' WHERE workspace_id=$1 AND scope_id=$2`, w, scopeID, lease)
		return err
	})
	if err != nil || lease == "" {
		return false, err
	}
	immediate := false
	defer func() {
		_, _ = s.Store.Database().Exec(context.WithoutCancel(ctx), `UPDATE triage_sync SET lease_until=NULL,due_at=now()+make_interval(secs=>$4) WHERE workspace_id=$1 AND scope_id=$2 AND lease=$3`, w, scopeID, lease, func() int {
			if immediate {
				return 0
			}
			return s.priorityInterval()
		}())
	}()
	var infoID, infoVersion, scopeVersion string
	if err = s.Store.Database().QueryRow(ctx, `SELECT info_id,info_version,scope_version FROM triage_targets WHERE workspace_id=$1 AND scope_id=$2`, w, scopeID).Scan(&infoID, &infoVersion, &scopeVersion); err != nil {
		return true, err
	}
	infoRecord, err := record(ctx, s.Store.Database(), w, Ref{ID: infoID, Version: infoVersion})
	if err != nil {
		return true, err
	}
	var info Issue
	if json.Unmarshal(infoRecord.Value, &info) != nil || info.External == nil {
		return true, ErrInvalid
	}
	info.Scope = Ref{ID: scopeID, Version: scopeVersion}
	external := *info.External
	if len(external.Labels) == 0 {
		selected, reason, _, err := s.selectPolicy(ctx, s.Store.Database(), w, info.Scope)
		if err != nil || reason != "" {
			return true, ErrInvalid
		}
		var policy Policy
		_ = json.Unmarshal(selected.Value, &policy)
		if policy.External == nil || policy.External.Connection != external.Connection {
			return true, ErrInvalid
		}
		external.Labels = policy.External.Labels
	}
	id := "priority-sync:" + scopeID
	current, oldErr := record(ctx, s.Store.Database(), w, Ref{ID: id, Version: "head"})
	expected := "0"
	sync := PrioritySync{Binding: digest(external), Scope: info.Scope, Info: infoRecord.Ref, Page: 1, Seen: []int64{}, Labels: []string{}, State: "observing"}
	if oldErr == nil {
		expected = current.Version
		if json.Unmarshal(current.Value, &sync) != nil {
			return true, ErrInvalid
		}
		if sync.Binding != digest(external) {
			sync = PrioritySync{Binding: digest(external), Scope: info.Scope, Info: infoRecord.Ref, Page: 1, Seen: []int64{}, Labels: []string{}, State: "observing"}
		}
	} else if !errors.Is(oldErr, pgx.ErrNoRows) {
		return true, oldErr
	}
	sync.Info = infoRecord.Ref
	var actor access.Principal
	check := func(tx pgx.Tx) error {
		var valid bool
		if err := tx.QueryRow(ctx, `SELECT lease=$3 AND lease_until>now() FROM triage_sync WHERE workspace_id=$1 AND scope_id=$2 FOR UPDATE`, w, scopeID, lease).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return ErrLease
		}
		c, err := control(ctx, tx, w, "triage")
		if err != nil || !c.Enabled || !c.CanRead || !c.CanCommit {
			return access.ErrForbidden
		}
		actor = access.Principal{UserID: c.Controller}
		if err = s.Access.CheckTx(ctx, tx, actor, w, "", "manage"); err != nil {
			return err
		}
		if err = s.Access.CheckTx(ctx, tx, actor, w, scopeID, "execute"); err != nil {
			return err
		}
		if err = s.Access.CheckTx(ctx, tx, actor, w, scopeID, "update"); err != nil {
			return err
		}
		var version string
		if err = tx.QueryRow(ctx, `SELECT version FROM shared_versions WHERE workspace_id=$1 AND resource_id=$2`, w, infoID).Scan(&version); err != nil || version != infoVersion {
			return ErrLease
		}
		return s.checkRefs(ctx, tx, actor, w, append(infoRecord.Refs, infoRecord.Ref))
	}
	if err = pgx.BeginFunc(ctx, s.Store.Database(), check); err != nil {
		return true, err
	}
	snapshot, readErr := s.PriorityBackend.ReadPriority(ctx, w, external.Connection, external.Number, sync.Page)
	if _, err = s.Store.Database().Exec(ctx, `UPDATE triage_sync SET checked_at=now(),observed_at=CASE WHEN $4 THEN now() ELSE observed_at END WHERE workspace_id=$1 AND scope_id=$2 AND lease=$3`, w, scopeID, lease, readErr == nil); err != nil {
		return true, err
	}
	writeAction := (*LabelEvent)(nil)
	// Label notifications change updated_at too. Only source fields affect the
	// evaluation fingerprint; preserve the local Issue body separately.
	if readErr == nil && !snapshot.More && !snapshot.UpdatedAt.IsZero() && (info.Observed == nil || info.Observed.Title != snapshot.Title || info.Observed.Body != snapshot.Body || info.Observed.State != snapshot.State) {
		info.Observed = &ExternalIssue{Title: snapshot.Title, Body: snapshot.Body, State: snapshot.State, At: snapshot.UpdatedAt}
		_, err = s.systemTriage(ctx, w, infoID, "observed-issue", "external-input:"+digest(info.Observed), infoVersion, check, func(tx pgx.Tx, ref Ref) (Record, error) {
			_, err := s.save(ctx, tx, w, ref, "issue", info, infoRecord.Refs)
			return Record{Ref: ref, Kind: "issue", Value: encode(info), Refs: infoRecord.Refs}, err
		})
		return true, err
	}
	if readErr != nil {
		sync.State = "failed"
		sync.Reason = "external_observation_failed"
	} else {
		for _, event := range snapshot.Events {
			if slices.Contains(sync.Seen, event.ID) {
				continue
			}
			sync.Seen = append(sync.Seen, event.ID)
			if event.Action != "labeled" && event.Action != "unlabeled" {
				continue
			}
			if len(mapped([]string{event.Label}, external.Labels)) == 0 {
				continue
			}
			if !(event.Own && sync.Pending != nil && event.Action == sync.Pending.Action && event.Label == sync.Pending.Label) {
				sync.Human = true
			}
		}
		sync.Labels = mapped(snapshot.Labels, external.Labels)
		if snapshot.More {
			sync.Page++
			sync.Seen = []int64{}
			sync.State = "observing"
		} else {
			adopted, err := record(ctx, s.Store.Database(), w, Ref{ID: "priority:" + scopeID, Version: "head"})
			var priority Priority
			if err == nil {
				_ = json.Unmarshal(adopted.Value, &priority)
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return true, err
			}
			wasConflict := sync.State == "conflict" && sync.ConflictDecision == priorityDecision(priority) && !sync.Human
			sync.State = "synced"
			sync.Reason = ""
			// Multiple external values or concurrent human decisions need resolution.
			if wasConflict {
				sync.State = "conflict"
				sync.Reason = "external_priority_conflict"
			} else if len(sync.Labels) > 1 && sync.Human {
				sync.State = "conflict"
				sync.Reason = "external_priority_conflict"
			} else if sync.Human || (!sync.Initialized && len(sync.Labels) > 0) {
				var value *string
				for level, label := range external.Labels {
					if slices.Contains(sync.Labels, label) {
						v := level
						value = &v
					}
				}
				if priority.Fixed && priority.Origin == "manual" && priorityDecision(priority) != sync.ConfirmedDecision && ((priority.Adopted == nil) != (value == nil) || (value != nil && priority.Adopted != nil && *priority.Adopted != *value)) {
					sync.State = "conflict"
					sync.Reason = "concurrent_manual_priority"
				} else {
					selected, reason, _, err := s.selectPolicy(ctx, s.Store.Database(), w, info.Scope)
					if err != nil {
						return true, err
					}
					if reason != "" {
						sync.State = "blocked"
						sync.Reason = reason
					} else {
						var policy Policy
						_ = json.Unmarshal(selected.Value, &policy)
						if value != nil && !hasLevel(policy, *value) {
							sync.State = "conflict"
							sync.Reason = "external_level_unmapped"
						} else {
							priority.Target, priority.Policy, priority.AdoptedPolicy = info.Scope, selected.Ref, &selected.Ref
							if priority.State == "" {
								priority.State = "unevaluated"
							}
							priority.Adopted = value
							priority.Origin = "external"
							priority.Fixed = true
							priority.AdoptedReason = "GitHubの優先度ラベルを取り込みました"
							priority.Reason = priority.AdoptedReason
							prExpected := "0"
							if adopted.ID != "" {
								prExpected = adopted.Version
							}
							refs := append(append(append(append([]Ref{}, infoRecord.Refs...), selected.Refs...), priority.Refs...), infoRecord.Ref, selected.Ref)
							rec, err := s.systemTriage(ctx, w, "priority:"+scopeID, "external-priority", "external-priority:"+lease, prExpected, check, func(tx pgx.Tx, ref Ref) (Record, error) {
								if err := s.checkRefs(ctx, tx, actor, w, refs); err != nil {
									return Record{}, err
								}
								if prExpected == "0" {
									if err := s.Access.PutResource(ctx, tx, access.Resource{WorkspaceID: w, ID: ref.ID}, nil); err != nil {
										return Record{}, err
									}
								}
								_, err := s.save(ctx, tx, w, ref, "priority", priority, refs)
								return Record{Ref: ref}, err
							})
							if err != nil {
								return true, err
							}
							adopted = rec
						}
					}
				}
			} else if priority.Adopted != nil {
				target, ok := external.Labels[*priority.Adopted]
				if !ok {
					sync.State = "blocked"
					sync.Reason = "external_level_unmapped"
				} else {
					action := LabelEvent{Action: "labeled", Label: target}
					if slices.Contains(sync.Labels, target) {
						action = LabelEvent{}
						for _, label := range sync.Labels {
							if label != target {
								action = LabelEvent{Action: "unlabeled", Label: label}
								break
							}
						}
					}
					if action.Label != "" {
						sync.Pending = &action
						writeAction = &action
						sync.State = "reconciling"
					} else {
						sync.Pending = nil
					}
				}
			}
			if sync.State == "synced" {
				sync.ConfirmedDecision = priorityDecision(priority)
				sync.ConflictDecision = ""
			}
			if sync.State == "conflict" {
				sync.ConflictDecision = priorityDecision(priority)
			}
			sync.Initialized = true
			sync.Human = false
			if adopted.ID != "" {
				sync.Priority = &adopted.Ref
			}
		}
	}
	if writeAction != nil {
		sync.WriteAttempts++
	}
	next := sync
	next.At = time.Time{}
	if oldErr == nil {
		var before PrioritySync
		if json.Unmarshal(current.Value, &before) == nil {
			before.At = time.Time{}
			if digest(before) == digest(next) {
				return true, nil
			}
		}
	}
	sync.At = time.Now().UTC()
	refs := append(append([]Ref{}, infoRecord.Refs...), infoRecord.Ref)
	if sync.Priority != nil {
		refs = append(refs, *sync.Priority)
	}
	saved, err := s.systemTriage(ctx, w, id, "priority-sync", "sync:"+lease, expected, check, func(tx pgx.Tx, ref Ref) (Record, error) {
		if expected == "0" {
			if err := s.Access.PutResource(ctx, tx, access.Resource{WorkspaceID: w, ID: ref.ID}, nil); err != nil {
				return Record{}, err
			}
		}
		_, err := s.save(ctx, tx, w, ref, "sync", sync, refs)
		return Record{Ref: ref}, err
	})
	if err != nil {
		return true, err
	}
	if writeAction != nil {
		// Recheck authority and the adopted version immediately before external I/O.
		err = pgx.BeginFunc(ctx, s.Store.Database(), func(tx pgx.Tx) error {
			if err := check(tx); err != nil {
				return err
			}
			if sync.Priority == nil {
				return ErrInvalid
			}
			priority, err := record(ctx, tx, w, *sync.Priority)
			if err != nil {
				return err
			}
			if err = s.checkRefs(ctx, tx, actor, w, append(priority.Refs, priority.Ref)); err != nil {
				return err
			}
			var version string
			if err := tx.QueryRow(ctx, `SELECT version FROM shared_versions WHERE workspace_id=$1 AND resource_id=$2`, w, sync.Priority.ID).Scan(&version); err != nil || version != sync.Priority.Version {
				return ErrLease
			}
			return nil
		})
		if err != nil {
			return true, err
		}
		// Whether this returns success or loses its response, the saved intent stays
		// reconciling and the next iteration observes the remote state first.
		_ = s.PriorityBackend.WritePriority(ctx, w, external.Connection, external.Number, writeAction.Action, writeAction.Label)
	}
	if sync.State == "observing" || sync.State == "reconciling" {
		immediate = true
	}
	_ = saved
	return true, nil
}

func (s *Service) priorityInterval() int {
	if s.PriorityIntervalSeconds > 0 {
		return s.PriorityIntervalSeconds
	}
	return 60
}
