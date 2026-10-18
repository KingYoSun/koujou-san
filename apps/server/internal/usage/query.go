package usage

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
)

type Request struct {
	Basis         []Ref     `json:"basis,omitempty"`
	Mode          string    `json:"mode"` // aggregate, export, records, changes
	Kind          string    `json:"kind"` // exactly one valuation or usage; never sum alternatives
	From          time.Time `json:"from"`
	Until         time.Time `json:"until"`
	Timezone      string    `json:"timezone"`
	Attribution   string    `json:"attribution,omitempty"`
	AttributionID string    `json:"attribution_id,omitempty"`
	Workflow      string    `json:"workflow,omitempty"`
	Scopes        []string  `json:"scopes,omitempty"`
	Model         string    `json:"model,omitempty"`
	Currency      string    `json:"currency,omitempty"`
	ID            string    `json:"id,omitempty"`
	Since         int64     `json:"since,omitempty"`
	Cursor        string    `json:"cursor,omitempty"`
	Limit         int       `json:"limit"`
}
type Page struct {
	Summary  json.RawMessage   `json:"summary,omitempty"`
	Items    []json.RawMessage `json:"items"`
	AsOf     time.Time         `json:"as_of"`
	Timezone string            `json:"timezone"`
	Kind     string            `json:"kind"`
	Through  int64             `json:"through"`
}
type cursor struct {
	Subject   string
	Workspace string
	Query     Request
	Through   int64
	Position  string
	AsOf      time.Time
	After     string
}

