package usage

import (
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"strings"
	"time"
)

type Comparison struct {
	Name  string  `json:"name"`
	Left  Request `json:"left"`
	Right Request `json:"right"`
}

func fixedBasis(r Ref) bool {
	return r.Version != "" && r.Version != "0" && r.Version != "head" && (strings.HasPrefix(r.ID, "context:") || strings.HasPrefix(r.ID, "manifest:") || strings.HasPrefix(r.ID, "run-definition:") || strings.HasPrefix(r.ID, "workflow-definition:"))
}
func (v Comparison) validate() error {
	if strings.TrimSpace(v.Name) == "" {
		return ErrInvalid
	}
	for _, r := range []Request{v.Left, v.Right} {
		if r.Mode != "compare" || r.From.IsZero() || !r.Until.After(r.From) || r.Cursor != "" || r.ID != "" || r.Since != 0 || r.Limit != 50 {
			return ErrInvalid
		}
		if r.Kind != "usage" && r.Kind != "reported" && r.Kind != "estimated" && r.Kind != "api_equivalent" && r.Kind != "allocated" {
			return ErrInvalid
		}
		if _, err := time.LoadLocation(r.Timezone); err != nil {
			return ErrInvalid
		}
		for _, ref := range r.Basis {
			if !fixedBasis(ref) {
				return ErrInvalid
			}
		}
	}
	return nil
}

// Both comparison tables start from the same authorized, deduplicated coverage
// rows as the existing period queries. A missing/hidden valuation stays unrated.
func comparisonSQL(common string, details bool) string {
	valued := common + `, valued AS (
 SELECT b.*,v.amount AS cost_amount,COALESCE(v.currency,'') AS cost_currency,
 CASE WHEN b.current_state<>'confirmed' THEN b.current_state WHEN $18='usage' THEN b.current_state ELSE COALESCE(v.current_state,'unrated') END AS cost_state,
 COALESCE(v.refs,'[]'::jsonb) AS cost_refs,
 COALESCE(b.payload#>>'{measurement,state}','unavailable') AS measurement,
 COALESCE(b.payload#>>'{measurement,scope}','unavailable') AS measured_scope,
 COALESCE(b.payload#>>'{measurement,billing}','unknown') AS billing
 FROM eligible b LEFT JOIN LATERAL(
 SELECT e.*,CASE WHEN EXISTS(SELECT 1 FROM usage_group_updates u WHERE u.workspace_id=$1 AND u.coverage_group=e.coverage_group AND u.position>e.position AND u.position<=$4) THEN 'pending' ELSE e.state END AS current_state
 FROM (SELECT DISTINCT ON(projection_key) * FROM usage_entries WHERE workspace_id=$1 AND coverage_group=b.coverage_group AND kind=$18 AND position<=$4 ORDER BY projection_key,position DESC)e
 WHERE ($19='' OR e.currency=$19) AND ` + access.ReadFilterSQL("e.refs") + `
 )v ON $18<>'usage'
 ) `
	if details {
		return valued + `SELECT jsonb_build_object('key',jsonb_build_array(coverage_group,cost_currency)::text,'group',coverage_group,'position',position,'at',at,'kind',$18::text,'attribution_kind',attribution_kind,'attribution_id',attribution_id,'model',NULLIF(model,''),'currency',NULLIF(cost_currency,''),'amount',CASE WHEN cost_state='rated' THEN cost_amount::text END,'state',cost_state,'refs',refs||cost_refs,'quantities',quantities,'value',payload,'timezone',$14::text),jsonb_build_array(coverage_group,cost_currency)::text FROM valued WHERE jsonb_build_array(coverage_group,cost_currency)::text>$15 ORDER BY 2 LIMIT $16`
	}
	return valued + `, buckets AS (
 SELECT attribution_kind,model,cost_currency,cost_state,measurement,measured_scope,billing,count(*) AS records,
 sum(cost_amount) FILTER(WHERE cost_state='rated')::text AS amount,
 count(*) FILTER(WHERE cost_state NOT IN ('rated','confirmed')) AS incomplete
 FROM valued GROUP BY 1,2,3,4,5,6,7
 ), keyed AS (SELECT *,jsonb_build_array(attribution_kind,model,cost_currency,cost_state,measurement,measured_scope,billing)::text AS key FROM buckets), quantity_totals AS (
 SELECT jsonb_build_array(e.attribution_kind,e.model,e.cost_currency,e.cost_state,e.measurement,e.measured_scope,e.billing)::text AS key,q.key AS category,q.value->>'unit' AS unit,
 sum((q.value->>'value')::numeric) FILTER(WHERE e.current_state='confirmed')::text AS value,
 count(*) FILTER(WHERE e.current_state='confirmed' AND q.value->>'state'='reported') AS reported,
 count(*) FILTER(WHERE e.current_state='confirmed' AND q.value->>'state'='estimated') AS estimated,
 count(*) FILTER(WHERE e.current_state<>'confirmed') AS uncertain,
 count(*) FILTER(WHERE q.value->>'state'='missing') AS missing,
 count(*) FILTER(WHERE q.value->>'state'='not_applicable') AS not_applicable
 FROM valued e CROSS JOIN LATERAL jsonb_each(e.quantities)q GROUP BY 1,2,3
 ), quantities AS(SELECT key,jsonb_agg(jsonb_build_object('category',category,'unit',unit,'value',value,'reported',reported,'estimated',estimated,'uncertain',uncertain,'missing',missing,'not_applicable',not_applicable) ORDER BY category,unit) AS values FROM quantity_totals GROUP BY key)
 SELECT jsonb_build_object('key',k.key,'attribution_kind',attribution_kind,'model',NULLIF(model,''),'currency',NULLIF(cost_currency,''),'kind',$18::text,'records',records,'amount',amount,'incomplete',incomplete,'state',cost_state,'measurement',measurement,'measured_scope',measured_scope,'billing',billing,'quantities',COALESCE(q.values,'[]'::jsonb),'timezone',$14::text),k.key FROM keyed k LEFT JOIN quantities q ON q.key=k.key WHERE k.key>$15 ORDER BY k.key LIMIT $16`
}

