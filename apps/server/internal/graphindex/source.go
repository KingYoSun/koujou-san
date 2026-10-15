package graphindex

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
)

type headValue struct {
	Version string
	Deleted bool
}
type sourceBuilder struct {
	DB                   database
	W, G                 string
	Seen                 map[string]bool
	Heads                map[string]headValue
	NodeCount, EdgeCount int
}

func newBuilder(db database, w, g string) *sourceBuilder {
	return &sourceBuilder{DB: db, W: w, G: g, Seen: map[string]bool{}, Heads: map[string]headValue{}}
}
func (b *sourceBuilder) head(ctx context.Context, r contexts.Ref) (headValue, error) {
	id := resource(r)
	if h, ok := b.Heads[id]; ok {
		return h, nil
	}
	var h headValue
	err := b.DB.QueryRow(ctx, "SELECT version,deleted FROM shared_versions WHERE workspace_id=$1 AND resource_id=$2", b.W, id).Scan(&h.Version, &h.Deleted)
	if err == nil {
		b.Heads[id] = h
	}
	return h, err
}
func (b *sourceBuilder) addEdge(out *batch, kind string, from, to contexts.Ref, order int, owner string, source contexts.Ref) {
	e := arcEdge{G: b.G, W: b.W, F: key(b.W, from), T: key(b.W, to), Kind: kind, Ord: order, Owner: owner, Source: string(encode(source))}
	e.K = hash([]any{kind, e.F, e.T, order, owner, source.Revision})
	out.Edges = append(out.Edges, e)
	b.EdgeCount++
}
func (b *sourceBuilder) node(ctx context.Context, out *batch, ref contexts.Ref) error {
	k := key(b.W, ref)
	if b.Seen[k] {
		return nil
	}
	h, err := b.head(ctx, ref)
	if err != nil {
		return err
	}
	form := "revision"
	if ref.Revision == "" {
		form = "identity"
	}
	v := vertex{G: b.G, K: k, W: b.W, Kind: ref.Kind, ID: ref.ID, Rev: ref.Revision, Form: form, Current: ref.Revision != "" && ref.Revision == h.Version && !h.Deleted, Deleted: h.Deleted, Logical: identity(b.W, ref)}
	out.Nodes = append(out.Nodes, v)
	b.Seen[k] = true
	b.NodeCount++
	if ref.Revision != "" {
		id := ref
		id.Revision = ""
		if err = b.node(ctx, out, id); err != nil {
			return err
		}
		b.addEdge(out, "version", ref, id, 0, resource(ref), ref)
	}
	return nil
}
func (b *sourceBuilder) content(ctx context.Context, out *batch, ref contexts.Ref, raw []byte) error {
	if err := b.node(ctx, out, ref); err != nil {
		return err
	}
	if unitKind(ref.Kind) {
		var unit contexts.Unit
		if err := json.Unmarshal(raw, &unit); err != nil {
			return err
		}
		for index, child := range unit.Children {
			if err := b.node(ctx, out, child); err != nil {
				return err
			}
			b.addEdge(out, "contains", ref, child, index, resource(ref), ref)
		}
	} else if ref.Kind == "scope" {
		var scope contexts.Scope
		if err := json.Unmarshal(raw, &scope); err != nil {
			return err
		}
		if scope.Parent != nil {
			parent := contexts.Ref{Kind: "scope", ID: scope.Parent.ID}
			if err := b.node(ctx, out, parent); err != nil {
				return err
			}
			b.addEdge(out, "scope_parent", ref, parent, 0, resource(ref), ref)
		}
	}
	return nil
}
func (b *sourceBuilder) membership(ctx context.Context, out *batch, id, revision string, raw []byte) error {
	var m contexts.Membership
	if err := json.Unmarshal(raw, &m); err != nil {
		return err
	}
	owner := "membership:" + id
	out.ReplaceOwners = append(out.ReplaceOwners, owner)
	if !m.Present {
		return nil
	}
	from := contexts.Ref{Kind: m.Context.Kind, ID: m.Context.ID}
	to := contexts.Ref{Kind: "scope", ID: m.Scope.ID}
	for _, r := range []contexts.Ref{from, to} {
		if err := b.node(ctx, out, r); err != nil {
			return err
		}
	}
	b.addEdge(out, "membership", from, to, 0, owner, contexts.Ref{Kind: "membership", ID: id, Revision: revision})
	return nil
}
func (b *sourceBuilder) relation(ctx context.Context, out *batch, id, revision string, raw []byte) error {
	var r Relation
	if err := json.Unmarshal(raw, &r); err != nil {
		return err
	}
	owner := "relation:" + id
	out.ReplaceOwners = append(out.ReplaceOwners, owner)
	if !r.Present {
		return nil
	}
	source := contexts.Ref{Kind: "relation", ID: id, Revision: revision}
	for _, ref := range []contexts.Ref{r.From, r.To} {
		if err := b.node(ctx, out, ref); err != nil {
			return err
		}
	}
	b.addEdge(out, r.Kind, r.From, r.To, r.Order, owner, source)
	return nil
}
func (b *sourceBuilder) event(ctx context.Context, out *batch, id, revision string, deleted bool) error {
	switch {
	case strings.HasPrefix(id, "context:") || strings.HasPrefix(id, "scope:"):
		logical := contexts.Ref{Kind: strings.SplitN(id, ":", 2)[0], ID: strings.SplitN(id, ":", 2)[1]}
		if logical.Kind == "context" {
			var kind string
			if err := b.DB.QueryRow(ctx, "SELECT kind FROM context_records WHERE workspace_id=$1 AND resource_id=$2 LIMIT 1", b.W, id).Scan(&kind); err != nil {
				return err
			}
			logical.Kind = kind
		}
		out.CurrentLogicals = append(out.CurrentLogicals, identity(b.W, logical))
		if deleted {
			out.DeletedLogicals = append(out.DeletedLogicals, identity(b.W, logical))
			return nil
		}
		var raw []byte
		var kind string
		if err := b.DB.QueryRow(ctx, "SELECT kind,payload FROM context_records WHERE workspace_id=$1 AND resource_id=$2 AND revision=$3", b.W, id, revision).Scan(&kind, &raw); err != nil {
			return err
		}
		return b.content(ctx, out, contexts.Ref{Kind: kind, ID: logical.ID, Revision: revision}, raw)
	case strings.HasPrefix(id, "membership:"):
		var raw []byte
		if err := b.DB.QueryRow(ctx, "SELECT payload FROM context_records WHERE workspace_id=$1 AND resource_id=$2 AND revision=$3", b.W, id, revision).Scan(&raw); err != nil {
			return err
		}
		return b.membership(ctx, out, strings.TrimPrefix(id, "membership:"), revision, raw)
	case strings.HasPrefix(id, "relation:"):
		out.ReplaceOwners = append(out.ReplaceOwners, id)
		if deleted {
			return nil
		}
		var raw []byte
		if err := b.DB.QueryRow(ctx, "SELECT payload FROM graph_relations WHERE workspace_id=$1 AND id=$2 AND revision=$3", b.W, strings.TrimPrefix(id, "relation:"), revision).Scan(&raw); err != nil {
			return err
		}
		return b.relation(ctx, out, strings.TrimPrefix(id, "relation:"), revision, raw)
	case strings.HasPrefix(id, "run:"):
		ref := contexts.Ref{Kind: "run", ID: strings.TrimPrefix(id, "run:"), Revision: revision}
		out.CurrentLogicals = append(out.CurrentLogicals, identity(b.W, ref))
		if deleted {
			out.DeletedLogicals = append(out.DeletedLogicals, identity(b.W, ref))
			return nil
		}
		return b.node(ctx, out, ref)
	}
	return nil
}
