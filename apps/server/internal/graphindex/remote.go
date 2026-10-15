package graphindex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/ArcadeData/arcadedb-drivers/go/arcadedb"
	"github.com/ArcadeData/arcadedb-drivers/go/arcadedb/generated"
)

var errUnavailable = errors.New("graph unavailable")
var errIncomplete = errors.New("graph stream incomplete")
var errHistory = errors.New("graph projection history unavailable")

const graphSchema = "koujou-graph-v1"

type Remote struct {
	Server *arcadedb.Server
	DB     *arcadedb.Database
	Name   string
}

func NewRemote(address, user, password, name string) (*Remote, error) {
	if !regexp.MustCompile(`^[A-Za-z0-9_]+$`).MatchString(name) {
		return nil, errInput
	}
	server, err := arcadedb.NewServer(address, arcadedb.WithBasicAuth(user, password), arcadedb.WithHTTPClient(&http.Client{Timeout: 15 * time.Second}))
	if err != nil {
		return nil, err
	}
	return &Remote{server, server.DB(name), name}, nil
}
func (r *Remote) Init(ctx context.Context) error {
	exists, err := r.Server.Exists(ctx, r.Name)
	if err != nil {
		return err
	}
	if !exists {
		response, err := r.Server.Raw().ExecuteServerCommandWithResponse(ctx, nil, generated.CommandRequest{Language: "sql", Command: "create database " + r.Name})
		if err != nil {
			return err
		}
		if response.StatusCode() > 299 {
			return errUnavailable
		}
	}
	for _, sql := range []string{
		"CREATE VERTEX TYPE KoujouNode IF NOT EXISTS", "CREATE EDGE TYPE KoujouEdge IF NOT EXISTS", "CREATE DOCUMENT TYPE KoujouHeader IF NOT EXISTS",
		"CREATE PROPERTY KoujouNode.g IF NOT EXISTS STRING", "CREATE PROPERTY KoujouNode.k IF NOT EXISTS STRING", "CREATE PROPERTY KoujouNode.logical IF NOT EXISTS STRING",
		"CREATE PROPERTY KoujouEdge.g IF NOT EXISTS STRING", "CREATE PROPERTY KoujouEdge.k IF NOT EXISTS STRING", "CREATE PROPERTY KoujouEdge.f IF NOT EXISTS STRING", "CREATE PROPERTY KoujouEdge.t IF NOT EXISTS STRING", "CREATE PROPERTY KoujouEdge.owner IF NOT EXISTS STRING",
		"CREATE PROPERTY KoujouHeader.k IF NOT EXISTS STRING",
		"CREATE INDEX IF NOT EXISTS ON KoujouNode (g,k) UNIQUE", "CREATE INDEX IF NOT EXISTS ON KoujouNode (g,logical) NOTUNIQUE",
		"CREATE INDEX IF NOT EXISTS ON KoujouEdge (g,k) UNIQUE", "CREATE INDEX IF NOT EXISTS ON KoujouEdge (g,f,k) NOTUNIQUE", "CREATE INDEX IF NOT EXISTS ON KoujouEdge (g,t,k) NOTUNIQUE", "CREATE INDEX IF NOT EXISTS ON KoujouEdge (g,owner) NOTUNIQUE",
		"CREATE INDEX IF NOT EXISTS ON KoujouHeader (k) UNIQUE",
	} {
		if _, err = r.DB.Command(ctx, arcadedb.SQL, sql, nil); err != nil {
			return err
		}
	}
	return nil
}

type answer struct {
	Rows   []map[string]any
	Reason string
}

func read(ctx context.Context, db *arcadedb.Database, sql string, params map[string]any, limit int) (answer, error) {
	a := answer{Rows: []map[string]any{}}
	trailer := false
	for event, err := range db.QueryStream(ctx, arcadedb.SQL, sql, params, arcadedb.WithLimit(limit)) {
		if err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				a.Reason = "timeout"
			} else {
				a.Reason = "stream_incomplete"
			}
			return a, err
		}
		if event.Stats != nil {
			trailer = true
			if event.Stats.Truncated {
				a.Reason = "truncated"
			}
			if event.Stats.Returned != len(a.Rows) {
				a.Reason = "stream_incomplete"
			}
		} else {
			a.Rows = append(a.Rows, event.Record)
		}
	}
	if !trailer {
		a.Reason = "stream_incomplete"
		return a, errIncomplete
	}
	return a, nil
}
func decodeRow(row map[string]any, v any) error { return json.Unmarshal(encode(row), v) }

type header struct {
	G           string `json:"g"`
	W           string `json:"w"`
	Seq         string `json:"cp_sequence"`
	Pos         string `json:"cp_position"`
	Schema      string `json:"schema_version"`
	Active      string `json:"active"`
	Stage       string `json:"stage_status"`
	LoadedNodes int    `json:"loaded_nodes"`
	LoadedEdges int    `json:"loaded_edges"`
}

