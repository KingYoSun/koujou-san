package searchindex

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/blevesearch/bleve/v2"
	"github.com/jackc/pgx/v5"
)

var errHistory = errors.New("projection history unavailable")

type Service struct {
	Store      *persistence.Store
	Access     *access.Service
	Contexts   *contexts.Service
	dir        string
	crypt      cipher.AEAD
	mu         sync.RWMutex
	update     sync.Mutex
	index      bleve.Index
	generation string
	after      string
	cancel     context.CancelFunc
	workers    sync.WaitGroup
}

func New(store *persistence.Store, a *access.Service, c *contexts.Service, dir string, key []byte) *Service {
	if dir == "" {
		dir = ".cache/search"
	}
	sum := sha256.Sum256(append([]byte("koujou-search-cursor-v1:"), key...))
	block, _ := aes.NewCipher(sum[:])
	crypt, _ := cipher.NewGCM(block)
	s := &Service{Store: store, Access: a, Contexts: c, dir: dir, crypt: crypt}
	raw, err := os.ReadFile(filepath.Join(dir, "active.json"))
	var active generation
	if err == nil && json.Unmarshal(raw, &active) == nil && active.Schema == schema {
		if path, ok := s.path(active.Name); ok {
			if i, err := bleve.Open(path); err == nil {
				s.index = i
				s.generation = active.Name
			}
		}
	}
	return s
}
func (s *Service) path(name string) (string, bool) {
	if !strings.HasPrefix(name, "gen-") || strings.ContainsAny(name, `/\`) {
		return "", false
	}
	return filepath.Join(s.dir, name), true
}
func (s *Service) Start(ctx context.Context, rebuild bool) {
	ctx, s.cancel = context.WithCancel(ctx)
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		if rebuild {
			_ = s.Rebuild(ctx)
		}
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			_ = s.Sync(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
func (s *Service) Close() error {
	if s.cancel != nil {
		s.cancel()
		s.workers.Wait()
	}
	s.update.Lock()
	defer s.update.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.index != nil {
		err := s.index.Close()
		s.index = nil
		return err
	}
	return nil
}
func (s *Service) seal(c cursor) string {
	nonce := make([]byte, s.crypt.NonceSize())
	_, _ = rand.Read(nonce)
	return base64.RawURLEncoding.EncodeToString(s.crypt.Seal(nonce, nonce, encode(c), []byte(schema)))
}
func (s *Service) open(raw string) (cursor, error) {
	var c cursor
	data, err := base64.RawURLEncoding.DecodeString(raw)
	n := s.crypt.NonceSize()
	if err != nil || len(data) < n {
		return c, errors.New("invalid cursor")
	}
	plain, err := s.crypt.Open(nil, data[:n], data[n:], []byte(schema))
	if err == nil {
		err = json.Unmarshal(plain, &c)
	}
	return c, err
}
func (s *Service) workspaces(ctx context.Context, db database, after string) ([]string, error) {
	rows, err := db.Query(ctx, "SELECT workspace_id FROM shared_streams WHERE workspace_id>$1 ORDER BY workspace_id LIMIT 100", after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []string{}
	for rows.Next() {
		var w string
		if err = rows.Scan(&w); err != nil {
			return nil, err
		}
		result = append(result, w)
	}
	return result, rows.Err()
}
func head(ctx context.Context, db database, w string) (checkpoint, string, error) {
	var cp checkpoint
	var origin string
	err := db.QueryRow(ctx, "SELECT sequence,position,origin FROM shared_streams WHERE workspace_id=$1", w).Scan(&cp.Sequence, &cp.Position, &origin)
	return cp, origin, err
}
func (s *Service) Sync(ctx context.Context) error {
	s.update.Lock()
	defer s.update.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	s.mu.RLock()
	missing := s.index == nil
	s.mu.RUnlock()
	if missing {
		return s.rebuild(ctx)
	}
	workspaces, err := s.workspaces(ctx, s.Store.Database(), s.after)
	if err != nil {
		return err
	}
	if len(workspaces) < 100 {
		s.after = ""
	} else {
		s.after = workspaces[len(workspaces)-1]
	}
	for _, w := range workspaces {
		s.mu.Lock()
		err = s.syncWorkspace(ctx, s.index, w)
		s.mu.Unlock()
		if errors.Is(err, errHistory) {
			return s.rebuild(ctx)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) syncWorkspace(ctx context.Context, i bleve.Index, w string) error {
	current, _, err := head(ctx, s.Store.Database(), w)
	if err != nil {
		return err
	}
	return s.syncTo(ctx, s.Store.Database(), i, w, current)
}
func (s *Service) syncTo(ctx context.Context, db database, i bleve.Index, w string, current checkpoint) error {
	cp, err := readCheckpoint(i, w)
	if err != nil {
		return err
	}
	_, origin, err := head(ctx, db, w)
	if err != nil {
		return err
	}
	if cp.Position == "" {
		cp.Position = origin
	}
	if cp.Sequence == current.Sequence && cp.Position == current.Position {
		return nil
	}
	if cp.Sequence >= current.Sequence {
		return errHistory
	}
	after := "c." + base64.RawURLEncoding.EncodeToString(encode(struct{ Workspace, After, Through string }{w, cp.Position, current.Position}))
	changes, err := s.Store.Changes(ctx, persistence.Scope{ActorID: "bleve", Grant: apicontract.Grant{WorkspaceID: w, CanRead: true}}, w, after, 100)
	if err != nil {
		return err
	}
	if changes.Error != nil {
		return errHistory
	}
	b := builder{db, map[string]contexts.Unit{}}
	for _, event := range *changes.Data {
		cp.Sequence++
		if err = s.apply(ctx, &b, i, w, cp.Sequence, event); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) apply(ctx context.Context, b *builder, i bleve.Index, w string, sequence int64, event persistence.Change) error {
	cp, err := readCheckpoint(i, w)
	if err != nil {
		return err
	}
	if sequence <= cp.Sequence {
		return nil
	}
	if sequence != cp.Sequence+1 {
		return errHistory
	}
	cp.Sequence = sequence
	batch := i.NewBatch()
	resource := event.ID
	if strings.HasPrefix(resource, "membership:") {
		var raw []byte
		var membership contexts.Membership
		err = b.db.QueryRow(ctx, "SELECT payload FROM context_records WHERE workspace_id=$1 AND resource_id=$2 AND stream_sequence<=$3 ORDER BY stream_sequence DESC LIMIT 1", w, resource, cp.Sequence).Scan(&raw)
		if err != nil {
			return err
		}
		if err = json.Unmarshal(raw, &membership); err != nil {
			return err
		}
		resource = "context:" + membership.Context.ID
	}
	if strings.HasPrefix(resource, "context:") {
		deleted := event.Deleted
		if strings.HasPrefix(event.ID, "membership:") {
			if err = b.db.QueryRow(ctx, "SELECT deleted FROM shared_versions WHERE workspace_id=$1 AND resource_id=$2", w, resource).Scan(&deleted); err != nil {
				return err
			}
		}
		if err = b.project(ctx, i, batch, w, resource, cp.Sequence, deleted); err != nil {
			return err
		}
	}
	cp.Position = event.Position
	batch.SetInternal(checkpointKey(w), encode(cp))
	if err = i.Batch(batch); err != nil {
		return err
	}
	return nil
}

// Rebuild uses a separate generation. Failed candidates never replace active.
func (s *Service) Rebuild(ctx context.Context) error {
	s.update.Lock()
	defer s.update.Unlock()
	return s.rebuild(ctx)
}
func (s *Service) rebuild(ctx context.Context) error {
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return err
	}
	name := "gen-" + rand.Text()
	path, _ := s.path(name)
	candidate, err := bleve.NewUsing(path, indexMapping(), "scorch", "boltdb", nil)
	if err != nil {
		return err
	}
	accepted := false
	defer func() {
		if !accepted {
			_ = candidate.Close()
			_ = os.RemoveAll(path)
		}
	}()
	if err = s.snapshot(ctx, candidate); err != nil {
		return err
	}
	// Keep old queries available during the bulk copy. Lock only the final catchup,
	// validation and switch; new PostgreSQL writes remain independent throughout.
	s.mu.Lock()
	defer s.mu.Unlock()
	err = pgx.BeginTxFunc(ctx, s.Store.Database(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		after := ""
		for {
			workspaces, err := s.workspaces(ctx, tx, after)
			if err != nil {
				return err
			}
			for _, w := range workspaces {
				cp, err := readCheckpoint(candidate, w)
				if err != nil {
					return err
				}
				target, _, err := head(ctx, tx, w)
				if err != nil {
					return err
				}
				for cp.Sequence != target.Sequence {
					err = s.syncTo(ctx, tx, candidate, w, target)
					if errors.Is(err, errHistory) {
						err = s.snapshotWorkspace(ctx, tx, candidate, w, target)
					}
					if err != nil {
						return err
					}
					cp, err = readCheckpoint(candidate, w)
					if err != nil {
						return err
					}
				}
			}
			if len(workspaces) < 100 {
				break
			}
			after = workspaces[len(workspaces)-1]
		}
		return s.validate(ctx, tx, candidate)
	})
	if err != nil {
		return err
	}
	marker := filepath.Join(s.dir, "active.json")
	temp := marker + ".tmp"
	if err = os.WriteFile(temp, encode(generation{schema, name}), 0600); err != nil {
		return err
	}
	if err = os.Rename(temp, marker); err != nil {
		return err
	}
	old, oldName := s.index, s.generation
	s.index = candidate
	s.generation = name
	accepted = true
	if old != nil {
		_ = old.Close()
		if oldPath, ok := s.path(oldName); ok {
			_ = os.RemoveAll(oldPath)
		}
	}
	return nil
}
func (s *Service) snapshot(ctx context.Context, i bleve.Index) error {
	return pgx.BeginTxFunc(ctx, s.Store.Database(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		after := ""
		for {
			workspaces, err := s.workspaces(ctx, tx, after)
			if err != nil {
				return err
			}
			for _, w := range workspaces {
				cp, _, err := head(ctx, tx, w)
				if err != nil {
					return err
				}
				if err = s.snapshotWorkspace(ctx, tx, i, w, cp); err != nil {
					return err
				}
			}
			if len(workspaces) < 100 {
				return nil
			}
			after = workspaces[len(workspaces)-1]
		}
	})
}
func (s *Service) snapshotWorkspace(ctx context.Context, db database, i bleve.Index, w string, cp checkpoint) error {
	b := builder{db, map[string]contexts.Unit{}}
	batch := i.NewBatch()
	after := ""
	for {
		rows, err := db.Query(ctx, `SELECT DISTINCT r.resource_id,v.deleted FROM context_records r JOIN shared_versions v USING(workspace_id,resource_id) WHERE r.workspace_id=$1 AND r.kind IN ('sentence','paragraph','section','document') AND r.resource_id>$2 ORDER BY r.resource_id LIMIT 100`, w, after)
		if err != nil {
			return err
		}
		type item struct {
			id      string
			deleted bool
		}
		page := []item{}
		for rows.Next() {
			var r item
			if err = rows.Scan(&r.id, &r.deleted); err != nil {
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
			if err = b.project(ctx, i, batch, w, r.id, cp.Sequence, r.deleted); err != nil {
				return err
			}
		}
		if batch.Size() > 0 {
			if err = i.Batch(batch); err != nil {
				return err
			}
			batch.Reset()
		}
		if len(page) < 100 {
			break
		}
		after = page[len(page)-1].id
	}
	batch.SetInternal(checkpointKey(w), encode(cp))
	return i.Batch(batch)
}
func (s *Service) validate(ctx context.Context, db database, i bleve.Index) error {
	var expected uint64
	if err := db.QueryRow(ctx, `SELECT count(*) FROM context_records r JOIN shared_versions v USING(workspace_id,resource_id) WHERE NOT v.deleted AND r.kind IN ('sentence','paragraph','section','document')`).Scan(&expected); err != nil {
		return err
	}
	actual, err := i.DocCount()
	if err != nil {
		return err
	}
	if actual != expected {
		return errors.New("projection count mismatch")
	}
	afterW, afterResource, afterRevision := "", "", ""
	for {
		rows, err := db.Query(ctx, `SELECT r.workspace_id,r.kind,r.resource_id,r.revision FROM context_records r JOIN shared_versions v USING(workspace_id,resource_id) WHERE NOT v.deleted AND r.kind IN ('sentence','paragraph','section','document') AND (r.workspace_id,r.resource_id,r.revision)>($1,$2,$3) ORDER BY r.workspace_id,r.resource_id,r.revision LIMIT 100`, afterW, afterResource, afterRevision)
		if err != nil {
			return err
		}
		count := 0
		for rows.Next() {
			var w, k, id, v string
			if err = rows.Scan(&w, &k, &id, &v); err != nil {
				break
			}
			doc, readErr := i.Document(docID(w, contexts.Ref{Kind: k, ID: strings.TrimPrefix(id, "context:"), Revision: v}))
			if readErr != nil || doc == nil {
				err = errors.New("projection version mismatch")
				break
			}
			afterW, afterResource, afterRevision = w, id, v
			count++
		}
		rows.Close()
		if err != nil {
			return err
		}
		if err = rows.Err(); err != nil {
			return err
		}
		if count < 100 {
			return nil
		}
	}
}
