package searchindex

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/blevesearch/bleve/v2"
	"github.com/jackc/pgx/v5"
)

type database interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}
type builder struct {
	db    database
	cache map[string]contexts.Unit
}

func (b *builder) unit(ctx context.Context, w string, r contexts.Ref) (contexts.Unit, error) {
	key := docID(w, r)
	if u, ok := b.cache[key]; ok {
		return u, nil
	}
	var raw []byte
	var u contexts.Unit
	err := b.db.QueryRow(ctx, "SELECT payload FROM context_records WHERE workspace_id=$1 AND resource_id=$2 AND revision=$3", w, "context:"+r.ID, r.Revision).Scan(&raw)
	if err == nil {
		err = json.Unmarshal(raw, &u)
	}
	if err == nil {
		if len(b.cache) >= 1000 {
			clear(b.cache)
		}
		b.cache[key] = u
	}
	return u, err
}
func (b *builder) text(ctx context.Context, w string, r contexts.Ref) (string, []contexts.Ref, error) {
	text := []string{}
	dependencies := []contexts.Ref{}
	seen := map[string]bool{}
	var visit func(contexts.Ref) error
	visit = func(ref contexts.Ref) error {
		key := docID(w, ref)
		if seen[key] {
			return nil
		}
		seen[key] = true
		u, err := b.unit(ctx, w, ref)
		if err != nil {
			return err
		}
		if u.Kind != ref.Kind {
			return fmt.Errorf("projection reference type mismatch")
		}
		dependencies = append(dependencies, ref)
		text = append(text, u.Name, u.Title)
		if u.Text != nil {
			text = append(text, *u.Text)
		}
		if u.External != nil && u.External.State == "ready" && len(u.External.Snapshot) > 0 {
			text = append(text, string(u.External.Snapshot))
		}
		for _, child := range u.Children {
			if err := visit(child); err != nil {
				return err
			}
		}
		return nil
	}
	err := visit(r)
	return strings.Join(text, " "), dependencies, err
}
func scopes(ctx context.Context, db database, w, id string, through int64) ([]string, error) {
	rows, err := db.Query(ctx, `SELECT payload FROM (SELECT DISTINCT ON(resource_id) resource_id,payload FROM context_records WHERE workspace_id=$1 AND kind='membership' AND stream_sequence<=$3 AND resource_id IN (SELECT resource_id FROM context_records WHERE workspace_id=$1 AND kind='membership' AND payload->'context'->>'id'=$2) ORDER BY resource_id,stream_sequence DESC) r WHERE (payload->>'present')::boolean AND payload->'context'->>'id'=$2`, w, id, through)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []string{}
	for rows.Next() {
		var raw []byte
		var m contexts.Membership
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &m); err != nil {
			return nil, err
		}
		result = append(result, scopeID(m.Scope))
	}
	return result, rows.Err()
}
func (b *builder) project(ctx context.Context, i bleve.Index, batch *bleve.Batch, w, resource string, through int64, deleted bool) error {
	if !strings.HasPrefix(resource, "context:") {
		return nil
	}
	id := strings.TrimPrefix(resource, "context:")
	memberships, err := scopes(ctx, b.db, w, id, through)
	if err != nil {
		return err
	}
	var head string
	if err = b.db.QueryRow(ctx, "SELECT revision FROM context_records WHERE workspace_id=$1 AND resource_id=$2 AND stream_sequence<=$3 ORDER BY stream_sequence DESC LIMIT 1", w, resource, through).Scan(&head); err != nil {
		return err
	}
	after := ""
	for {
		rows, err := b.db.Query(ctx, "SELECT kind,revision,payload FROM context_records WHERE workspace_id=$1 AND resource_id=$2 AND stream_sequence<=$3 AND revision>$4 ORDER BY revision LIMIT 100", w, resource, through, after)
		if err != nil {
			return err
		}
		type record struct {
			ref  contexts.Ref
			unit contexts.Unit
		}
		page := []record{}
		for rows.Next() {
			var k, v string
			var raw []byte
			if err = rows.Scan(&k, &v, &raw); err != nil {
				break
			}
			var u contexts.Unit
			if err = json.Unmarshal(raw, &u); err != nil {
				break
			}
			page = append(page, record{contexts.Ref{Kind: k, ID: id, Revision: v}, u})
		}
		rows.Close()
		if err != nil {
			return err
		}
		if err = rows.Err(); err != nil {
			return err
		}
		if len(page) == 0 {
			return nil
		}
		for _, r := range page {
			key := docID(w, r.ref)
			if deleted {
				batch.Delete(key)
				continue
			}
			text, deps, err := b.text(ctx, w, r.ref)
			if err != nil {
				return err
			}
			doc := document{Workspace: w, ID: id, Kind: r.ref.Kind, Revision: r.ref.Revision, Current: r.ref.Revision == head, Text: text, Scopes: memberships, Tags: r.unit.Tags, Source: string(encode(source{r.ref, deps}))}
			if err = batch.Index(key, doc); err != nil {
				return err
			}
		}
		if batch.Size() >= 1000 {
			if err = i.Batch(batch); err != nil {
				return err
			}
			batch.Reset()
		}
		after = page[len(page)-1].ref.Revision
		if len(page) < 100 {
			return nil
		}
	}
}
