package graphindex

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
)

func meta(w, pos, state, reason string) apicontract.Meta {
	var marker *string
	if pos != "" {
		marker = &pos
	}
	return apicontract.Meta{WorkspaceID: w, Position: marker, Completeness: apicontract.Completeness{State: state, Reason: reason}}
}
func failed(w, code, reason string) apicontract.Response[Result] {
	return apicontract.Response[Result]{Error: &apicontract.Problem{Code: code}, Meta: meta(w, "", "pending", reason)}
}
func (s *Service) request(r Request) (Request, error) {
	if r.Root.ID == "" || !(unitKind(r.Root.Kind) || r.Root.Kind == "scope" || r.Root.Kind == "run") || r.Root.Revision == "head" {
		return r, errInput
	}
	if r.Direction == "" {
		r.Direction = "both"
	}
	if r.Direction != "both" && r.Direction != "out" && r.Direction != "in" {
		return r, errInput
	}
	for _, k := range r.Kinds {
		if !edgeKind(k) {
			return r, errInput
		}
	}
	for _, pair := range []struct {
		value *int
		max   int
	}{{&r.Depth, s.Limits.Depth}, {&r.Nodes, s.Limits.Nodes}, {&r.Edges, s.Limits.Edges}, {&r.Limit, s.Limits.Page}} {
		if *pair.value == 0 {
			*pair.value = pair.max
		}
		if *pair.value < 1 || *pair.value > pair.max {
			return r, errInput
		}
	}
	return r, nil
}

type cursor struct {
	User, Workspace, Query, Generation, Position string
	After                                        int
}
type exploration struct {
	s       *Service
	ctx     context.Context
	p       access.Principal
	w, g    string
	r       Request
	nodes   map[string]vertex
	edges   map[string]arcEdge
	reasons map[string]bool
	grants  map[string]bool
	loaded  map[string]vertex
	checked map[string]bool
	order   []string
	until   int
}

