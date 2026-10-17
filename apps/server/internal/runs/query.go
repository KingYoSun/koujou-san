package runs

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
)

type Request struct {
	RunID    string `json:"run_id,omitempty"`
	Version  string `json:"version,omitempty"`
	Kind     string `json:"kind"`
	After    string `json:"after,omitempty"`
	Limit    int    `json:"limit,omitempty"`
	Snapshot string `json:"snapshot,omitempty"`
}
type Page struct {
	Run      *Ref     `json:"run,omitempty"`
	Items    []Record `json:"items"`
	Next     string   `json:"next"`
	Snapshot string   `json:"snapshot,omitempty"`
}
type linkSnapshot struct {
	Workspace string
	Run       Ref
	Through   string
}

// Query reads one bounded page. A fixed Run version also fixes the upper event
// position, so an export never grows as the execution continues.
func (s *Service) Query(ctx context.Context, p access.Principal, w string, r Request) (apicontract.Response[Page], error) {
	return s.page(ctx, p, w, r, false)
}
func (s *Service) page(ctx context.Context, p access.Principal, w string, r Request, strict bool) (apicontract.Response[Page], error) {
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
	if r.Kind == "" {
		r.Kind = "run"
	}
	valid := r.Kind == "run" || r.Kind == "input" || r.Kind == "pause" || r.Kind == "pause-history" || r.Kind == "output" || r.Kind == "event" || r.Kind == "result" || r.Kind == "all" || r.Kind == "related"
	if !valid || (r.Kind != "run" && r.RunID == "") {
		return empty, ErrInvalid
	}
	page := Page{Items: []Record{}}
	var through string
	if r.RunID != "" {
		if r.Version == "" {
			r.Version = "head"
		}
		head, err := s.Read(ctx, p, w, Ref{ID: runID(r.RunID), Version: r.Version})
		if err != nil {
			return empty, err
		}
		page.Run = &head.Ref
		r.RunID = head.ID
		through = head.Position
	}
	if r.Kind == "related" {
		if r.Snapshot != "" {
			b, err := base64.RawURLEncoding.DecodeString(r.Snapshot)
			var snapshot linkSnapshot
			if err != nil || json.Unmarshal(b, &snapshot) != nil || snapshot.Workspace != w || snapshot.Run != *page.Run {
				return empty, ErrInvalid
			}
			if n, err := strconv.ParseInt(snapshot.Through, 10, 64); err != nil || n < 0 {
				return empty, ErrInvalid
			}
			through = snapshot.Through
			page.Snapshot = r.Snapshot
		} else {
			if err := s.Store.Database().QueryRow(ctx, `SELECT sequence::text FROM shared_streams WHERE workspace_id=$1`, w).Scan(&through); err != nil {
				return empty, err
			}
			page.Snapshot = base64.RawURLEncoding.EncodeToString(raw(linkSnapshot{w, *page.Run, through}))
		}
	}
	// i.refs contains the fixed input's dependencies once, not one copy per log
	// chunk. Only selected Run identities and the requested page are inspected.
	base := ` FROM run_records e JOIN run_records h ON h.workspace_id=e.workspace_id AND h.id=e.run_id
 AND h.version=COALESCE(NULLIF($5,''),(SELECT version FROM shared_versions WHERE workspace_id=$1 AND resource_id=e.run_id))
 JOIN run_records i ON i.workspace_id=h.workspace_id AND i.id=h.payload->'input'->>'id' AND i.version=h.payload->'input'->>'version'
 LEFT JOIN run_records n ON e.kind='link' AND n.workspace_id=e.workspace_id AND n.id=e.payload->'next'->>'id' AND n.version=e.payload->'next'->>'version'
 LEFT JOIN run_records ni ON ni.workspace_id=n.workspace_id AND ni.id=n.payload->'input'->>'id' AND ni.version=n.payload->'input'->>'version'
 WHERE e.workspace_id=$1 AND ($4='' OR e.run_id=$4) AND ($6='' OR e.position<=NULLIF($6,'')::bigint)
 AND (($7='all' AND e.kind!='link') OR e.kind=CASE WHEN $7='pause-history' THEN 'pause' WHEN $7='related' THEN 'link' ELSE $7 END)
 AND ($7!='run' OR e.version=h.version)
 AND ($7!='pause' OR NOT EXISTS(SELECT 1 FROM run_records newer WHERE newer.workspace_id=e.workspace_id AND newer.id=e.id AND newer.position>e.position AND newer.position<=h.position))`
	version := ""
	if page.Run != nil {
		version = page.Run.Version
	}
	key := `lpad(e.position::text,20,'0')||':'||e.id||':'||e.version`
	dependencies := `i.refs||COALESCE(ni.refs,'[]'::jsonb)||e.refs||jsonb_build_array(jsonb_build_object('id',e.id,'version',e.version))`
	filter := access.ReadFilterSQL(dependencies)
	if strict {
		var denied bool
		err := s.Store.Database().QueryRow(ctx, `WITH candidates AS MATERIALIZED (SELECT `+dependencies+` AS refs`+base+` AND `+key+`>$8 ORDER BY e.position,e.id,e.version LIMIT $9) SELECT EXISTS(SELECT 1 FROM candidates c WHERE NOT (`+access.ReadFilterSQL("c.refs")+`))`, w, p.UserID, p.OperationID, r.RunID, version, through, r.Kind, r.After, r.Limit+1).Scan(&denied)
		if err != nil {
			return empty, err
		}
		if denied {
			return empty, access.ErrForbidden
		}
	}
	rows, err := s.Store.Database().Query(ctx, `SELECT e.id,e.version,e.run_id,e.kind,e.position::text,e.payload,e.refs,`+key+base+` AND `+key+`>$8 AND `+filter+` ORDER BY e.position,e.id,e.version LIMIT $9`, w, p.UserID, p.OperationID, r.RunID, version, through, r.Kind, r.After, r.Limit+1)
	if err != nil {
		return empty, err
	}
	keys := []string{}
	for rows.Next() {
		var item Record
		var refs []byte
		var cursor string
		if err = rows.Scan(&item.ID, &item.Version, &item.RunID, &item.Kind, &item.Position, &item.Value, &refs, &cursor); err != nil {
			break
		}
		if err = json.Unmarshal(refs, &item.Refs); err != nil {
			break
		}
		page.Items = append(page.Items, item)
		keys = append(keys, cursor)
	}
	if err == nil {
		err = rows.Err()
	}
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
	if page.Run != nil {
		if _, err = s.Read(ctx, p, w, *page.Run); err != nil {
			return empty, err
		}
	} else if err = s.Access.Check(ctx, p, w, "", "read"); err != nil {
		return empty, err
	}
	return apicontract.Response[Page]{Data: &page, Meta: meta}, nil
}

