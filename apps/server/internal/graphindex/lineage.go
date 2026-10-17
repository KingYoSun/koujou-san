package graphindex

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/jackc/pgx/v5"
	"strings"
)

// Inherited links are rebuilt from fixed domain refs and the PostgreSQL lineage.
// There is no second write path for publication or a separate graph authority.
func (b *sourceBuilder) lineage(ctx context.Context, out *batch, id, version string) error {
	var refsRaw []byte
	var root, evaluation, evaluationVersion string
	err := b.DB.QueryRow(ctx, `SELECT r.refs,l.root,l.evaluation_id,l.evaluation_version FROM hopper_lineage l JOIN (
 SELECT r.workspace_id,r.id,r.version,r.refs||COALESCE(i.refs,'[]'::jsonb) AS refs FROM run_records r LEFT JOIN run_records i ON i.workspace_id=r.workspace_id AND i.id=r.payload->'input'->>'id' AND i.version=r.payload->'input'->>'version' WHERE r.workspace_id=$1 AND r.id=$2 AND r.version=$3
 UNION ALL SELECT workspace_id,id,version,refs FROM workflow_records WHERE workspace_id=$1 AND id=$2 AND version=$3) r ON r.workspace_id=l.workspace_id AND r.id=l.resource_id WHERE l.workspace_id=$1 AND l.resource_id=$2`, b.W, id, version).Scan(&refsRaw, &root, &evaluation, &evaluationVersion)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	var refs []access.VersionRef
	if err = json.Unmarshal(refsRaw, &refs); err != nil {
		return err
	}
	toKind, toID, _ := strings.Cut(id, ":")
	to := contexts.Ref{Kind: toKind, ID: toID, Revision: version}
	for _, ref := range refs {
		fromID := ref.ID
		if strings.HasPrefix(fromID, "workflow-task:") {
			if err = b.DB.QueryRow(ctx, `SELECT owner_id FROM workflow_records WHERE workspace_id=$1 AND id=$2 AND version=$3`, b.W, fromID, ref.Version).Scan(&fromID); err != nil {
				return err
			}
		}
		k, logical, _ := strings.Cut(fromID, ":")
		if fromID == id || ref.Version == "" || !(k == "scope" || k == "run" || orchestrationKind(k)) {
			continue
		}
		var same bool
		if err = b.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM hopper_lineage WHERE workspace_id=$1 AND resource_id=$2 AND root=$3)`, b.W, fromID, root).Scan(&same); err != nil {
			return err
		}
		if !same {
			continue
		}
		from := contexts.Ref{Kind: k, ID: logical, Revision: ref.Version}
		if err = b.node(ctx, out, from); err != nil {
			return err
		}
		b.addEdge(out, "reference", from, to, 0, id, contexts.Ref{Kind: "hopper-evaluation", ID: strings.TrimPrefix(evaluation, "hopper-evaluation:"), Revision: evaluationVersion})
	}
	return nil
}