func (x *exploration) allow(ids []string) error {
	unknown := []string{}
	for _, id := range ids {
		if _, ok := x.grants[id]; !ok {
			unknown = append(unknown, id)
			x.grants[id] = false
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	grants, err := x.s.Access.Readable(x.ctx, x.p, x.w, unknown)
	if err != nil {
		return err
	}
	for id := range grants {
		x.grants[id] = true
	}
	return nil
}
func (x *exploration) vertices(keys []string) (map[string]vertex, error) {
	found := map[string]vertex{}
	missing := []string{}
	unique := map[string]bool{}
	for _, k := range keys {
		if v, ok := x.loaded[k]; ok {
			found[k] = v
		}
		if !x.checked[k] && !unique[k] {
			missing = append(missing, k)
			unique[k] = true
		}
	}
	keys = missing
	if len(keys) == 0 {
		return found, nil
	}
	a, err := read(x.ctx, x.s.Remote.DB, "SELECT g,k,w,kind,id,rev,form,current,deleted,logical FROM KoujouNode WHERE g=:g AND k IN :keys", map[string]any{"g": x.g, "keys": keys}, -1)
	if a.Reason != "" {
		x.reasons[a.Reason] = true
	}
	if err != nil || a.Reason != "" {
		return found, errIncomplete
	}
	ids := []string{}
	values := []vertex{}
	for _, row := range a.Rows {
		var v vertex
		if err = decodeRow(row, &v); err != nil {
			return found, err
		}
		if v.W != x.w {
			return found, errIncomplete
		}
		values = append(values, v)
		ids = append(ids, resource(contexts.Ref{Kind: v.Kind, ID: v.ID}))
	}
	if err = x.allow(ids); err != nil {
		return found, err
	}
	// Batch fixed-version checks against SoT. A projected node never proves that
	// its immutable source still exists (notably after restoring a backup).
	rows, err := x.s.Store.Database().Query(x.ctx, nodeSourcesSQL, x.w, encode(values))
	if err != nil {
		return found, err
	}
	valid := map[string]bool{}
	for rows.Next() {
		var k string
		if err = rows.Scan(&k); err != nil {
			break
		}
		valid[k] = true
	}
	rows.Close()
	if err != nil {
		return found, err
	}
	if err = rows.Err(); err != nil {
		return found, err
	}
	refs := []access.VersionRef{}
	for _, v := range values {
		refs = append(refs, access.VersionRef{ID: resource(contexts.Ref{Kind: v.Kind, ID: v.ID}), Version: v.Rev})
	}
	versions, err := x.s.Access.Versions(x.ctx, x.p, x.w, refs)
	if err != nil {
		return found, err
	}
	for _, v := range values {
		x.checked[v.K] = true
		if !x.grants[resource(contexts.Ref{Kind: v.Kind, ID: v.ID})] {
			continue
		}
		if !versions[access.VersionRef{ID: resource(contexts.Ref{Kind: v.Kind, ID: v.ID}), Version: v.Rev}] {
			continue
		}
		if !valid[v.K] {
			x.reasons["required_version_pending"] = true
			continue
		}
		found[v.K] = v
		x.loaded[v.K] = v
	}
	return found, nil
}
func (x *exploration) walk() error {
	root := key(x.w, x.r.Root)
	nodes, err := x.vertices([]string{root})
	if err != nil {
		return err
	}
	if _, ok := nodes[root]; !ok {
		x.reasons["required_version_pending"] = true
		return nil
	}
	x.nodes = nodes
	x.order = append(x.order, "n"+root)
	frontier := []string{root}
	for depth := 0; len(frontier) > 0; depth++ {
		next := []string{}
		for start := 0; start < len(frontier); start += 500 {
			end := min(start+500, len(frontier))
			for _, direction := range []string{"out", "in"} {
				if x.r.Direction != "both" && x.r.Direction != direction {
					continue
				}
				field := "f"
				if direction == "in" {
					field = "t"
				}
				after := ""
				for {
					condition := field + " IN :keys"
					if end-start == 1 {
						condition = field + "=:point"
					}
					sql := "SELECT g,k,w,f,t,kind,ord,owner,source FROM KoujouEdge WHERE g=:g AND " + condition + " AND k>:seek"
					if len(x.r.Kinds) > 0 {
						sql += " AND kind IN :kinds"
					}
					fetch := min(500, max(1, x.until-len(x.order)))
					sql += " ORDER BY k LIMIT " + strconv.Itoa(fetch)
					a, err := read(x.ctx, x.s.Remote.DB, sql, map[string]any{"g": x.g, "keys": frontier[start:end], "point": frontier[start], "seek": after, "kinds": x.r.Kinds}, -1)
					if a.Reason != "" {
						x.reasons[a.Reason] = true
					}
					if err != nil || a.Reason != "" {
						return errIncomplete
					}
					candidates := []arcEdge{}
					keys := []string{}
					owners := []string{}
					for _, row := range a.Rows {
						var e arcEdge
						if err = decodeRow(row, &e); err != nil {
							return err
						}
						after = e.K
						if _, seen := x.edges[e.K]; seen {
							continue
						}
						if e.W != x.w {
							return errIncomplete
						}
						candidates = append(candidates, e)
						keys = append(keys, e.F, e.T)
						owners = append(owners, e.Owner)
					}
					vertices, err := x.vertices(keys)
					if err != nil {
						return err
					}
					if err = x.allow(owners); err != nil {
						return err
					}
					permitted := []arcEdge{}
					for _, e := range candidates {
						_, f := vertices[e.F]
						_, t := vertices[e.T]
						if f && t && x.grants[e.Owner] {
							permitted = append(permitted, e)
						}
					}
					sources, err := x.sources(permitted)
					if err != nil {
						return err
					}
					for _, e := range candidates {
						f, fok := vertices[e.F]
						t, tok := vertices[e.T]
						if !fok || !tok || !x.grants[e.Owner] {
							continue
						}
						origin, exists := sources[e.K]
						if !exists {
							x.reasons["required_version_pending"] = true
							continue
						}
						e.Origin = origin
						// Fixed containment may intentionally point to an older revision. Only
						// identity -> version expansion defaults to the current revision.
						if e.Kind == "version" && direction == "in" && !x.r.History && !f.Current {
							continue
						}
						if depth >= x.r.Depth {
							x.reasons["depth_limit"] = true
							continue
						}
						extra := 0
						for _, v := range []vertex{f, t} {
							if _, ok := x.nodes[v.K]; !ok {
								extra++
							}
						}
						if len(x.nodes)+extra > x.r.Nodes {
							x.reasons["node_limit"] = true
							continue
						}
						if len(x.edges) >= x.r.Edges {
							x.reasons["edge_limit"] = true
							continue
						}
						x.edges[e.K] = e
						x.order = append(x.order, "e"+e.K)
						for _, v := range []vertex{f, t} {
							if _, ok := x.nodes[v.K]; !ok {
								x.nodes[v.K] = v
								x.order = append(x.order, "n"+v.K)
								next = append(next, v.K)
							}
						}
						if len(x.order) >= x.until {
							return nil
						}
					}
					if x.reasons["node_limit"] || x.reasons["edge_limit"] {
						return nil
					}
					if len(a.Rows) < fetch {
						break
					}
				}
			}
		}
		if x.reasons["node_limit"] || x.reasons["edge_limit"] {
			break
		}
		frontier = next
	}
	return nil
}
func (s *Service) Query(ctx context.Context, p access.Principal, w string, r Request) (result apicontract.Response[Result], err error) {
	ctx, cancel := context.WithTimeout(ctx, s.Limits.Timeout)
	defer cancel()
	defer func() {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result = apicontract.Response[Result]{Data: &Result{Items: []Item{}, Unexplored: []string{"timeout"}, Evidence: contexts.CandidateEvidence{Engine: "arcadedb", State: "partial", Reason: "timeout"}}, Meta: meta(w, "", "partial", "timeout")}
		}
	}()
	if err := s.Access.Check(ctx, p, w, resource(r.Root), "get"); err != nil {
		return failed(w, "forbidden", "not_accessed"), err
	}
	subject, err := s.Access.CursorSubject(ctx, p, w)
	if err != nil {
		return failed(w, "forbidden", "not_accessed"), err
	}
	if p.OperationID != "" {
		op, err := s.Access.Operation(ctx, p, w, p.OperationID)
		if err != nil {
			return failed(w, "forbidden", "not_accessed"), err
		}
		if op.Bounded {
			r.History = true
		}
	}
	r, err = s.request(r)
	if err != nil {
		return failed(w, "invalid_request", "invalid_graph"), err
	}
	required, err := head(ctx, s.Store.Database(), w)
	if err != nil {
		return failed(w, "unavailable", "storage_unavailable"), err
	}
	if s.Remote == nil {
		return failed(w, "unavailable", "graph_unavailable"), nil
	}
	active, err := s.Remote.Active(ctx, w)
	if err != nil || active.Active == "" || active.Schema != graphSchema {
		return failed(w, "unavailable", "graph_unavailable"), err
	}
	h, err := headerRead(ctx, s.Remote.DB, checkpointKey(active.Active))
	if err != nil {
		return failed(w, "unavailable", "graph_unavailable"), err
	}
	cp, err := parseCheckpoint(h)
	if err != nil {
		return failed(w, "unavailable", "graph_unavailable"), err
	}
	requested := r
	requested.Cursor = ""
	signature := hash(requested)
	position := cursor{}
	codec := apicontract.NewCursorCodec(s.Key, "koujou-graph-cursor-v1")
	if r.Cursor != "" {
		if codec.Open(r.Cursor, graphSchema, &position) != nil || position.User != subject || position.Workspace != w || position.Query != signature || position.Generation != active.Active || position.Position != cp.Position {
			return failed(w, "invalid_request", "invalid_cursor"), nil
		}
	}
	x := exploration{s: s, ctx: ctx, p: p, w: w, g: active.Active, r: r, nodes: map[string]vertex{}, edges: map[string]arcEdge{}, reasons: map[string]bool{}, grants: map[string]bool{}, loaded: map[string]vertex{}, checked: map[string]bool{}, until: position.After + r.Limit + 1}
	err = x.walk()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			x.reasons["timeout"] = true
		} else if len(x.reasons) == 0 {
			return failed(w, "unavailable", "graph_unavailable"), err
		}
	}
	// No mixed generation/checkpoint may claim to describe a complete graph.
	now, readErr := s.Remote.Active(ctx, w)
	end, endErr := headerRead(ctx, s.Remote.DB, checkpointKey(active.Active))
	if readErr != nil || endErr != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			x.reasons["timeout"] = true
		} else {
			x.reasons["stream_incomplete"] = true
		}
	} else if now.Active != active.Active || end != h {
		x.reasons["projection_changed"] = true
	}
	if cp != required {
		x.reasons["projection_pending"] = true
	}
	if ctx.Err() != nil {
		x.nodes = map[string]vertex{}
		x.edges = map[string]arcEdge{}
	} else {
		latest, err := head(ctx, s.Store.Database(), w)
		if err != nil {
			return failed(w, "unavailable", "storage_unavailable"), err
		}
		if latest != required {
			x.reasons["projection_pending"] = true
		}
		ids := []string{}
		for _, v := range x.nodes {
			ids = append(ids, resource(contexts.Ref{Kind: v.Kind, ID: v.ID}))
		}
		for _, e := range x.edges {
			ids = append(ids, e.Owner)
		}
		grants, err := s.Access.Readable(ctx, p, w, ids)
		if err != nil {
			return failed(w, "forbidden", "not_accessed"), err
		}
		for k, v := range x.nodes {
			if !grants[resource(contexts.Ref{Kind: v.Kind, ID: v.ID})] {
				delete(x.nodes, k)
			}
		}
		for k, e := range x.edges {
			_, f := x.nodes[e.F]
			_, t := x.nodes[e.T]
			if !f || !t || !grants[e.Owner] {
				delete(x.edges, k)
			}
		}
	}
	// Only public, authorized DTOs enter pagination. Raw RIDs and scan offsets are
	// never exposed, and a continuation is bound to the actor and projection.
	all := map[string]Item{}
	for k, v := range x.nodes {
		n := Node{Kind: v.Kind, ID: v.ID, Revision: v.Rev, Form: v.Form, Current: v.Current, Deleted: v.Deleted}
		id := "n" + k
		all[id] = Item{Node: &n}
	}
	for k, e := range x.edges {
		f, t := x.nodes[e.F], x.nodes[e.T]
		var src contexts.Ref
		if json.Unmarshal([]byte(e.Source), &src) != nil {
			return failed(w, "unavailable", "graph_unavailable"), errIncomplete
		}
		edge := Edge{ID: k, Kind: e.Kind, From: contexts.Ref{Kind: f.Kind, ID: f.ID, Revision: f.Rev}, To: contexts.Ref{Kind: t.Kind, ID: t.ID, Revision: t.Rev}, Order: e.Ord, Source: src, Origin: e.Origin}
		id := "e" + k
		all[id] = Item{Edge: &edge}
	}
	// Stable breadth-first discovery order permits a page to stop after its
	// look-ahead item. Continuations replay only the necessary prefix, so every
	// bridge is reauthorized after policy changes without exposing frontier IDs.
	items := []Item{}
	authorized := []Item{}
	for _, k := range x.order {
		if item, ok := all[k]; ok {
			authorized = append(authorized, item)
		}
	}
	pageEnd := min(len(authorized), position.After+r.Limit)
	if position.After < pageEnd {
		items = append(items, authorized[position.After:pageEnd]...)
	}
	more := len(authorized) > pageEnd
	if more {
		x.reasons["page_limit"] = true
	}
	reasons := []string{}
	for reason := range x.reasons {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	state, reason := "complete", ""
	if len(reasons) > 0 {
		state, reason = "partial", reasons[0]
	}
	for _, pending := range []string{"required_version_pending", "projection_pending"} {
		if x.reasons[pending] {
			state, reason = "pending", pending
		}
	}
	result = apicontract.Response[Result]{Data: &Result{Items: items, Unexplored: reasons, Evidence: contexts.CandidateEvidence{Engine: "arcadedb", Position: cp.Position, RequiredPosition: required.Position, State: state, Reason: reason}}, Meta: meta(w, cp.Position, state, reason)}
	if more {
		next := codec.Seal(cursor{subject, w, signature, active.Active, cp.Position, pageEnd}, graphSchema)
		result.Meta.NextCursor = &next
	}
	return result, nil
}

const nodeSourcesSQL = `WITH wanted AS (SELECT * FROM jsonb_to_recordset($2::jsonb) AS t(k text,kind text,id text,rev text))
 SELECT t.k FROM wanted t JOIN shared_versions v ON v.workspace_id=$1 AND v.resource_id=CASE WHEN t.kind IN ('sentence','paragraph','section','document') THEN 'context:' ELSE t.kind||':' END||t.id
 WHERE (t.rev='' OR EXISTS(SELECT 1 FROM context_records r WHERE r.workspace_id=$1 AND r.resource_id=v.resource_id AND r.revision=t.rev AND r.kind=t.kind)
 OR (t.kind='run' AND (v.version=t.rev OR EXISTS(SELECT 1 FROM graph_relations r WHERE r.workspace_id=$1 AND ((r.payload->'from'->>'kind'='run' AND r.payload->'from'->>'id'=t.id AND r.payload->'from'->>'revision'=t.rev) OR (r.payload->'to'->>'kind'='run' AND r.payload->'to'->>'id'=t.id AND r.payload->'to'->>'revision'=t.rev))))))`
