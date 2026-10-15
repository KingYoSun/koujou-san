package graphindex

import (
	"encoding/json"

	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
)

// Relationship provenance is read from its immutable SoT version, rather than
// inferred again during traversal or rebuild. Denied relationships are removed
// before this check so missing denied sources cannot affect public completeness.
func (x *exploration) sources(edges []arcEdge) (map[string]*contexts.Origin, error) {
	type wanted struct {
		K string `json:"k"`
		contexts.Ref
	}
	refs := []wanted{}
	for _, e := range edges {
		var ref contexts.Ref
		if err := json.Unmarshal([]byte(e.Source), &ref); err != nil {
			return nil, err
		}
		refs = append(refs, wanted{e.K, ref})
	}
	result := map[string]*contexts.Origin{}
	if len(refs) == 0 {
		return result, nil
	}
	rows, err := x.s.Store.Database().Query(x.ctx, edgeSourcesSQL, x.w, encode(refs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var raw []byte
		if err = rows.Scan(&k, &raw); err != nil {
			return nil, err
		}
		var origin *contexts.Origin
		if len(raw) > 0 {
			if err = json.Unmarshal(raw, &origin); err != nil {
				return nil, err
			}
		}
		result[k] = origin
	}
	return result, rows.Err()
}

const edgeSourcesSQL = `WITH wanted AS (SELECT * FROM jsonb_to_recordset($2::jsonb) AS t(k text,kind text,id text,revision text))
 SELECT t.k,g.payload->'origin' FROM wanted t
 LEFT JOIN context_records c ON c.workspace_id=$1 AND c.resource_id=CASE WHEN t.kind IN ('sentence','paragraph','section','document') THEN 'context:' ELSE t.kind||':' END||t.id AND c.revision=t.revision AND c.kind=t.kind
 LEFT JOIN graph_relations g ON t.kind='relation' AND g.workspace_id=$1 AND g.id=t.id AND g.revision=t.revision
 WHERE c.revision IS NOT NULL OR g.revision IS NOT NULL OR (t.kind='run' AND (EXISTS(SELECT 1 FROM shared_versions v WHERE v.workspace_id=$1 AND v.resource_id='run:'||t.id AND v.version=t.revision) OR EXISTS(SELECT 1 FROM graph_relations r WHERE r.workspace_id=$1 AND ((r.payload->'from'->>'kind'='run' AND r.payload->'from'->>'id'=t.id AND r.payload->'from'->>'revision'=t.revision) OR (r.payload->'to'->>'kind'='run' AND r.payload->'to'->>'id'=t.id AND r.payload->'to'->>'revision'=t.revision)))))`
