package automation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/jackc/pgx/v5"
)

type Request struct {
	Mode  string `json:"mode"`
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	After string `json:"after"`
	Limit int    `json:"limit"`
}
type Page struct {
	Items []json.RawMessage `json:"items"`
	Next  string            `json:"next"`
}

func (s *Service) Query(ctx context.Context, p access.Principal, w string, r Request) (apicontract.Response[Page], error) {
	empty := apicontract.Response[Page]{}
	if err := s.Access.Check(ctx, p, w, "", "read"); err != nil {
		return empty, err
	}
	if r.Limit == 0 {
		r.Limit = 50
	}
	if r.Limit < 1 || r.Limit > 100 {
		return empty, ErrInvalid
	}
	var sql string
	refs := `e.refs||jsonb_build_array(jsonb_build_object('id',e.id,'version',e.version))`
	switch r.Mode {
	case "priorities":
		refs = `COALESCE(e.refs,'[]'::jsonb)||COALESCE(i.refs,'[]'::jsonb)||jsonb_build_array(jsonb_build_object('id',t.scope_id,'version',t.scope_version))`
		key := `CASE WHEN rank.policy_id IS NULL THEN '1/' ELSE '0/' END||COALESCE(rank.policy_id,'~')||'/'||COALESCE(rank.policy_version,'~')||'/'||lpad(COALESCE(rank.rank,2147483647)::text,10,'0')||'/'||t.received_at::text||'/'||t.scope_id`
		sql = `SELECT jsonb_build_object('id',COALESCE(e.id,'priority:'||t.scope_id),'version',COALESCE(e.version,'0'),'kind','priority','value',COALESCE(e.payload-'refs',jsonb_build_object('target',jsonb_build_object('id',t.scope_id,'version',t.scope_version),'adopted',null,'recommendation',null,'fixed',false,'state','unevaluated'))||CASE WHEN j.state IS NOT NULL AND j.state<>'done' THEN jsonb_build_object('state',j.state,'reason',j.reason) ELSE '{}'::jsonb END,'title',COALESCE(i.payload#>>'{observed,title}',i.payload->>'title',c.payload->>'name'),'deadline',t.deadline,'received_at',t.received_at,'next_due',t.next_due,'policy_group',rank.policy_id||'@'||rank.policy_version),` + key + ` FROM triage_targets t
 LEFT JOIN automation_records i ON i.workspace_id=t.workspace_id AND i.id=t.info_id AND i.version=t.info_version
 JOIN context_records c ON c.workspace_id=t.workspace_id AND c.resource_id=t.scope_id AND c.revision=t.scope_version
 LEFT JOIN shared_versions h ON h.workspace_id=t.workspace_id AND h.resource_id='priority:'||t.scope_id
 LEFT JOIN automation_records e ON e.workspace_id=h.workspace_id AND e.id=h.resource_id AND e.version=h.version
 LEFT JOIN triage_ranks rank ON rank.workspace_id=t.workspace_id AND rank.scope_id=t.scope_id
 LEFT JOIN automation_jobs j ON j.workspace_id=t.workspace_id AND j.id=t.latest_job
 WHERE t.workspace_id=$1 AND $4::text=$4 AND ($5='' OR t.scope_id=$5) AND (` + key + `)>$6 AND ` + access.ReadFilterSQL(refs) + ` ORDER BY ` + key + ` LIMIT $7`
	case "heads":
		sql = `SELECT jsonb_build_object('id',e.id,'version',e.version,'kind',e.kind,'position',e.position,'value',CASE WHEN e.kind='model-input' THEN e.payload-'request'-'evidence'-'issue' WHEN e.kind='model-attempt' THEN e.payload #- '{response,body}' WHEN e.kind='issue' THEN (e.payload-'body'-'sources') #- '{observed,body}' ELSE e.payload END,'refs',CASE WHEN e.kind IN ('model-input','model-attempt','issue') THEN '[]'::jsonb ELSE e.refs END)||CASE WHEN e.kind='sync' THEN jsonb_build_object('observed_at',(SELECT t.observed_at FROM triage_sync t WHERE t.workspace_id=e.workspace_id AND t.scope_id=substring(e.id from 15))) ELSE '{}'::jsonb END,e.id FROM automation_records e JOIN shared_versions v ON v.workspace_id=e.workspace_id AND v.resource_id=e.id AND v.version=e.version AND NOT v.deleted WHERE e.workspace_id=$1 AND ($4='' OR e.kind=$4) AND ($5='' OR e.id=$5) AND e.id>$6 AND ` + access.ReadFilterSQL(refs) + ` AND (e.kind<>'batch' OR NOT EXISTS(SELECT 1 FROM triage_batch_jobs b JOIN automation_jobs bj ON bj.workspace_id=b.workspace_id AND bj.id=b.job_id WHERE b.workspace_id=e.workspace_id AND b.batch_id=e.id AND NOT (` + access.ReadFilterSQL("bj.refs||COALESCE(bj.result->'refs','[]'::jsonb)||COALESCE(bj.result->'usage','[]'::jsonb)") + `))) ORDER BY e.id LIMIT $7`
	case "priority":
		value, err := s.Priority(ctx, p, w, r.ID)
		if err != nil {
			return empty, err
		}
		page := Page{Items: []json.RawMessage{encode(value)}}
		return apicontract.Response[Page]{Data: &page, Meta: apicontract.Meta{WorkspaceID: w, Completeness: apicontract.Completeness{State: "complete"}}}, nil
	case "selection":
		var version string
		if err := s.Store.Database().QueryRow(ctx, "SELECT version FROM shared_versions WHERE workspace_id=$1 AND resource_id=$2", w, r.ID).Scan(&version); err != nil {
			return empty, access.ErrForbidden
		}
		policy, reason, err := s.Select(ctx, p, w, Ref{ID: r.ID, Version: version})
		if err != nil {
			return empty, err
		}
		page := Page{Items: []json.RawMessage{encode(map[string]any{"target": Ref{ID: r.ID, Version: version}, "policy": policy, "reason": reason})}}
		return apicontract.Response[Page]{Data: &page, Meta: apicontract.Meta{WorkspaceID: w, Completeness: apicontract.Completeness{State: "complete"}}}, nil
	case "records":
		sql = `SELECT jsonb_build_object('id',id,'version',version,'kind',kind,'position',position,'value',CASE WHEN e.kind='model-input' THEN e.payload-'request'-'evidence'-'issue' WHEN e.kind='model-attempt' THEN e.payload #- '{response,body}' WHEN e.kind='issue' THEN (e.payload-'body'-'sources') #- '{observed,body}' ELSE e.payload END,'refs',CASE WHEN e.kind IN ('model-input','model-attempt','issue') THEN '[]'::jsonb ELSE e.refs END),lpad(position::text,20,'0') FROM automation_records e WHERE workspace_id=$1 AND ($4='' OR kind=$4) AND ($5='' OR id=$5) AND lpad(position::text,20,'0')>$6 AND ` + access.ReadFilterSQL(refs) + ` AND (e.kind<>'batch' OR NOT EXISTS(SELECT 1 FROM triage_batch_jobs b JOIN automation_jobs bj ON bj.workspace_id=b.workspace_id AND bj.id=b.job_id WHERE b.workspace_id=e.workspace_id AND b.batch_id=e.id AND NOT (` + access.ReadFilterSQL("bj.refs||COALESCE(bj.result->'refs','[]'::jsonb)||COALESCE(bj.result->'usage','[]'::jsonb)") + `))) ORDER BY position LIMIT $7`

	case "evaluations":
		refs = `e.refs||COALESCE(e.result->'refs','[]'::jsonb)||COALESCE(e.result->'usage','[]'::jsonb)`
		sql = `SELECT (to_jsonb(e)-'checkpoint'-'lease'-'refs'-'result')||jsonb_build_object('result',CASE WHEN jsonb_typeof(e.result)='object' THEN e.result-'refs' ELSE e.result END,'has_checkpoint',checkpoint<>'null'::jsonb,'control_version',COALESCE((SELECT version FROM shared_versions WHERE workspace_id=$1 AND resource_id='job-control:'||e.id),'0')),e.id FROM automation_jobs e WHERE e.workspace_id=$1 AND e.kind='triage' AND $4::text=$4 AND ($5='' OR e.id=$5 OR e.target_id=$5) AND e.id>$6 AND ` + access.ReadFilterSQL(refs) + ` ORDER BY e.id LIMIT $7`
	case "attempts":
		sql = `SELECT jsonb_build_object('id',e.id,'version',e.version,'kind',e.kind,'value',e.payload #- '{response,body}'),e.id FROM triage_model_attempts a JOIN automation_jobs j ON j.workspace_id=a.workspace_id AND j.id=a.job_id JOIN shared_versions h ON h.workspace_id=a.workspace_id AND h.resource_id=a.resource_id JOIN automation_records e ON e.workspace_id=h.workspace_id AND e.id=h.resource_id AND e.version=h.version WHERE e.workspace_id=$1 AND $4::text=$4 AND ($5='' OR a.job_id=$5 OR j.target_id=$5) AND e.id>$6 AND ` + access.ReadFilterSQL(refs) + ` ORDER BY e.id LIMIT $7`
	case "jobs":
		refs = `e.refs||COALESCE(e.result->'refs','[]'::jsonb)||COALESCE(e.result->'usage','[]'::jsonb)`
		sql = `SELECT (to_jsonb(e)-'checkpoint'-'lease')||jsonb_build_object('has_checkpoint',checkpoint<>'null'::jsonb,'control_version',COALESCE((SELECT version FROM shared_versions WHERE workspace_id=$1 AND resource_id='job-control:'||e.id),'0')),id FROM automation_jobs e WHERE workspace_id=$1 AND ($4='' OR kind=$4) AND ($5='' OR id=$5 OR target_id=$5) AND id>$6 AND ` + access.ReadFilterSQL(refs) + ` ORDER BY id LIMIT $7`
	case "history":
		job, err := scanJob(s.Store.Database().QueryRow(ctx, "SELECT "+jobColumns+" FROM automation_jobs WHERE workspace_id=$1 AND id=$2", w, r.ID))
		if err != nil {
			return empty, access.ErrForbidden
		}
		deps := append([]Ref{}, job.Refs...)
		if job.Result != nil {
			deps = append(deps, job.Result.Refs...)
			deps = append(deps, job.Result.Usage...)
		}
		if err = s.checkRefs(ctx, nil, p, w, deps); err != nil {
			return empty, err
		}
		sql = `SELECT jsonb_build_object('position',sequence,'state',state,'reason',reason,'worker',worker,'attempt',attempt,'has_checkpoint',checkpoint<>'null'::jsonb,'at',at),lpad(sequence::text,20,'0') FROM automation_job_events WHERE workspace_id=$1 AND $2<>'' AND $3::text=$3 AND $4::text=$4 AND job_id=$5 AND lpad(sequence::text,20,'0')>$6 ORDER BY sequence LIMIT $7`
	default:
		return empty, ErrInvalid
	}
	rows, err := s.Store.Database().Query(ctx, sql, w, p.UserID, p.OperationID, r.Kind, r.ID, r.After, r.Limit+1)
	if err != nil {
		return empty, err
	}
	page := Page{Items: []json.RawMessage{}}
	keys := []string{}
	for rows.Next() {
		var raw json.RawMessage
		var key string
		if err = rows.Scan(&raw, &key); err != nil {
			rows.Close()
			return empty, err
		}
		page.Items = append(page.Items, raw)
		keys = append(keys, key)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return empty, err
	}
	meta := apicontract.Meta{WorkspaceID: w, Completeness: apicontract.Completeness{State: "complete"}}
	if len(page.Items) > r.Limit {
		page.Items = page.Items[:r.Limit]
		page.Next = keys[r.Limit-1]
		meta.NextCursor = &page.Next
	}
	var pos string
	if err = s.Store.Database().QueryRow(ctx, "SELECT COALESCE((SELECT position FROM shared_streams WHERE workspace_id=$1),'')", w).Scan(&pos); err != nil {
		return empty, err
	}
	meta.Position = &pos
	if err = s.Access.Check(ctx, p, w, "", "read"); err != nil {
		return empty, err
	}
	return apicontract.Response[Page]{Data: &page, Meta: meta}, nil
}
func (s *Service) Priority(ctx context.Context, p access.Principal, w, target string) (Record, error) {
	if err := s.Access.Check(ctx, p, w, target, "get"); err != nil {
		return Record{}, err
	}
	var version string
	if err := s.Store.Database().QueryRow(ctx, "SELECT version FROM shared_versions WHERE workspace_id=$1 AND resource_id=$2", w, target).Scan(&version); err != nil {
		return Record{}, access.ErrForbidden
	}
	ref := Ref{ID: target, Version: version}
	policy, reason, refs, err := s.selectPolicy(ctx, s.Store.Database(), w, ref)
	if err != nil {
		return Record{}, err
	}
	out := Record{Ref: Ref{ID: "priority:" + target, Version: "0"}, Kind: "priority"}
	value := Priority{Target: ref, Policy: policy.Ref, State: "unevaluated", Reason: reason}
	saved, err := record(ctx, s.Store.Database(), w, Ref{ID: out.ID, Version: "head"})
	if err == nil {
		out = saved
		_ = json.Unmarshal(saved.Value, &value)
		refs = append(refs, saved.Ref)
		refs = append(refs, saved.Refs...)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Record{}, err
	}
	j, err := scanJob(s.Store.Database().QueryRow(ctx, "SELECT "+jobColumns+" FROM automation_jobs WHERE workspace_id=$1 AND id=(SELECT latest_job FROM triage_targets WHERE workspace_id=$1 AND scope_id=$2)", w, target))
	if err == nil {
		refs = append(refs, j.Refs...)
		if j.Result != nil {
			refs = append(refs, j.Result.Refs...)
			refs = append(refs, j.Result.Usage...)
		}
		if j.State != "done" {
			value.State = j.State
			value.Reason = j.Reason
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Record{}, err
	}
	value.Stale = value.Target != ref || (policy.ID != "" && value.Policy != policy.Ref)
	if value.Stale && (value.State == "evaluated" || value.State == "unevaluated") {
		value.State = "reevaluation_waiting"
	}
	if reason != "" {
		value.State = "waiting"
		value.Reason = reason
	}
	if err = s.checkRefs(ctx, nil, p, w, refs); err != nil {
		return Record{}, err
	}
	out.Value = encode(value)
	out.Refs = refs
	return out, nil
}

type ApplyPast struct {
	apicontract.Mutation
	Policy Ref    `json:"policy"`
	After  string `json:"after"`
	Limit  int    `json:"limit"`
}

// ApplyPast scans one explicit page of Issue identities, never Context bodies.
type Application struct {
	apicontract.Reference
	Requested int    `json:"requested"`
	Next      string `json:"next"`
}

func (s *Service) ApplyPast(ctx context.Context, p access.Principal, w string, r ApplyPast) (apicontract.Response[Application], error) {
	if r.Limit < 1 || r.Limit > 100 || !fixed(r.Policy) {
		return apicontract.Response[Application]{}, ErrInvalid
	}
	if err := s.manage(ctx, nil, p, w); err != nil {
		return apicontract.Response[Application]{}, err
	}
	scope := persistenceScope(p, w, func(tx pgx.Tx) error { return s.manage(ctx, tx, p, w) })
	base, applyErr := s.Store.Apply(ctx, scope, persistenceCommand(w, "past-apply:"+r.OperationID, "automation-past-apply", r.Mutation, encode(r)), func(tx pgx.Tx, ref apicontract.Reference) (json.RawMessage, error) {
		if err := s.checkRefs(ctx, tx, p, w, []Ref{r.Policy}); err != nil {
			return nil, err
		}
		rows, err := tx.Query(ctx, `SELECT c.resource_id,c.revision FROM context_records c JOIN shared_versions v ON v.workspace_id=c.workspace_id AND v.resource_id=c.resource_id AND v.version=c.revision AND NOT v.deleted WHERE c.workspace_id=$1 AND c.kind='scope' AND c.payload->>'kind'='issue' AND c.resource_id>$4 AND $3::text=$3 AND `+access.ReadFilterSQL(`jsonb_build_array(jsonb_build_object('id',c.resource_id,'version',c.revision))`)+` ORDER BY c.resource_id LIMIT $5`, w, p.UserID, p.OperationID, r.After, r.Limit+1)
		if err != nil {
			return nil, err
		}
		targets := []Ref{}
		for rows.Next() {
			var ref Ref
			if err = rows.Scan(&ref.ID, &ref.Version); err != nil {
				rows.Close()
				return nil, err
			}
			targets = append(targets, ref)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		next := ""
		if len(targets) > r.Limit {
			targets = targets[:r.Limit]
			next = targets[r.Limit-1].ID
		}
		count := 0
		dependencies := []Ref{r.Policy}
		for _, target := range targets {
			chosen, reason, refs, err := s.selectPolicy(ctx, tx, w, target)
			if err != nil {
				return nil, err
			}
			if reason != "" || chosen.Ref != r.Policy {
				continue
			}
			if s.checkRefs(ctx, tx, p, w, refs) != nil {
				continue
			}
			if err = s.queueTriage(ctx, tx, w, target, "explicit:"+r.OperationID+":"+target.ID+"@"+target.Version); err != nil {
				return nil, err
			}
			dependencies = append(dependencies, refs...)
			count++
		}
		if next != "" {
			dependencies = append(dependencies, Ref{ID: next})
		}
		if err := s.Access.PutResource(ctx, tx, access.Resource{WorkspaceID: w, ID: ref.ID}, nil); err != nil {
			return nil, err
		}
		return s.save(ctx, tx, w, Ref{ID: ref.ID, Version: ref.Version}, "application", map[string]any{"requested": count, "next": next}, dependencies)
	})
	if applyErr != nil {
		return apicontract.Response[Application]{}, applyErr
	}
	out := apicontract.Response[Application]{Error: base.Error, Meta: base.Meta}
	if base.Data == nil {
		return out, nil
	}
	saved, err := s.Read(ctx, p, w, Ref{ID: base.Data.ID, Version: base.Data.Version})
	if err != nil {
		return out, err
	}
	data := Application{Reference: *base.Data}
	if err = json.Unmarshal(saved.Value, &data); err != nil {
		return out, err
	}
	out.Data = &data
	return out, nil
}

type JobAction struct {
	apicontract.Mutation
	Action string `json:"action"`
	Reason string `json:"reason"`
}

func (s *Service) JobAction(ctx context.Context, p access.Principal, w, id string, r JobAction) (apicontract.Response[apicontract.Reference], error) {
	if (r.Action != "stop" && r.Action != "resume") || r.Reason == "" {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	if err := s.manage(ctx, nil, p, w); err != nil {
		return apicontract.Response[apicontract.Reference]{}, err
	}
	scope := persistenceScope(p, w, func(tx pgx.Tx) error { return s.manage(ctx, tx, p, w) })
	return s.Store.Apply(ctx, scope, persistenceCommand(w, "job-control:"+id, "automation-job-control", r.Mutation, encode(r)), func(tx pgx.Tx, _ apicontract.Reference) (json.RawMessage, error) {
		j, err := scanJob(tx.QueryRow(ctx, "SELECT "+jobColumns+" FROM automation_jobs WHERE workspace_id=$1 AND id=$2 FOR UPDATE", w, id))
		if err != nil {
			return nil, access.ErrForbidden
		}
		if j.State == "done" {
			return nil, ErrInvalid
		}
		if err = s.checkRefs(ctx, tx, p, w, j.Refs); err != nil {
			return nil, err
		}
		state := "stopped"
		if r.Action == "resume" {
			if _, err = s.authority(ctx, tx, j, false); err != nil {
				return nil, err
			}
			state = "waiting"
		}
		if _, err = tx.Exec(ctx, `UPDATE automation_jobs SET state=$3,reason=$4,lease_until=NULL,worker='',lease='',due_at=now(),deadline=CASE WHEN $3='waiting' THEN NULL ELSE deadline END,attempts=CASE WHEN $3='waiting' THEN 0 ELSE attempts END,updated_at=now() WHERE workspace_id=$1 AND id=$2`, w, id, state, r.Reason); err != nil {
			return nil, err
		}
		if err = logJob(ctx, tx, w, id); err != nil {
			return nil, err
		}
		return encode(map[string]string{"id": id, "state": state, "reason": r.Reason}), nil
	})
}
func (s *Service) manage(ctx context.Context, tx pgx.Tx, p access.Principal, w string) error {
	action := "manage"
	if p.OperationID != "" {
		action = "triage_manage"
	}
	if tx == nil {
		return s.Access.Check(ctx, p, w, "", action)
	}
	return s.Access.CheckTx(ctx, tx, p, w, "", action)
}

func send(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func reply(w http.ResponseWriter, value any, err error) {
	if err != nil {
		code, status := "unavailable", 503
		if errors.Is(err, access.ErrForbidden) {
			code, status = "forbidden", 403
		}
		if errors.Is(err, ErrInvalid) {
			code, status = "invalid_request", 400
		}
		send(w, status, map[string]string{"error": code})
		return
	}
	send(w, 200, value)
}
func wire(r *http.Request, v any) error {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		return ErrInvalid
	}
	return nil
}
func (s *Service) Register(mux *http.ServeMux) {
	for _, name := range []string{"model-template", "model-connections"} {
		mux.HandleFunc("GET /automation/workspaces/{workspace}/"+name, func(w http.ResponseWriter, r *http.Request) {
			user, ok := s.Access.User(w, r)
			if !ok {
				return
			}
			p := access.Principal{UserID: user}
			workspace := r.PathValue("workspace")
			if err := s.Access.Check(r.Context(), p, workspace, "", "read"); err != nil {
				reply(w, nil, err)
				return
			}
			if name == "model-template" {
				reply(w, DefaultModel(), nil)
				return
			}
			value, err := s.Gateway.Connections(workspace)
			reply(w, value, err)
		})
	}
	mux.HandleFunc("GET /automation/workspaces/{workspace}/template", func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.Access.User(w, r)
		if !ok {
			return
		}
		if s.Access.Check(r.Context(), access.Principal{UserID: user}, r.PathValue("workspace"), "", "read") != nil {
			reply(w, nil, access.ErrForbidden)
			return
		}
		send(w, 200, DefaultLevels())
	})
	mux.HandleFunc("GET /automation/workspaces/{workspace}/records/{id}", func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.Access.User(w, r)
		if !ok {
			return
		}
		v := r.URL.Query().Get("version")
		if v == "" {
			v = "head"
		}
		value, err := s.Read(r.Context(), access.Principal{UserID: user}, r.PathValue("workspace"), Ref{ID: r.PathValue("id"), Version: v})
		reply(w, value, err)
	})
	mux.HandleFunc("GET /automation/workspaces/{workspace}/priority/{id}", func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.Access.User(w, r)
		if !ok {
			return
		}
		value, err := s.Priority(r.Context(), access.Principal{UserID: user}, r.PathValue("workspace"), r.PathValue("id"))
		reply(w, value, err)
	})
	mux.HandleFunc("POST /automation/workspaces/{workspace}/{action}", func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.Access.User(w, r)
		if !ok {
			return
		}
		p := access.Principal{UserID: user}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		switch r.PathValue("action") {
		case "query":
			var req Request
			if wire(r, &req) != nil {
				reply(w, nil, ErrInvalid)
				return
			}
			v, e := s.Query(r.Context(), p, r.PathValue("workspace"), req)
			reply(w, v, e)
		case "apply-past":
			var req ApplyPast
			if wire(r, &req) != nil {
				reply(w, nil, ErrInvalid)
				return
			}
			v, e := s.ApplyPast(r.Context(), p, r.PathValue("workspace"), req)
			mutationReply(w, v, e)
		default:
			send(w, 404, map[string]string{"error": "not_found"})
		}
	})
	mux.HandleFunc("POST /automation/workspaces/{workspace}/records/{id}", func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.Access.User(w, r)
		if !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		var req Write
		if wire(r, &req) != nil {
			reply(w, nil, ErrInvalid)
			return
		}
		v, e := s.Put(r.Context(), access.Principal{UserID: user}, r.PathValue("workspace"), r.PathValue("id"), req)
		mutationReply(w, v, e)
	})
	mux.HandleFunc("POST /automation/workspaces/{workspace}/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.Access.User(w, r)
		if !ok {
			return
		}
		var req JobAction
		if wire(r, &req) != nil {
			reply(w, nil, ErrInvalid)
			return
		}
		v, e := s.JobAction(r.Context(), access.Principal{UserID: user}, r.PathValue("workspace"), r.PathValue("id"), req)
		mutationReply(w, v, e)
	})
}
func mutationReply[T any](w http.ResponseWriter, v apicontract.Response[T], err error) {
	if err == nil && v.Error != nil {
		send(w, 409, v)
		return
	}
	reply(w, v, err)
}