// Outcomes are historical fixed assessments, never a new judgment. Time counts
// each Run once, independently of its number of model requests or currencies.
func comparisonOutcomesSQL() string {
	return `, owners AS(SELECT DISTINCT attribution_kind,attribution_id FROM eligible), run_heads AS (
 SELECT r.* FROM owners o CROSS JOIN LATERAL(SELECT * FROM run_records WHERE workspace_id=$1 AND id=o.attribution_id AND kind='run' AND position<=$4 ORDER BY position DESC LIMIT 1)r
 WHERE o.attribution_kind='run' AND ` + access.ReadFilterSQL("r.refs||jsonb_build_array(jsonb_build_object('id',r.id,'version',r.version))") + `
 ), outcomes AS (
 SELECT r.*,i.payload#>'{definition}' AS inspection,
 CASE WHEN i.id IS NOT NULL THEN 'independent' WHEN r.payload->'assessment' IS NOT NULL THEN 'self-report' ELSE 'unverified' END AS quality_source
 FROM run_heads r LEFT JOIN inspection_launches l ON l.workspace_id=$1 AND l.run_id=r.id
 LEFT JOIN inspection_records i ON i.workspace_id=$1 AND i.id=l.launch_id AND i.version=l.version AND i.kind='launch' AND i.position<=$4 AND ` + access.ReadFilterSQL("i.refs||jsonb_build_array(jsonb_build_object('id',i.id,'version',i.version))") + `
 ), quality AS(
 SELECT inspection,quality_source,COALESCE(payload#>>'{assessment,state}','unobserved') AS state,COALESCE(payload#>>'{assessment,verdict}','unobserved') AS verdict,count(*) AS records
 FROM outcomes GROUP BY 1,2,3,4
 ) `
}

func comparisonQualitySQL(common string) string {
	return common + comparisonOutcomesSQL() + ` SELECT jsonb_build_object('key',jsonb_build_array(inspection,quality_source,state,verdict)::text,'inspection',inspection,'source',quality_source,'state',state,'verdict',verdict,'records',records,'timezone',$14::text),jsonb_build_array(inspection,quality_source,state,verdict)::text FROM quality WHERE jsonb_build_array(inspection,quality_source,state,verdict)::text>$15 ORDER BY 2 LIMIT $16`
}

func comparisonSummarySQL() string {
	return comparisonOutcomesSQL() + ` SELECT jsonb_build_object(
 'runs',(SELECT count(*) FROM outcomes),
 'run_metadata_unavailable',(SELECT count(*) FROM owners WHERE attribution_kind='run')-(SELECT count(*) FROM outcomes),
 'completed',(SELECT count(*) FROM outcomes WHERE payload->>'state' IN('succeeded','failed','cancelled')),
 'failed',(SELECT count(*) FROM outcomes WHERE payload->>'state'='failed'),
 'unfinished',(SELECT count(*) FROM outcomes WHERE payload->>'state' NOT IN('succeeded','failed','cancelled')),
 'rework',(SELECT count(*) FROM outcomes WHERE payload->'previous' IS NOT NULL),
 'execution_seconds',(SELECT sum(EXTRACT(EPOCH FROM ((payload->>'finished_at')::timestamptz-(payload->>'started_at')::timestamptz)))::text FROM outcomes WHERE payload->>'finished_at' IS NOT NULL AND payload->>'started_at' IS NOT NULL),
 'waiting_seconds',(SELECT sum(EXTRACT(EPOCH FROM ((payload->>'started_at')::timestamptz-(payload->>'created_at')::timestamptz)))::text FROM outcomes WHERE payload->>'started_at' IS NOT NULL),
 'time_unavailable',(SELECT count(*) FROM outcomes WHERE payload->>'finished_at' IS NULL OR payload->>'started_at' IS NULL),
 'quality',COALESCE((SELECT jsonb_agg(jsonb_build_object('source',quality_source,'state',state,'verdict',verdict,'records',records)) FROM (SELECT quality_source,state,verdict,sum(records) AS records FROM quality GROUP BY 1,2,3)q),'[]'::jsonb),
 'owners',COALESCE((SELECT jsonb_agg(v) FROM(SELECT attribution_kind AS kind,count(*) AS records FROM owners GROUP BY attribution_kind)v),'[]'::jsonb))
 WHERE $14::text<>'' AND $15::text IS NOT NULL AND $16::int>0`
}
