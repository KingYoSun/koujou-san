package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
)

type Query struct {
	Kind    string `json:"kind"`
	Subject *Ref   `json:"subject,omitempty"`
	Mine    bool   `json:"mine,omitempty"`
	After   string `json:"after,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}
type Item struct {
	Ref
	Kind  string          `json:"kind"`
	Value json.RawMessage `json:"value"`
}
type Page struct {
	Items []Item `json:"items"`
	Next  string `json:"next"`
}

func (s *Service) Query(ctx context.Context, p access.Principal, w string, req Query) (apicontract.Response[Page], error) {
	result := apicontract.Response[Page]{}
	if err := s.Access.Check(ctx, p, w, "", "read"); err != nil {
		return result, err
	}
	if req.Limit == 0 {
		req.Limit = 50
	}
	if req.Limit < 1 || req.Limit > 100 {
		return result, ErrInvalid
	}
	page := Page{Items: []Item{}}
	var sql string
	var args []any
	if req.Kind == "work" {
		taskVisible := access.ReadFilterSQL(`tr.refs||jsonb_build_array(jsonb_build_object('id',tr.id,'version',tr.version))`)
		// The projection contains only summary fields and authorization refs.
		// In particular it never returns fixed input/Context bodies or raw logs.
		sql = `WITH work AS (
 SELECT r.id,r.version,'run' AS kind,
 jsonb_build_object('name',COALESCE(NULLIF(r.payload->>'name',''),'名称未設定のRun'),'state',r.payload->'state','observation',r.payload->'observation','assessment',(r.payload->'assessment') - 'report','stage',COALESCE(t.node_id,p.payload->'request'->>'waiting_for',NULLIF(i.payload->>'name',''),'工程名未指定'),'active_pause',r.payload->'active_pause','reason',p.payload->'reason','device',d.name,'requested_by',u.display_name,'updated_at',COALESCE(r.payload->'finished_at',r.payload->'started_at',r.payload->'created_at')) AS value,
 i.refs||r.refs||jsonb_build_array(jsonb_build_object('id',r.id,'version',r.version)) AS refs
 FROM run_records r JOIN shared_versions v ON v.workspace_id=r.workspace_id AND v.resource_id=r.id AND v.version=r.version
 JOIN run_records i ON i.workspace_id=r.workspace_id AND i.id=r.payload->'input'->>'id' AND i.version=r.payload->'input'->>'version'
 LEFT JOIN run_records p ON p.workspace_id=r.workspace_id AND p.id=r.payload->'active_pause'->>'id' AND p.version=r.payload->'active_pause'->>'version'
 LEFT JOIN access_devices d ON d.workspace_id=r.workspace_id AND d.id=r.payload->'assignment'->>'device_id'
 LEFT JOIN auth_users u ON u.id=r.payload->>'requested_by'
 LEFT JOIN LATERAL (SELECT t.node_id FROM workflow_tasks t JOIN LATERAL (SELECT tr.id,tr.version,tr.refs FROM workflow_records tr WHERE tr.workspace_id=t.workspace_id AND tr.kind='task' AND tr.id='workflow-task:'||substr(t.execution_id,20)||':'||t.node_id||':'||t.attempt::text ORDER BY tr.position DESC LIMIT 1) tr ON true WHERE t.workspace_id=r.workspace_id AND t.run_id=r.id AND t.run_id<>'' AND t.current AND ` + taskVisible + ` LIMIT 1) t ON true
 WHERE r.workspace_id=$1 AND r.kind='run' AND r.id>$5
 UNION ALL
 SELECT e.id,e.version,'execution',
 jsonb_build_object('name',d.payload->'name','state',e.payload->'state','observation','known','stage',COALESCE(stages.names,'詳細で確認'),'reason',e.payload->'reason','requested_by',u.display_name,'updated_at',e.payload->'started_at'),
 e.refs||d.refs||jsonb_build_array(jsonb_build_object('id',e.id,'version',e.version),jsonb_build_object('id',e.owner_id,'version',e.version))
 FROM workflow_records e JOIN shared_versions v ON v.workspace_id=e.workspace_id AND v.resource_id=e.id AND v.version=e.version
 JOIN workflow_records d ON d.workspace_id=e.workspace_id AND d.id=e.payload->'definition'->>'id' AND d.version=e.payload->'definition'->>'version'
 LEFT JOIN auth_users u ON u.id=e.payload->>'controller'
 LEFT JOIN LATERAL (SELECT string_agg(n.node_id,' / ')||CASE WHEN count(*)=2 THEN ' …' ELSE '' END AS names FROM (SELECT t.node_id FROM workflow_tasks t JOIN LATERAL (SELECT tr.id,tr.version,tr.refs FROM workflow_records tr WHERE tr.workspace_id=t.workspace_id AND tr.kind='task' AND tr.id='workflow-task:'||substr(t.execution_id,20)||':'||t.node_id||':'||t.attempt::text ORDER BY tr.position DESC LIMIT 1) tr ON true WHERE t.workspace_id=e.workspace_id AND t.execution_id=e.id AND t.current AND t.state NOT IN ('done','skipped') AND ` + taskVisible + ` ORDER BY t.node_id LIMIT 2) n) stages ON true
 WHERE e.workspace_id=$1 AND e.kind='execution' AND e.id>$5
 ) SELECT r.id,r.version,r.kind,r.value||jsonb_build_object('recipient',n.recipient,'recipient_name',u.display_name),r.id FROM work r
 LEFT JOIN LATERAL (SELECT latest.recipient FROM (SELECT n.id,n.version,n.refs,n.recipient FROM workbench_records n JOIN shared_versions v ON v.workspace_id=n.workspace_id AND v.resource_id=n.id AND v.version=n.version WHERE n.workspace_id=$1 AND n.owner_id=r.id AND n.kind='route' AND (n.payload->'pause' IS NULL OR n.payload->'pause'=r.value->'active_pause') ORDER BY n.position DESC LIMIT 1) latest WHERE ` + access.ReadFilterSQL(`latest.refs||jsonb_build_array(jsonb_build_object('id',latest.id,'version',latest.version))`) + `) n ON true
 LEFT JOIN auth_users u ON u.id=n.recipient
 WHERE (NOT $4::boolean OR n.recipient=$2) AND ` + access.ReadFilterSQL("r.refs") + ` ORDER BY r.id LIMIT $6`
		args = []any{w, p.UserID, p.OperationID, req.Mine, req.After, req.Limit + 1}
	} else if req.Kind == "notes" || req.Kind == "history" {
		if req.Subject == nil || !fixed(*req.Subject) {
			return result, ErrInvalid
		}
		if _, _, err := s.subject(ctx, p, w, *req.Subject); err != nil {
			return result, err
		}
		// A numeric position uniquely orders this domain's one record/mutation.
		after := int64(0)
		var err error
		if req.After != "" {
			after, err = strconv.ParseInt(req.After, 10, 64)
			if err != nil || after < 0 {
				return result, ErrInvalid
			}
		}
		head := ""
		if req.Kind == "notes" {
			head = ` AND EXISTS(SELECT 1 FROM shared_versions v WHERE v.workspace_id=r.workspace_id AND v.resource_id=r.id AND v.version=r.version)`
		}
		sql = `SELECT r.id,r.version,r.kind,r.payload,r.position::text FROM workbench_records r WHERE r.workspace_id=$1 AND r.owner_id=$4 AND r.position>$5` + head + ` AND ` + access.ReadFilterSQL(`r.refs||jsonb_build_array(jsonb_build_object('id',r.id,'version',r.version))`) + ` ORDER BY r.position,r.id LIMIT $6`
		args = []any{w, p.UserID, p.OperationID, req.Subject.ID, after, req.Limit + 1}
	} else {
		return result, ErrInvalid
	}
	rows, err := s.Store.Database().Query(ctx, sql, args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	keys := []string{}
	for rows.Next() {
		var item Item
		var key string
		if err = rows.Scan(&item.ID, &item.Version, &item.Kind, &item.Value, &key); err != nil {
			return result, err
		}
		page.Items = append(page.Items, item)
		keys = append(keys, key)
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	if len(page.Items) > req.Limit {
		page.Items = page.Items[:req.Limit]
		page.Next = keys[req.Limit-1]
	}
	state := "complete"
	if page.Next != "" {
		state = "partial"
	}
	result.Data = &page
	result.Meta = apicontract.Meta{WorkspaceID: w, Completeness: apicontract.Completeness{State: state}}
	return result, nil
}
func (s *Service) Register(mux *http.ServeMux) {
	base := "/workbench/workspaces/{workspace}"
	route := func(method, path string, fn func(context.Context, access.Principal, string, *http.Request) (any, error)) {
		mux.HandleFunc(method+" "+base+path, func(w http.ResponseWriter, r *http.Request) {
			uid, ok := s.Access.User(w, r)
			if !ok {
				return
			}
			value, err := fn(r.Context(), access.Principal{UserID: uid}, r.PathValue("workspace"), r)
			status := 200
			if errors.Is(err, access.ErrForbidden) {
				status = 403
			} else if errors.Is(err, ErrInvalid) || errors.Is(err, runs.ErrInvalid) {
				status = 422
			} else if errors.Is(err, runs.ErrState) {
				status = 409
			} else if err != nil {
				status = 503
			}
			if err != nil {
				value = map[string]string{"error": http.StatusText(status)}
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(value)
		})
	}
	route("POST", "/query", func(ctx context.Context, p access.Principal, w string, r *http.Request) (any, error) {
		var v Query
		if json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(&v) != nil {
			return nil, ErrInvalid
		}
		return s.Query(ctx, p, w, v)
	})
	route("GET", "/notes/{id}", func(ctx context.Context, p access.Principal, w string, r *http.Request) (any, error) {
		version := r.URL.Query().Get("version")
		if version == "" {
			version = "head"
		}
		return s.Read(ctx, p, w, Ref{ID: r.PathValue("id"), Version: version})
	})
	route("GET", "/input/{id}", func(ctx context.Context, p access.Principal, w string, r *http.Request) (any, error) {
		version := r.URL.Query().Get("version")
		if version == "" {
			version = "head"
		}
		record, err := s.Runs.Read(ctx, p, w, Ref{ID: r.PathValue("id"), Version: version})
		if err != nil {
			return nil, err
		}
		var run runs.Run
		if record.Kind != "run" || json.Unmarshal(record.Value, &run) != nil {
			return nil, ErrInvalid
		}
		var value json.RawMessage
		err = s.Store.Database().QueryRow(ctx, `SELECT jsonb_build_object('artifacts',payload->'artifacts','derivation',payload->'derivation','previous',payload->'previous','sources',payload->'sources') FROM run_records WHERE workspace_id=$1 AND id=$2 AND version=$3`, w, run.Input.ID, run.Input.Version).Scan(&value)
		return value, err
	})
	route("POST", "/notes/{id}", func(ctx context.Context, p access.Principal, w string, r *http.Request) (any, error) {
		var v Write
		if json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(&v) != nil {
			return nil, ErrInvalid
		}
		return s.Put(ctx, p, w, r.PathValue("id"), v)
	})
}