func (s *Service) cursor(v cursor) string {
	raw, _ := json.Marshal(v)
	h := hmac.New(sha256.New, s.key)
	h.Write(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
func (s *Service) openCursor(token string) (cursor, error) {
	var v cursor
	a, b, ok := strings.Cut(token, ".")
	if !ok {
		return v, ErrInvalid
	}
	raw, e := base64.RawURLEncoding.DecodeString(a)
	if e != nil {
		return v, ErrInvalid
	}
	proof, e := base64.RawURLEncoding.DecodeString(b)
	if e != nil {
		return v, ErrInvalid
	}
	h := hmac.New(sha256.New, s.key)
	h.Write(raw)
	if !hmac.Equal(proof, h.Sum(nil)) || json.Unmarshal(raw, &v) != nil {
		return v, ErrInvalid
	}
	return v, nil
}
func (s *Service) Query(ctx context.Context, p access.Principal, w string, req Request) (apicontract.Response[Page], error) {
	empty := apicontract.Response[Page]{}
	if err := s.Access.Check(ctx, p, w, "", "read"); err != nil {
		return empty, err
	}
	if req.Mode == "" {
		req.Mode = "aggregate"
	}
	if req.Kind == "" {
		req.Kind = "usage"
	}
	if req.Timezone == "" {
		req.Timezone = "UTC"
	}
	if req.Limit == 0 {
		req.Limit = 50
	}
	if req.Limit < 1 || req.Limit > 100 || req.Since < 0 {
		return empty, ErrInvalid
	}
	if _, err := time.LoadLocation(req.Timezone); err != nil {
		return empty, ErrInvalid
	}
	subject, err := s.Access.CursorSubject(ctx, p, w)
	if err != nil {
		return empty, err
	}
	token := req.Cursor
	req.Cursor = ""
	c := cursor{Subject: subject, Workspace: w, Query: req, AsOf: time.Now().UTC()}
	if token != "" {
		v, e := s.openCursor(token)
		a, _ := json.Marshal(req)
		b, _ := json.Marshal(v.Query)
		if e != nil || v.Subject != subject || v.Workspace != w || !hmac.Equal(a, b) {
			return empty, ErrInvalid
		}
		c = v
	} else {
		if err = s.Store.Database().QueryRow(ctx, `SELECT COALESCE((SELECT sequence FROM shared_streams WHERE workspace_id=$1),0),COALESCE((SELECT position FROM shared_streams WHERE workspace_id=$1),'')`, w).Scan(&c.Through, &c.Position); err != nil {
			return empty, err
		}
	}
	var sql, summarySQL string
	var args []any
	base := []any{w, p.UserID, p.OperationID, c.Through, req.Kind}
	switch req.Mode {
	case "run":
		if req.AttributionID == "" || (req.Kind != "usage" && req.Kind != "reported" && req.Kind != "estimated" && req.Kind != "api_equivalent" && req.Kind != "allocated") {
			return empty, ErrInvalid
		}
		// Select this Run's indexed keys first; never compute every Workspace
		// head before filtering. The second attribution check excludes corrections
		// that moved an old key to a different Run.
		sql = `WITH candidates AS (SELECT DISTINCT projection_key FROM usage_entries WHERE workspace_id=$1 AND kind=$5 AND attribution_kind='run' AND attribution_id=$6 AND position<=$4 AND projection_key>$7), latest AS (
 SELECT e.* FROM candidates c CROSS JOIN LATERAL (SELECT * FROM usage_entries u WHERE u.workspace_id=$1 AND u.projection_key=c.projection_key AND u.position<=$4 ORDER BY u.position DESC LIMIT 1) e)
 SELECT jsonb_build_object('key',e.projection_key,'group',e.coverage_group,'position',e.position,'at',e.at,'kind',e.kind,'currency',e.currency,'model',NULLIF(e.model,''),'amount',CASE WHEN e.state='rated' AND NOT EXISTS(SELECT 1 FROM usage_group_updates g WHERE g.workspace_id=$1 AND g.coverage_group=e.coverage_group AND g.position>e.position AND g.position<=$4) THEN e.amount::text END,'state',CASE WHEN e.kind<>'usage' AND EXISTS(SELECT 1 FROM usage_group_updates g WHERE g.workspace_id=$1 AND g.coverage_group=e.coverage_group AND g.position>e.position AND g.position<=$4) THEN 'pending' ELSE e.state END,'refs',(SELECT COALESCE(jsonb_agg(r),'[]'::jsonb) FROM jsonb_array_elements(e.refs) r WHERE r->>'id' LIKE 'usage:%' OR r->>'id' LIKE 'price:%' OR r->>'id' LIKE 'evaluation:%'),'quantities',e.quantities,'value',e.payload),e.projection_key
 FROM latest e WHERE e.attribution_kind='run' AND e.attribution_id=$6 AND ` + access.ReadFilterSQL("e.refs") + ` ORDER BY e.projection_key LIMIT $8`
		args = append(base, req.AttributionID, c.After, req.Limit+1)
	case "records":
		if req.Kind != "usage" && req.Kind != "price" && req.Kind != "evaluation" && req.Kind != "comparison" {
			return empty, ErrInvalid
		}
		sql = `SELECT jsonb_build_object('id',e.id,'version',e.version,'position',e.position,'kind',e.kind,'value',e.payload),lpad(e.position::text,20,'0') FROM usage_records e
 WHERE e.workspace_id=$1 AND e.position<=$4 AND e.kind=$5 AND ($6='' OR e.id=$6) AND lpad(e.position::text,20,'0')>$7
 AND ` + access.ReadFilterSQL(`e.dependencies||jsonb_build_array(jsonb_build_object('id',e.id,'version',e.version))`) + ` ORDER BY e.position LIMIT $8`
		args = append(base, req.ID, c.After, req.Limit+1)
	case "changes":
		sql = `SELECT jsonb_build_object('group',e.coverage_group,'position',e.position,'affected_from',e.affected_from,'affected_until',e.affected_until,'source',jsonb_build_object('id',e.resource_id,'version',e.version)),lpad(e.position::text,20,'0')
 FROM usage_group_updates e JOIN usage_records r ON r.workspace_id=e.workspace_id AND r.id=e.resource_id AND r.version=e.version
 WHERE e.workspace_id=$1 AND e.position<=$4 AND $5<>'' AND e.position>$6 AND lpad(e.position::text,20,'0')>$7 AND ` + access.ReadFilterSQL(`r.dependencies||jsonb_build_array(jsonb_build_object('id',r.id,'version',r.version))`) + ` ORDER BY e.position LIMIT $8`
		args = append(base, req.Since, c.After, req.Limit+1)
	case "aggregate", "export", "compare", "compare_details", "compare_quality":
		if req.Kind != "usage" && req.Kind != "reported" && req.Kind != "estimated" && req.Kind != "api_equivalent" && req.Kind != "allocated" {
			return empty, ErrInvalid
		}
		if req.From.IsZero() {
			return empty, ErrInvalid
		}
		if !req.Until.After(req.From) {
			return empty, ErrInvalid
		}
		common := `WITH latest AS (SELECT DISTINCT ON(projection_key) * FROM usage_entries WHERE workspace_id=$1 AND kind=$5 AND position<=$4 ORDER BY projection_key,position DESC), eligible AS (
 SELECT e.*,CASE WHEN e.kind<>'usage' AND EXISTS(SELECT 1 FROM usage_group_updates u WHERE u.workspace_id=$1 AND u.coverage_group=e.coverage_group AND u.position>e.position AND u.position<=$4) THEN 'pending' ELSE e.state END AS current_state
 FROM latest e WHERE e.at>=$6 AND e.at<$7 AND ($8='' OR e.attribution_kind=$8) AND ($9='' OR e.attribution_id=$9) AND ($10='' OR e.workflow=$10)
 AND (cardinality($11::text[])=0 OR e.scopes && $11) AND ($12='' OR e.model=$12) AND ($13='' OR e.currency=$13) AND ` + access.ReadFilterSQL("e.refs") + ` AND e.refs @> $17::jsonb) `
		args = append(base, req.From, req.Until, req.Attribution, req.AttributionID, req.Workflow, req.Scopes, req.Model, req.Currency, req.Timezone, c.After, req.Limit+1)
		if req.Scopes == nil {
			args[10] = []string{}
		}

		basis := req.Basis
		if basis == nil {
			basis = []Ref{}
		}
		for _, ref := range basis {
			if !fixedBasis(ref) || s.Access.CheckVersion(ctx, nil, p, w, ref.ID, ref.Version) != nil {
				return empty, access.ErrForbidden
			}
		}
		basisJSON, _ := json.Marshal(basis)
		args = append(args, basisJSON)
		if req.Mode == "compare_quality" {
			args[4] = "usage"
			args[12] = ""
			sql = comparisonQualitySQL(common)
		} else if req.Mode == "compare" || req.Mode == "compare_details" {
			args[4] = "usage"
			args[12] = ""
			args = append(args, req.Kind, req.Currency)
			sql = comparisonSQL(common, req.Mode == "compare_details")
			if req.Mode == "compare" {
				summarySQL = common + comparisonSummarySQL()
			}
		} else if req.Mode == "export" {
			sql = common + `SELECT jsonb_build_object('key',projection_key,'group',coverage_group,'position',position,'at',at,'timezone',$14::text,'kind',kind,'currency',currency,'amount',CASE WHEN current_state='rated' THEN amount::text END,'state',current_state,'refs',refs,'quantities',quantities,'value',payload),projection_key FROM eligible WHERE projection_key>$15 ORDER BY projection_key LIMIT $16`
		} else {
			sql = common + `, buckets AS (
 SELECT to_char(at AT TIME ZONE $14,'YYYY-MM-DD') AS day,attribution_kind,attribution_id,workflow,model,currency,kind,count(*) AS records,
 sum(amount) FILTER(WHERE current_state='rated')::text AS amount,count(*) FILTER(WHERE current_state NOT IN ('rated','confirmed')) AS incomplete,
 max(position) AS position
 FROM eligible GROUP BY 1,2,3,4,5,6,7), keyed AS (SELECT *,jsonb_build_array(day,attribution_kind,attribution_id,workflow,model,currency,kind)::text AS key FROM buckets), quantity_totals AS (
 SELECT jsonb_build_array(to_char(e.at AT TIME ZONE $14,'YYYY-MM-DD'),e.attribution_kind,e.attribution_id,e.workflow,e.model,e.currency,e.kind)::text AS key,q.key AS category,q.value->>'unit' AS unit,
 sum((q.value->>'value')::numeric)::text AS value,count(*) FILTER(WHERE q.value->>'state'='missing') AS missing,count(*) FILTER(WHERE q.value->>'state'='not_applicable') AS not_applicable
 FROM eligible e CROSS JOIN LATERAL jsonb_each(e.quantities) q WHERE e.current_state IN ('rated','confirmed') GROUP BY 1,2,3), quantity_buckets AS (
 SELECT key,jsonb_agg(jsonb_build_object('category',category,'unit',unit,'value',value,'missing',missing,'not_applicable',not_applicable) ORDER BY category,unit) AS quantities FROM quantity_totals GROUP BY key)
 SELECT jsonb_build_object('key',k.key,'day',day,'attribution_kind',attribution_kind,'attribution_id',attribution_id,'workflow',workflow,'model',NULLIF(model,''),'currency',NULLIF(currency,''),'kind',kind,'records',records,'amount',amount,'incomplete',incomplete,'position',position,'quantities',COALESCE(q.quantities,'[]'::jsonb)),k.key FROM keyed k LEFT JOIN quantity_buckets q ON q.key=k.key WHERE k.key>$15 ORDER BY k.key LIMIT $16`
		}
	default:
		return empty, ErrInvalid
	}
	rows, err := s.Store.Database().Query(ctx, sql, args...)
	if err != nil {
		return empty, err
	}
	items := []json.RawMessage{}
	keys := []string{}
	for rows.Next() {
		var raw json.RawMessage
		var key string
		if err = rows.Scan(&raw, &key); err != nil {
			rows.Close()
			return empty, err
		}
		items = append(items, raw)
		keys = append(keys, key)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return empty, err
	}
	// Current membership and operation/policy must still hold after SQL finishes.
	now, err := s.Access.CursorSubject(ctx, p, w)
	if err != nil || now != subject || s.Access.Check(ctx, p, w, "", "read") != nil {
		return empty, access.ErrForbidden
	}
	meta := apicontract.Meta{WorkspaceID: w, Position: &c.Position, Completeness: apicontract.Completeness{State: "complete"}}
	if len(items) > req.Limit {
		items = items[:req.Limit]
		c.After = keys[req.Limit-1]
		next := s.cursor(c)
		meta.NextCursor = &next
	}
	page := Page{Items: items, AsOf: c.AsOf, Timezone: req.Timezone, Kind: req.Kind, Through: c.Through}
	if req.Mode == "compare" {
		err = s.Store.Database().QueryRow(ctx, summarySQL, args[:17]...).Scan(&page.Summary)
		if err != nil {
			return empty, err
		}
		now, err = s.Access.CursorSubject(ctx, p, w)
		if err != nil || now != subject {
			return empty, access.ErrForbidden
		}
	}
	return apicontract.Response[Page]{Data: &page, Meta: meta}, nil
}
