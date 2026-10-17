package githubobserve

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/jackc/pgx/v5"
)

type Service struct {
	store  *persistence.Store
	access *access.Service
	config *Config
	remote *Remote
	wake   chan struct{}
	mu     sync.Mutex
}

func New(ctx context.Context, store *persistence.Store, a *access.Service, c *Config) (*Service, error) {
	if c == nil {
		return &Service{store: store, access: a}, nil
	}
	s := &Service{store: store, access: a, config: c, remote: newRemote(c), wake: make(chan struct{}, 1)}
	err := pgx.BeginFunc(ctx, store.Database(), func(tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, "UPDATE github_targets SET active=false; DELETE FROM github_bindings"); e != nil {
			return e
		}
		for _, t := range c.Targets {
			raw, _ := json.Marshal(t)
			tag, e := tx.Exec(ctx, `INSERT INTO github_targets(id,spec,installation_id,repository_id) VALUES($1,$2,$3,$4) ON CONFLICT(id) DO UPDATE SET active=true,due_at=now() WHERE github_targets.spec=excluded.spec`, t.ID, raw, t.InstallationID, t.RepositoryID)
			if e != nil {
				return e
			}
			if tag.RowsAffected() != 1 {
				return errors.New("GitHub target identity changed; use a new target ID")
			}
		}
		for _, b := range c.Bindings {
			jobs, _ := json.Marshal(b.RequiredJobs)
			if _, e := tx.Exec(ctx, `INSERT INTO github_bindings VALUES($1,$2,$3,$4)`, b.WorkspaceID, b.TargetID, b.ExpectedCommit, jobs); e != nil {
				return e
			}
			if _, e := tx.Exec(ctx, `INSERT INTO access_resources(workspace_id,id,restricted) VALUES($1,$2,false) ON CONFLICT DO NOTHING`, b.WorkspaceID, "github:"+b.TargetID); e != nil {
				return e
			}
		}
		return nil
	})
	return s, err
}
func (s *Service) Start(ctx context.Context) {
	if s.config == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			// Drain due targets in indexed order; only one worker calls GitHub.
			for {
				worked, err := s.step(ctx)
				if err != nil || !worked {
					break
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			case <-s.wake:
			}
		}
	}()
}
func (s *Service) step(ctx context.Context) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if time.Now().Before(s.remote.resumeAt) {
		return false, nil
	}
	var raw []byte
	err := s.store.Database().QueryRow(ctx, `SELECT spec FROM github_targets WHERE active AND due_at<=now() ORDER BY due_at,id LIMIT 1`).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var t Target
	if json.Unmarshal(raw, &t) != nil {
		return false, errors.New("invalid stored target")
	}
	snapshot, observeErr := s.remote.observe(ctx, t)
	checked := time.Now().UTC()
	next := checked.Add(s.config.interval())
	if s.remote.resumeAt.After(next) {
		next = s.remote.resumeAt
	}
	err = pgx.BeginFunc(ctx, s.store.Database(), func(tx pgx.Tx) error {
		if observeErr != nil {
			_, e := tx.Exec(ctx, `UPDATE github_targets SET checked_at=$2,reason=$3,due_at=$4 WHERE id=$1`, t.ID, checked, observeErr.Error(), next)
			return e
		}
		value, _ := json.Marshal(snapshot)
		var version int64
		var previous []byte
		if e := tx.QueryRow(ctx, `SELECT version,snapshot FROM github_targets WHERE id=$1 FOR UPDATE`, t.ID).Scan(&version, &previous); e != nil {
			return e
		}
		var old Snapshot
		_ = json.Unmarshal(previous, &old)
		if old.SourceVersion != snapshot.SourceVersion {
			version++
			if _, e := tx.Exec(ctx, `INSERT INTO github_observations VALUES($1,$2,$3,$4)`, t.ID, version, checked, value); e != nil {
				return e
			}
		}
		_, e := tx.Exec(ctx, `UPDATE github_targets SET snapshot=$2,version=$3,checked_at=$4,observed_at=$4,reason='',due_at=$5 WHERE id=$1`, t.ID, value, version, checked, next)
		return e
	})
	return true, err
}

