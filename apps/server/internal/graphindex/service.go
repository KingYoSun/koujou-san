package graphindex

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ArcadeData/arcadedb-drivers/go/arcadedb"
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/jackc/pgx/v5"
)

type Service struct {
	Store       *persistence.Store
	Access      *access.Service
	Contexts    *contexts.Service
	Remote      *Remote
	Limits      Limits
	Key         []byte
	mu          sync.Mutex
	cancel      context.CancelFunc
	workers     sync.WaitGroup
	after       string
	initialized bool
}

func New(store *persistence.Store, a *access.Service, c *contexts.Service, remote *Remote, limits Limits, key []byte) *Service {
	return &Service{Store: store, Access: a, Contexts: c, Remote: remote, Limits: limits, Key: key}
}
func (s *Service) Start(ctx context.Context) {
	ctx, s.cancel = context.WithCancel(ctx)
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		timer := time.NewTicker(time.Second)
		defer timer.Stop()
		for {
			_ = s.Sync(ctx)
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
		}
	}()
}
func (s *Service) Close() error {
	if s.cancel != nil {
		s.cancel()
		s.workers.Wait()
	}
	if s.Remote != nil {
		return s.Remote.Close()
	}
	return nil
}
func (s *Service) init(ctx context.Context) error {
	if s.Remote == nil {
		return errUnavailable
	}
	if !s.initialized {
		if err := s.Remote.Init(ctx); err != nil {
			return err
		}
		s.initialized = true
	}
	return nil
}
func head(ctx context.Context, q database, w string) (checkpoint, error) {
	var cp checkpoint
	err := q.QueryRow(ctx, "SELECT sequence,position FROM shared_streams WHERE workspace_id=$1", w).Scan(&cp.Sequence, &cp.Position)
	return cp, err
}
func parseCheckpoint(h header) (checkpoint, error) {
	seq, err := strconv.ParseInt(h.Seq, 10, 64)
	if err != nil || h.Pos == "" || h.Schema != graphSchema || h.Stage != "ready" {
		return checkpoint{}, errHistory
	}
	return checkpoint{seq, h.Pos}, nil
}
func (s *Service) Sync(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.init(ctx); err != nil {
		return err
	}
	rows, err := s.Store.Database().Query(ctx, "SELECT workspace_id FROM shared_streams WHERE workspace_id>$1 ORDER BY workspace_id LIMIT 100", s.after)
	if err != nil {
		return err
	}
	workspaces := []string{}
	for rows.Next() {
		var w string
		if err = rows.Scan(&w); err != nil {
			break
		}
		workspaces = append(workspaces, w)
	}
	rows.Close()
	if err != nil {
		return err
	}
	if err = rows.Err(); err != nil {
		return err
	}
	s.after = ""
	if len(workspaces) == 100 {
		s.after = workspaces[99]
	}
	for _, w := range workspaces {
		h, err := s.Remote.Active(ctx, w)
		if err != nil {
			return err
		}
		if h.Active == "" || h.Schema != graphSchema {
			if err = s.rebuild(ctx, w); err != nil {
				return err
			}
			continue
		}
		target, err := head(ctx, s.Store.Database(), w)
		if err != nil {
			return err
		}
		err = s.syncTo(ctx, w, h.Active, target)
		if errors.Is(err, errHistory) {
			err = s.rebuild(ctx, w)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) syncTo(ctx context.Context, w, g string, target checkpoint) error {
	h, err := headerRead(ctx, s.Remote.DB, checkpointKey(g))
	if err != nil {
		return err
	}
	cp, err := parseCheckpoint(h)
	if err != nil {
		return err
	}
	if cp == target {
		return nil
	}
	if cp.Sequence >= target.Sequence {
		return errHistory
	}
	cursor := "c." + base64.RawURLEncoding.EncodeToString(encode(struct{ Workspace, After, Through string }{w, cp.Position, target.Position}))
	changes, err := s.Store.Changes(ctx, persistence.Scope{ActorID: "arcadedb", Grant: apicontract.Grant{WorkspaceID: w, CanRead: true}}, w, cursor, 100)
	if err != nil {
		return err
	}
	if changes.Error != nil {
		return errHistory
	}
	b := newBuilder(s.Store.Database(), w, g)
	out := batch{}
	next := cp
	last := map[string]int{}
	for i, event := range *changes.Data {
		last[event.ID] = i
	}
	for i, event := range *changes.Data {
		next.Sequence++
		next.Position = event.Position
		if (strings.HasPrefix(event.ID, "membership:") || strings.HasPrefix(event.ID, "relation:")) && last[event.ID] != i {
			continue
		}
		if err = b.event(ctx, &out, event.ID, event.Version, event.Deleted); err != nil {
			return err
		}
	}
	err = s.Remote.Commit(ctx, w, g, out, cp, next)
	if err != nil {
		observed, readErr := headerRead(ctx, s.Remote.DB, checkpointKey(g))
		if readErr == nil && observed.Seq == fmt.Sprint(next.Sequence) && observed.Pos == next.Position {
			return nil
		}
	}
	return err
}
func (s *Service) Rebuild(ctx context.Context, w string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.init(ctx); err != nil {
		return err
	}
	return s.rebuild(ctx, w)
}
func (s *Service) rebuild(ctx context.Context, w string) error {
	old, err := s.Remote.Active(ctx, w)
	if err != nil {
		return err
	}
	g := rand.Text()
	var cp checkpoint
	var nodes, edges int
	err = pgx.BeginTxFunc(ctx, s.Store.Database(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var err error
		cp, err = head(ctx, tx, w)
		if err != nil {
			return err
		}
		b := newBuilder(tx, w, g)
		flush := func(out batch) error {
			out.ReplaceOwners = nil
			return s.Remote.DB.Transaction(ctx, func(db *arcadedb.Database) error {
				if err := writeBatch(ctx, db, g, out); err != nil {
					return err
				}
				_, err := db.Command(ctx, arcadedb.SQL, "UPDATE KoujouHeader SET k=:k,g=:g,w=:w,cp_sequence=:cs,cp_position=:cp,schema_version=:sv,stage_status='loading',loaded_nodes=:nn,loaded_edges=:ne UPSERT WHERE k=:k", map[string]any{"k": checkpointKey(g), "g": g, "w": w, "cs": fmt.Sprint(cp.Sequence), "cp": cp.Position, "sv": graphSchema, "nn": b.NodeCount, "ne": b.EdgeCount})
				return err
			})
		}
		afterID, afterRevision := "", ""
		for {
			rows, err := tx.Query(ctx, `SELECT r.kind,r.resource_id,r.revision,r.payload FROM context_records r WHERE r.workspace_id=$1 AND r.kind IN ('sentence','paragraph','section','document','scope') AND (r.resource_id,r.revision)>($2,$3) ORDER BY r.resource_id,r.revision LIMIT 500`, w, afterID, afterRevision)
			if err != nil {
				return err
			}
			out := batch{}
			count := 0
			type record struct {
				kind, id, version string
				raw               []byte
			}
			page := []record{}
			for rows.Next() {
				var r record
				if err = rows.Scan(&r.kind, &r.id, &r.version, &r.raw); err != nil {
					break
				}
				page = append(page, r)
			}
			rows.Close()
			if err != nil {
				return err
			}
			if err = rows.Err(); err != nil {
				return err
			}
			for _, r := range page {
				ref := contexts.Ref{Kind: r.kind, ID: strings.TrimPrefix(r.id, r.kind+":"), Revision: r.version}
				if unitKind(r.kind) {
					ref.ID = r.id[len("context:"):]
				}
				if err = b.content(ctx, &out, ref, r.raw); err != nil {
					return err
				}
				afterID, afterRevision = r.id, r.version
				count++
			}
			if err = flush(out); err != nil {
				return err
			}
			if count < 500 {
				break
			}
		}
		for _, kind := range []string{"membership", "relation"} {
			after := ""
			for {
				sql := `SELECT r.resource_id,r.revision,r.payload FROM context_records r JOIN shared_versions v USING(workspace_id,resource_id) WHERE r.workspace_id=$1 AND r.kind='membership' AND NOT v.deleted AND r.revision=v.version AND r.resource_id>$2 ORDER BY r.resource_id LIMIT 500`
				if kind == "relation" {
					sql = `SELECT r.id,r.revision,r.payload FROM graph_relations r JOIN shared_versions v ON v.workspace_id=r.workspace_id AND v.resource_id='relation:'||r.id AND v.version=r.revision WHERE r.workspace_id=$1 AND NOT v.deleted AND r.id>$2 ORDER BY r.id LIMIT 500`
				}
				rows, err := tx.Query(ctx, sql, w, after)
				if err != nil {
					return err
				}
				type record struct {
					id, rev string
					raw     []byte
				}
				page := []record{}
				for rows.Next() {
					var r record
					if err = rows.Scan(&r.id, &r.rev, &r.raw); err != nil {
						break
					}
					page = append(page, r)
				}
				rows.Close()
				if err != nil {
					return err
				}
				if err = rows.Err(); err != nil {
					return err
				}
				out := batch{}
				for _, r := range page {
					if kind == "membership" {
						err = b.membership(ctx, &out, r.id[len("membership:"):], r.rev, r.raw)
					} else {
						err = b.relation(ctx, &out, r.id, r.rev, r.raw)
					}
					if err != nil {
						return err
					}
					after = r.id
				}
				if err = flush(out); err != nil {
					return err
				}
				if len(page) < 500 {
					break
				}
			}
		}
		afterRun := ""
		for {
			rows, err := tx.Query(ctx, "SELECT resource_id,version FROM shared_versions WHERE workspace_id=$1 AND resource_id LIKE 'run:%' AND NOT deleted AND resource_id>$2 ORDER BY resource_id LIMIT 500", w, afterRun)
			if err != nil {
				return err
			}
			refs := []contexts.Ref{}
			for rows.Next() {
				var id, rev string
				if err = rows.Scan(&id, &rev); err != nil {
					break
				}
				refs = append(refs, contexts.Ref{Kind: "run", ID: strings.TrimPrefix(id, "run:"), Revision: rev})
				afterRun = id
			}
			rows.Close()
			if err != nil {
				return err
			}
			if err = rows.Err(); err != nil {
				return err
			}
			out := batch{}
			for _, ref := range refs {
				if err = b.node(ctx, &out, ref); err != nil {
					return err
				}
			}
			if err = flush(out); err != nil {
				return err
			}
			if len(refs) < 500 {
				break
			}
		}
		nodes, edges = b.NodeCount, b.EdgeCount
		return nil
	})
	if err != nil {
		return err
	}
	for _, check := range []struct {
		table string
		count int
	}{{"KoujouNode", nodes}, {"KoujouEdge", edges}} {
		a, err := read(ctx, s.Remote.DB, "SELECT count(*) AS n FROM "+check.table+" WHERE g=:g", map[string]any{"g": g}, 1)
		if err != nil || a.Reason != "" || len(a.Rows) != 1 || a.Rows[0]["n"] != float64(check.count) {
			return errIncomplete
		}
	}
	if err = checkpointWrite(ctx, s.Remote.DB, w, g, cp); err != nil {
		return err
	}
	target, err := head(ctx, s.Store.Database(), w)
	if err != nil {
		return err
	}
	for cp != target {
		if err = s.syncTo(ctx, w, g, target); err != nil {
			return err
		}
		h, err := headerRead(ctx, s.Remote.DB, checkpointKey(g))
		if err != nil {
			return err
		}
		cp, err = parseCheckpoint(h)
		if err != nil {
			return err
		}
	}
	err = s.Remote.DB.Transaction(ctx, func(tx *arcadedb.Database) error {
		active, err := headerRead(ctx, tx, activeKey(w))
		if err != nil {
			return err
		}
		if active.Active != old.Active {
			return errHistory
		}
		_, err = tx.Command(ctx, arcadedb.SQL, "UPDATE KoujouHeader SET k=:k,w=:w,active=:g,schema_version=:sv UPSERT WHERE k=:k", map[string]any{"k": activeKey(w), "w": w, "g": g, "sv": graphSchema})
		return err
	})
	if err != nil {
		observed, readErr := s.Remote.Active(ctx, w)
		if readErr == nil && observed.Active == g {
			return nil
		}
	}
	return err
}
