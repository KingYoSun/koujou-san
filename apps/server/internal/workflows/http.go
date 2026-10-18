package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
)

type Request struct {
	Kind    string `json:"kind"`
	Type    string `json:"type,omitempty"`
	ID      string `json:"id,omitempty"`
	Version string `json:"version,omitempty"`
	After   string `json:"after,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}
type Page struct {
	Items []Record `json:"items"`
	Next  string   `json:"next"`
	Root  *Ref     `json:"root,omitempty"`
}

func (s *Service) Query(ctx context.Context, p access.Principal, w string, req Request) (apicontract.Response[Page], error) {
	return s.page(ctx, p, w, req, false)
}
func (s *Service) page(ctx context.Context, p access.Principal, w string, req Request, strict bool) (apicontract.Response[Page], error) {
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
	page := Page{Items: []Record{}}
	db := s.Store.Database()
	query := `SELECT r.id,r.owner_id,r.version,r.kind,r.position::text,r.payload,r.refs,r.id FROM workflow_records r JOIN shared_versions v ON v.workspace_id=r.workspace_id AND v.resource_id=r.id AND v.version=r.version WHERE r.workspace_id=$1 AND ($4='' OR r.kind=$4) AND r.id>$5`
	args := []any{w, p.UserID, p.OperationID, req.Type, req.After}
	if req.Kind == "records" || req.Kind == "tasks" || req.Kind == "history" {
		version := req.Version
		if version == "" {
			version = "head"
		}
		root, err := s.Read(ctx, p, w, Ref{ID: req.ID, Version: version})
		if err != nil {
			return result, err
		}
		page.Root = &root.Ref
		condition := "r.id=$4"
		if req.Kind != "records" {
			condition = "r.owner_id=$4"
		}
		query = `SELECT r.id,r.owner_id,r.version,r.kind,r.position::text,r.payload,r.refs,lpad(r.position::text,20,'0')||':'||r.id FROM workflow_records r WHERE r.workspace_id=$1 AND ` + condition + ` AND r.position<=$6::bigint AND lpad(r.position::text,20,'0')||':'||r.id>$5`
		if req.Kind == "tasks" {
			query += ` AND r.kind='task' AND NOT EXISTS(SELECT 1 FROM workflow_records n WHERE n.workspace_id=r.workspace_id AND n.id=r.id AND n.position>r.position AND n.position<=$6::bigint)`
		}
		args = []any{w, p.UserID, p.OperationID, req.ID, req.After, root.Position}
	} else if req.Kind != "heads" {
		return result, ErrInvalid
	}
	if !strict {
		query += ` AND ` + access.ReadFilterSQL(`r.refs||jsonb_build_array(jsonb_build_object('id',r.id,'version',r.version))`)
	} else {
		query += ` AND $2::text<>'' AND $3::text IS NOT NULL`
	}
	if req.Kind == "heads" {
		query += ` ORDER BY r.id`
	} else {
		query += ` ORDER BY r.position,r.id`
	}
	query += ` LIMIT $` + map[bool]string{true: "7", false: "6"}[len(args) == 6]
	if req.Kind == "heads" {
		query = strings.Replace(query, "r.payload,r.refs", `jsonb_build_object('name',r.payload->'name','schema',r.payload->'schema','state',r.payload->'state'),r.refs`, 1)
	}
	if req.Kind == "tasks" {
		query = strings.Replace(query, "r.payload,r.refs", `r.payload-'prepared',r.refs`, 1)
	}
	args = append(args, req.Limit+1)
	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	keys := []string{}
	for rows.Next() {
		var r Record
		var refs []byte
		var key string
		if err = rows.Scan(&r.ID, &r.Owner, &r.Version, &r.Kind, &r.Position, &r.Value, &refs, &key); err != nil {
			return result, err
		}
		if err = json.Unmarshal(refs, &r.Refs); err != nil {
			return result, err
		}
		if strict {
			if err = s.checkRefs(ctx, db, p, w, append(r.Refs, r.Ref)); err != nil {
				return result, err
			}
		}
		page.Items = append(page.Items, r)
		keys = append(keys, key)
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	meta := apicontract.Meta{WorkspaceID: w, Completeness: apicontract.Completeness{State: "complete"}}
	if len(page.Items) > req.Limit {
		page.Items = page.Items[:req.Limit]
		page.Next = keys[req.Limit-1]
		meta.NextCursor = &page.Next
	}
	result.Data = &page
	result.Meta = meta
	return result, nil
}
func reply(w http.ResponseWriter, workspace string, value any, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	status := 200
	if err != nil {
		code := "unavailable"
		status = 503
		if errors.Is(err, access.ErrForbidden) {
			code = "forbidden"
			status = 403
		} else if errors.Is(err, ErrInvalid) {
			code = "invalid_request"
			status = 400
		} else if errors.Is(err, ErrState) {
			code = "state_conflict"
			status = 409
		}
		value = apicontract.Response[any]{Error: &apicontract.Problem{Code: code}, Meta: apicontract.Meta{WorkspaceID: workspace, Completeness: apicontract.Completeness{State: "pending", Reason: code}}}
	}
	if v, ok := value.(apicontract.Response[apicontract.Reference]); ok && v.Error != nil {
		status = 409
		if v.Error.Code == "forbidden" {
			status = 403
		}
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func body(r *http.Request, value any) bool {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	return d.Decode(value) == nil
}
func (s *Service) Register(mux *http.ServeMux) {
	s.registerExecution(mux)
	base := "/workflows/workspaces/{workspace}"
	principal := func(w http.ResponseWriter, r *http.Request) (access.Principal, bool) {
		id, ok := s.Access.User(w, r)
		return access.Principal{UserID: id}, ok
	}
	mux.HandleFunc("POST "+base+"/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		p, ok := principal(w, r)
		if !ok {
			return
		}
		var req CreateRun
		if !body(r, &req) {
			reply(w, r.PathValue("workspace"), nil, ErrInvalid)
			return
		}
		v, err := s.CreateRun(r.Context(), p, r.PathValue("workspace"), r.PathValue("id"), req)
		reply(w, r.PathValue("workspace"), v, err)
	})
	mux.HandleFunc("GET "+base+"/capabilities/{device}", func(w http.ResponseWriter, r *http.Request) {
		p, ok := principal(w, r)
		if !ok {
			return
		}
		version := r.URL.Query().Get("version")
		if version == "" {
			version = "head"
		}
		v, err := s.Capabilities(r.Context(), p, r.PathValue("workspace"), r.PathValue("device"), version)
		reply(w, r.PathValue("workspace"), v, err)
	})
	mux.HandleFunc("GET "+base+"/records/{id}", func(w http.ResponseWriter, r *http.Request) {
		p, ok := principal(w, r)
		if !ok {
			return
		}
		version := r.URL.Query().Get("version")
		if version == "" {
			version = "head"
		}
		v, err := s.Read(r.Context(), p, r.PathValue("workspace"), Ref{ID: r.PathValue("id"), Version: version})
		if err == nil && r.URL.Query().Get("format") == "source" {
			reply(w, r.PathValue("workspace"), struct {
				Ref
				Kind      string `json:"kind"`
				ValueUTF8 string `json:"value_utf8"`
			}{v.Ref, v.Kind, string(v.Value)}, nil)
			return
		}
		reply(w, r.PathValue("workspace"), v, err)
	})
	mux.HandleFunc("POST "+base+"/records/{id}", func(w http.ResponseWriter, r *http.Request) {
		p, ok := principal(w, r)
		if !ok {
			return
		}
		var req Write
		if !body(r, &req) {
			reply(w, r.PathValue("workspace"), nil, ErrInvalid)
			return
		}
		v, err := s.Put(r.Context(), p, r.PathValue("workspace"), r.PathValue("id"), req)
		reply(w, r.PathValue("workspace"), v, err)
	})
	mux.HandleFunc("POST "+base+"/query", func(w http.ResponseWriter, r *http.Request) {
		p, ok := principal(w, r)
		if !ok {
			return
		}
		var req Request
		if !body(r, &req) {
			reply(w, r.PathValue("workspace"), nil, ErrInvalid)
			return
		}
		v, err := s.Query(r.Context(), p, r.PathValue("workspace"), req)
		reply(w, r.PathValue("workspace"), v, err)
	})
	mux.HandleFunc("POST "+base+"/{action}", func(w http.ResponseWriter, r *http.Request) {
		p, ok := principal(w, r)
		if !ok {
			return
		}
		ws := r.PathValue("workspace")
		var v any
		var err error
		switch r.PathValue("action") {
		case "validate":
			var req Ref
			if !body(r, &req) {
				err = ErrInvalid
			} else {
				v, err = s.Validate(r.Context(), p, ws, req)
			}
		case "verify":
			var req Verify
			if !body(r, &req) {
				err = ErrInvalid
			} else {
				v, err = s.Verify(r.Context(), p, ws, req)
			}
		case "adopt":
			var req Adopt
			if !body(r, &req) {
				err = ErrInvalid
			} else {
				v, err = s.Adopt(r.Context(), p, ws, req)
			}
		default:
			err = ErrInvalid
		}
		reply(w, ws, v, err)
	})
	mux.HandleFunc("GET "+base+"/definitions/{id}/export", func(w http.ResponseWriter, r *http.Request) {
		p, ok := principal(w, r)
		if !ok {
			return
		}
		ref := Ref{ID: r.PathValue("id"), Version: r.URL.Query().Get("version")}
		if !fixed(ref) {
			reply(w, r.PathValue("workspace"), nil, ErrInvalid)
			return
		}
		rec, err := s.Read(r.Context(), p, r.PathValue("workspace"), ref)
		if err != nil {
			reply(w, r.PathValue("workspace"), nil, err)
			return
		}
		if !strings.HasSuffix(rec.Kind, "definition") {
			reply(w, r.PathValue("workspace"), nil, ErrInvalid)
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="koujou-definition.json"`)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(rec.Value)
	})
}