type ExportFrame struct {
	Type          string           `json:"type"`
	Schema        string           `json:"schema,omitempty"`
	Run           *Ref             `json:"run,omitempty"`
	Record        *Record          `json:"record,omitempty"`
	Records       string           `json:"records,omitempty"`
	Code          string           `json:"code,omitempty"`
	LinksSnapshot string           `json:"links_snapshot,omitempty"`
	Manifest      *contexts.Record `json:"manifest,omitempty"`
}

func (s *Service) exportHTTP(w http.ResponseWriter, r *http.Request) {
	p, ok := s.principal(w, r)
	if !ok {
		return
	}
	workspace := r.PathValue("workspace")
	version := r.URL.Query().Get("version")
	if version == "" || version == "head" {
		reply(w, workspace, nil, ErrInvalid)
		return
	}
	request := Request{RunID: r.PathValue("id"), Version: version, Kind: "all", Limit: 100}
	first, err := s.page(r.Context(), p, workspace, request, true)
	if err != nil {
		reply(w, workspace, nil, err)
		return
	}
	linksRequest := Request{RunID: request.RunID, Version: version, Kind: "related", Limit: 100}
	links, err := s.Query(r.Context(), p, workspace, linksRequest)
	if err != nil {
		reply(w, workspace, nil, err)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", `attachment; filename="koujou-run.ndjson"`)
	// A bounded query still has its request context, while a large export is not
	// constrained by the development server's short ordinary-response deadline.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	encoder := json.NewEncoder(w)
	if encoder.Encode(ExportFrame{Type: "begin", Schema: "run-export-v1", Run: first.Data.Run, LinksSnapshot: links.Data.Snapshot}) != nil {
		return
	}
	count := int64(0)
	for {
		for _, record := range first.Data.Items {
			if encoder.Encode(ExportFrame{Type: "record", Record: &record}) != nil {
				return
			}
			count++
			if record.Kind == "input" {
				var spec Spec
				if json.Unmarshal(record.Value, &spec) != nil {
					_ = encoder.Encode(ExportFrame{Type: "error", Code: "unavailable"})
					return
				}
				if spec.Manifest != nil {
					manifest, err := s.Contexts.Read(r.Context(), p, workspace, *spec.Manifest)
					if err != nil {
						code := "source_unavailable"
						if err.Error() == "forbidden" {
							code = "forbidden"
						}
						_ = encoder.Encode(ExportFrame{Type: "error", Code: code})
						return
					}
					if encoder.Encode(ExportFrame{Type: "manifest", Manifest: &manifest}) != nil {
						return
					}
				}
			}
		}
		if first.Data.Next == "" {
			break
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		request.After = first.Data.Next
		first, err = s.page(r.Context(), p, workspace, request, true)
		if err != nil {
			code := "unavailable"
			if err == access.ErrForbidden {
				code = "forbidden"
			}
			_ = encoder.Encode(ExportFrame{Type: "error", Code: code})
			return
		}
	}
	for {
		for _, record := range links.Data.Items {
			if encoder.Encode(ExportFrame{Type: "record", Record: &record}) != nil {
				return
			}
			count++
		}
		if links.Data.Next == "" {
			break
		}
		linksRequest.After = links.Data.Next
		linksRequest.Snapshot = links.Data.Snapshot
		links, err = s.Query(r.Context(), p, workspace, linksRequest)
		if err != nil {
			_ = encoder.Encode(ExportFrame{Type: "error", Code: "unavailable"})
			return
		}
	}
	_ = encoder.Encode(ExportFrame{Type: "complete", Run: first.Data.Run, Records: strconv.FormatInt(count, 10)})
}

func (s *Service) principal(w http.ResponseWriter, r *http.Request) (access.Principal, bool) {
	id, ok := s.Access.User(w, r)
	return access.Principal{UserID: id}, ok
}
func reply(w http.ResponseWriter, workspace string, value any, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	status := 200
	if err != nil {
		if err.Error() == "forbidden" {
			err = access.ErrForbidden
		}
		code := "unavailable"
		status = 503
		switch err {
		case ErrInvalid:
			code = "invalid_request"
			status = 400
		case ErrState:
			code = "state_conflict"
			status = 409
		case access.ErrForbidden:
			code = "forbidden"
			status = 403
		}
		value = apicontract.Response[any]{Error: &apicontract.Problem{Code: code}, Meta: apicontract.Meta{WorkspaceID: workspace, Completeness: apicontract.Completeness{State: "pending", Reason: code}}}
	}
	if v, ok := value.(apicontract.Response[apicontract.Reference]); ok && v.Error != nil {
		status = 409
		if v.Error.Code == "forbidden" {
			status = 403
		}
		if v.Error.Code == "unavailable" {
			status = 503
		}
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func body(r *http.Request, v any) bool {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	return d.Decode(v) == nil
}
func (s *Service) Register(mux *http.ServeMux) {
	s.registerAssignment(mux)
	s.registerRunner(mux)
	base := "/runs/workspaces/{workspace}"
	mux.HandleFunc("GET "+base+"/executions/{id}/export", s.exportHTTP)
	mux.HandleFunc("GET "+base+"/records/{id}", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.principal(w, r)
		if !ok {
			return
		}
		workspace := r.PathValue("workspace")
		version := r.URL.Query().Get("version")
		if version == "" {
			version = "head"
		}
		v, err := s.Read(r.Context(), p, workspace, Ref{ID: r.PathValue("id"), Version: version})
		reply(w, workspace, v, err)
	})
	mux.HandleFunc("POST "+base+"/query", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.principal(w, r)
		if !ok {
			return
		}
		workspace := r.PathValue("workspace")
		var q Request
		if !body(r, &q) {
			reply(w, workspace, nil, ErrInvalid)
			return
		}
		v, err := s.Query(r.Context(), p, workspace, q)
		reply(w, workspace, v, err)
	})
	mux.HandleFunc("POST "+base+"/executions/{id}/{action}", func(w http.ResponseWriter, r *http.Request) {
		workspace, id, action := r.PathValue("workspace"), r.PathValue("id"), r.PathValue("action")
		var p access.Principal
		if action == "report" {
			var err error
			p, err = s.Access.DevicePrincipal(r.Context(), workspace, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
			if err != nil {
				reply(w, workspace, nil, err)
				return
			}
		} else {
			var ok bool
			p, ok = s.principal(w, r)
			if !ok {
				return
			}
		}
		var value any
		var err error
		switch action {
		case "assign":
			var req Assign
			if !body(r, &req) {
				err = ErrInvalid
			} else {
				value, err = s.Assign(r.Context(), p, workspace, id, req)
			}
		case "create":
			var req Create
			if !body(r, &req) {
				err = ErrInvalid
			} else {
				value, err = s.Create(r.Context(), p, workspace, id, req)
			}
		case "answer":
			var req Answer
			if !body(r, &req) {
				err = ErrInvalid
			} else {
				value, err = s.Answer(r.Context(), p, workspace, id, req)
			}
		case "report":
			var req Event
			if !body(r, &req) {
				err = ErrInvalid
			} else {
				value, err = s.Report(r.Context(), p, workspace, id, req)
			}
		case "request-start", "cancel":
			var req apicontract.Mutation
			if !body(r, &req) {
				err = ErrInvalid
			} else {
				value, err = s.Action(r.Context(), p, workspace, id, action, req)
			}
		default:
			err = ErrInvalid
		}
		reply(w, workspace, value, err)
	})
}