func activeKey(w string) string     { return "active-" + hash(w) }
func checkpointKey(g string) string { return "checkpoint-" + g }
func headerRead(ctx context.Context, db *arcadedb.Database, k string) (header, error) {
	a, err := read(ctx, db, "SELECT g,w,cp_sequence,cp_position,schema_version,active,stage_status,loaded_nodes,loaded_edges FROM KoujouHeader WHERE k=:k", map[string]any{"k": k}, 1)
	if err != nil || a.Reason != "" {
		return header{}, errUnavailable
	}
	if len(a.Rows) == 0 {
		return header{}, nil
	}
	var h header
	err = decodeRow(a.Rows[0], &h)
	return h, err
}
func (r *Remote) Active(ctx context.Context, w string) (header, error) {
	return headerRead(ctx, r.DB, activeKey(w))
}

const nodeScript = `FOREACH (row IN :nodes) { UPDATE KoujouNode SET g=$row.g,k=$row.k,w=$row.w,kind=$row.kind,id=$row.id,rev=$row.rev,form=$row.form,current=$row.current,deleted=$row.deleted,logical=$row.logical UPSERT WHERE g=$row.g AND k=$row.k; }; RETURN true;`
const edgeScript = `FOREACH (row IN :edges) { CREATE EDGE KoujouEdge FROM (SELECT FROM KoujouNode WHERE g=$row.g AND k=$row.f) TO (SELECT FROM KoujouNode WHERE g=$row.g AND k=$row.t) SET g=$row.g,k=$row.k,w=$row.w,f=$row.f,t=$row.t,kind=$row.kind,ord=$row.ord,owner=$row.owner,source=$row.source; }; RETURN true;`

type batch struct {
	Nodes           []vertex
	Edges           []arcEdge
	ReplaceOwners   []string
	CurrentLogicals []string
	DeletedLogicals []string
}

func writeBatch(ctx context.Context, db *arcadedb.Database, g string, b batch) error {
	if len(b.ReplaceOwners) > 0 {
		if _, err := db.Command(ctx, arcadedb.SQL, "DELETE FROM KoujouEdge WHERE g=:g AND owner IN :owners", map[string]any{"g": g, "owners": b.ReplaceOwners}); err != nil {
			return err
		}
	}
	if len(b.CurrentLogicals) > 0 {
		if _, err := db.Command(ctx, arcadedb.SQL, "UPDATE KoujouNode SET current=false WHERE g=:g AND logical IN :ids AND form='revision'", map[string]any{"g": g, "ids": b.CurrentLogicals}); err != nil {
			return err
		}
	}
	if len(b.DeletedLogicals) > 0 {
		if _, err := db.Command(ctx, arcadedb.SQL, "UPDATE KoujouNode SET current=false,deleted=true WHERE g=:g AND logical IN :ids", map[string]any{"g": g, "ids": b.DeletedLogicals}); err != nil {
			return err
		}
	}
	for start := 0; start < len(b.Nodes); start += 500 {
		end := min(start+500, len(b.Nodes))
		if _, err := db.Command(ctx, arcadedb.QueryLanguage("sqlscript"), nodeScript, map[string]any{"nodes": b.Nodes[start:end]}); err != nil {
			return err
		}
	}
	for start := 0; start < len(b.Edges); start += 500 {
		end := min(start+500, len(b.Edges))
		keys := []string{}
		for _, e := range b.Edges[start:end] {
			keys = append(keys, e.K)
		}
		existing, err := read(ctx, db, "SELECT k FROM KoujouEdge WHERE g=:g AND k IN :keys", map[string]any{"g": g, "keys": keys}, -1)
		if err != nil {
			return err
		}
		if existing.Reason != "" {
			return errIncomplete
		}
		seen := map[string]bool{}
		for _, row := range existing.Rows {
			seen[row["k"].(string)] = true
		}
		edges := []arcEdge{}
		for _, e := range b.Edges[start:end] {
			if !seen[e.K] {
				edges = append(edges, e)
				seen[e.K] = true
			}
		}
		if len(edges) == 0 {
			continue
		}
		if _, err := db.Command(ctx, arcadedb.QueryLanguage("sqlscript"), edgeScript, map[string]any{"edges": edges}); err != nil {
			return err
		}
	}
	return nil
}
func checkpointWrite(ctx context.Context, db *arcadedb.Database, w, g string, cp checkpoint) error {
	_, err := db.Command(ctx, arcadedb.SQL, "UPDATE KoujouHeader SET k=:k,g=:g,w=:w,cp_sequence=:cseq,cp_position=:cpos,schema_version=:sv,stage_status='ready' UPSERT WHERE k=:k", map[string]any{"k": checkpointKey(g), "g": g, "w": w, "cseq": fmt.Sprint(cp.Sequence), "cpos": cp.Position, "sv": graphSchema})
	return err
}
func (r *Remote) Commit(ctx context.Context, w, g string, b batch, previous, next checkpoint) error {
	return r.DB.Transaction(ctx, func(tx *arcadedb.Database) error {
		h, err := headerRead(ctx, tx, checkpointKey(g))
		if err != nil {
			return err
		}
		if h.Seq == fmt.Sprint(next.Sequence) && h.Pos == next.Position {
			return nil
		}
		if h.Seq != fmt.Sprint(previous.Sequence) || h.Pos != previous.Position {
			return errHistory
		}
		if err = writeBatch(ctx, tx, g, b); err != nil {
			return err
		}
		return checkpointWrite(ctx, tx, w, g, next)
	})
}
func (r *Remote) Close() error { return r.Server.Close() }