type View struct {
	Target         Target     `json:"target"`
	ExpectedCommit string     `json:"expected_commit,omitempty"`
	RequiredJobs   []string   `json:"required_jobs,omitempty"`
	State          string     `json:"observation_state"`
	Reason         string     `json:"reason,omitempty"`
	CheckedAt      *time.Time `json:"checked_at"`
	ObservedAt     *time.Time `json:"observed_at"`
	DueAt          time.Time  `json:"due_at"`
	Delayed        bool       `json:"delayed"`
	Version        int64      `json:"version"`
	Snapshot       *Snapshot  `json:"snapshot"`
}

// Current performs a read-only observation now, for an explicitly bound target.
// A cached or delayed success is never sufficient to authorize an external write.
func (s *Service) Current(ctx context.Context, p access.Principal, w, id string) (Snapshot, error) {
	if err := s.access.Check(ctx, p, w, "github:"+id, "get"); err != nil {
		return Snapshot{}, err
	}
	if s.config == nil {
		return Snapshot{}, errors.New("not_configured")
	}
	var source []byte
	if err := s.store.Database().QueryRow(ctx, `SELECT t.spec FROM github_targets t JOIN github_bindings b ON b.target_id=t.id WHERE b.workspace_id=$1 AND t.id=$2 AND t.active`, w, id).Scan(&source); err != nil {
		return Snapshot{}, err
	}
	var target Target
	if json.Unmarshal(source, &target) != nil {
		return Snapshot{}, errors.New("invalid_target")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.remote.observe(ctx, target)
}

func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /github/workspaces/{workspace}/observations", s.listHTTP)
	mux.HandleFunc("GET /github/workspaces/{workspace}/observations/{target}/history", s.historyHTTP)
	mux.HandleFunc("POST /github/webhook", s.webhookHTTP)
}
func send(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func (s *Service) authorized(w http.ResponseWriter, r *http.Request, target string) bool {
	id, ok := s.access.User(w, r)
	if !ok {
		return false
	}
	action := "read"
	if target != "" {
		action = "get"
		target = "github:" + target
	}
	if s.access.Check(r.Context(), access.Principal{UserID: id}, r.PathValue("workspace"), target, action) != nil {
		send(w, 403, map[string]string{"error": "forbidden"})
		return false
	}
	return true
}
func (s *Service) listHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(w, r, "") {
		return
	}
	if s.config == nil {
		send(w, 503, map[string]string{"error": "not_configured"})
		return
	}
	workspace := r.PathValue("workspace")
	rows, err := s.store.Database().Query(r.Context(), `SELECT t.spec,b.expected_commit,b.required_jobs,t.checked_at,t.observed_at,t.due_at,t.reason,t.version,t.snapshot FROM github_bindings b JOIN github_targets t ON t.id=b.target_id WHERE b.workspace_id=$1 AND t.active AND t.id>$2 ORDER BY t.id LIMIT 51`, workspace, r.URL.Query().Get("after"))
	if err != nil {
		send(w, 503, map[string]string{"error": "unavailable"})
		return
	}
	defer rows.Close()
	items := []View{}
	// Denied rows are omitted; the cursor advances over a bounded inspected page.
	cursor := ""
	more := false
	count := 0
	id, _ := s.access.User(w, r)
	for rows.Next() {
		var v View
		var spec, jobs, snapshot []byte
		if rows.Scan(&spec, &v.ExpectedCommit, &jobs, &v.CheckedAt, &v.ObservedAt, &v.DueAt, &v.Reason, &v.Version, &snapshot) != nil {
			send(w, 503, map[string]string{"error": "unavailable"})
			return
		}
		_ = json.Unmarshal(spec, &v.Target)
		if count == 50 {
			more = true
			break
		}
		count++
		cursor = v.Target.ID
		if s.access.Check(r.Context(), access.Principal{UserID: id}, workspace, "github:"+v.Target.ID, "get") != nil {
			continue
		}
		_ = json.Unmarshal(jobs, &v.RequiredJobs)
		if len(snapshot) > 0 {
			v.Snapshot = &Snapshot{}
			_ = json.Unmarshal(snapshot, v.Snapshot)
		}
		v.State = "observed"
		if v.Reason != "" {
			v.State = "unknown"
		}
		if v.ObservedAt == nil {
			v.State = "not_fetched"
		}
		v.Delayed = time.Now().After(v.DueAt.Add(s.config.interval()))
		items = append(items, v)
	}
	if rows.Err() != nil {
		send(w, 503, map[string]string{"error": "unavailable"})
		return
	}
	var next *string
	if more {
		next = &cursor
	}
	send(w, 200, apicontract.Response[[]View]{Data: &items, Meta: apicontract.Meta{WorkspaceID: workspace, NextCursor: next, Completeness: apicontract.Completeness{State: "complete"}}})
}
func (s *Service) historyHTTP(w http.ResponseWriter, r *http.Request) {
	if s.config == nil {
		send(w, 503, map[string]string{"error": "not_configured"})
		return
	}
	if !s.authorized(w, r, r.PathValue("target")) {
		return
	}
	before := int64(1<<63 - 1)
	if raw := r.URL.Query().Get("before"); raw != "" {
		n, e := strconv.ParseInt(raw, 10, 64)
		if e != nil || n < 1 {
			send(w, 400, map[string]string{"error": "invalid_cursor"})
			return
		}
		before = n
	}
	rows, err := s.store.Database().Query(r.Context(), `SELECT h.version,h.observed_at,h.snapshot FROM github_observations h JOIN github_bindings b ON b.target_id=h.target_id WHERE b.workspace_id=$1 AND h.target_id=$2 AND h.version<$3 ORDER BY h.version DESC LIMIT 51`, r.PathValue("workspace"), r.PathValue("target"), before)
	if err != nil {
		send(w, 503, map[string]string{"error": "unavailable"})
		return
	}
	defer rows.Close()
	type entry struct {
		Version    int64           `json:"version"`
		ObservedAt time.Time       `json:"observed_at"`
		Snapshot   json.RawMessage `json:"snapshot"`
	}
	items := []entry{}
	for rows.Next() {
		var v entry
		if rows.Scan(&v.Version, &v.ObservedAt, &v.Snapshot) != nil {
			send(w, 503, map[string]string{"error": "unavailable"})
			return
		}
		items = append(items, v)
	}
	if rows.Err() != nil {
		send(w, 503, map[string]string{"error": "unavailable"})
		return
	}
	next := ""
	if len(items) > 50 {
		items = items[:50]
		next = strconv.FormatInt(items[49].Version, 10)
	}
	send(w, 200, map[string]any{"items": items, "next_cursor": next})
}
func (s *Service) webhookHTTP(w http.ResponseWriter, r *http.Request) {
	if s.config == nil || len(s.config.secret) == 0 {
		w.WriteHeader(404)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 25<<20))
	if err != nil {
		w.WriteHeader(400)
		return
	}
	mac := hmac.New(sha256.New, s.config.secret)
	_, _ = mac.Write(body)
	signature, e := hex.DecodeString(strings.TrimPrefix(r.Header.Get("X-Hub-Signature-256"), "sha256="))
	if e != nil || !hmac.Equal(signature, mac.Sum(nil)) {
		w.WriteHeader(401)
		return
	}
	var event struct {
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
		Repository struct {
			ID int64 `json:"id"`
		} `json:"repository"`
	}
	if json.Unmarshal(body, &event) != nil {
		w.WriteHeader(400)
		return
	}
	// Payload contents are never adopted as observation results. A duplicate or
	// out-of-order delivery only makes the same indexed repository due again.
	_, err = s.store.Database().Exec(r.Context(), `UPDATE github_targets SET due_at=LEAST(due_at,now()) WHERE active AND installation_id=$1 AND repository_id=$2`, event.Installation.ID, event.Repository.ID)
	if err != nil {
		w.WriteHeader(503)
		return
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
	w.WriteHeader(202)
}
