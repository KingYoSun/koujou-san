package inspections

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"net/http"
	"strconv"
	"strings"
)

type Request struct {
	Kind    string `json:"kind"`
	ID      string `json:"id,omitempty"`
	Type    string `json:"type,omitempty"`
	Version string `json:"version,omitempty"`
	After   string `json:"after,omitempty"`
	Through string `json:"through,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}
type Page struct {
	Items   []Record `json:"items"`
	Next    string   `json:"next"`
	Root    *Ref     `json:"root,omitempty"`
	Through string   `json:"through,omitempty"`
}

func (s *Service) Query(ctx context.Context, p access.Principal, w string, req Request) (apicontract.Response[Page], error) {
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
	sql := `SELECT r.id,r.version,r.owner_id,r.kind,r.position::text,jsonb_build_object('name',r.payload->'name','kind',r.payload->'kind','purpose',r.payload->'purpose','state',r.payload->'state'),r.refs,r.id FROM inspection_records r JOIN shared_versions v ON v.workspace_id=r.workspace_id AND v.resource_id=r.id AND v.version=r.version WHERE r.workspace_id=$1 AND ($4='' OR r.kind=$4) AND r.id>$5`
	args := []any{w, p.UserID, p.OperationID, req.Type, req.After}
	order := "r.id"
	if req.Kind == "history" {
		version := req.Version
		if version == "" {
			version = "head"
		}
		root, err := s.Read(ctx, p, w, Ref{ID: req.ID, Version: version})
		if err != nil {
			return result, err
		}
		page.Root = &root.Ref
		var after uint64
		afterID := ""
		if req.After != "" {
			position, id, ok := strings.Cut(req.After, ":")
			if !ok {
				return result, ErrInvalid
			}
			var err error
			after, err = strconv.ParseUint(position, 10, 64)
			if err != nil {
				return result, ErrInvalid
			}
			afterID = id
		}
		sql = `SELECT r.id,r.version,r.owner_id,r.kind,r.position::text,r.payload,r.refs,lpad(r.position::text,20,'0')||':'||r.id FROM inspection_records r WHERE r.workspace_id=$1 AND r.owner_id=$4 AND r.position<=$6::bigint AND (r.position,r.id)>($5::bigint,$7::text)`
		args = []any{w, p.UserID, p.OperationID, root.Owner, after, root.Position, afterID}
		// Launches do not advance the definition's own version. Pin the stream bound
		// separately from the root reference so subsequent pages use the same range.
		var sequence uint64
		if err := s.Store.Database().QueryRow(ctx, `SELECT sequence FROM shared_streams WHERE workspace_id=$1`, w).Scan(&sequence); err != nil {
			return result, err
		}
		through := sequence
		if req.Through != "" {
			var err error
			through, err = strconv.ParseUint(req.Through, 10, 64)
			if err != nil || through > sequence {
				return result, ErrInvalid
			}
		}
		page.Through = strconv.FormatUint(through, 10)
		args[5] = page.Through
		order = "r.position,r.id"
	} else if req.Kind != "heads" {
		return result, ErrInvalid
	}
	sql += ` AND ` + access.ReadFilterSQL(`r.refs||jsonb_build_array(jsonb_build_object('id',r.id,'version',r.version),jsonb_build_object('id',r.owner_id,'version',r.version))`) + ` ORDER BY ` + order
	sql += ` LIMIT $` + strconv.Itoa(len(args)+1)
	args = append(args, req.Limit+1)
	rows, err := s.Store.Database().Query(ctx, sql, args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	keys := []string{}
	for rows.Next() {
		var r Record
		var refs []byte
		var key string
		if err = rows.Scan(&r.ID, &r.Version, &r.Owner, &r.Kind, &r.Position, &r.Value, &refs, &key); err != nil {
			return result, err
		}
		if json.Unmarshal(refs, &r.Refs) != nil {
			return result, ErrInvalid
		}
		page.Items = append(page.Items, r)
		keys = append(keys, key)
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	if len(page.Items) > req.Limit {
		page.Items = page.Items[:req.Limit]
		page.Next = keys[req.Limit-1]
	}
	result.Data = &page
	result.Meta = apicontract.Meta{WorkspaceID: w, Completeness: apicontract.Completeness{State: "complete"}}
	if page.Next != "" {
		result.Meta.NextCursor = &page.Next
	}
	return result, nil
}
func reply(w http.ResponseWriter, workspace string, v any, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	status := 200
	if err != nil {
		status = 503
		code := "unavailable"
		switch {
		case errors.Is(err, access.ErrForbidden):
			status = 403
			code = "forbidden"
		case errors.Is(err, ErrInvalid), errors.Is(err, runs.ErrInvalid):
			status = 400
			code = "invalid_request"
		case errors.Is(err, ErrState), errors.Is(err, runs.ErrState):
			status = 409
			code = "state_conflict"
		}
		v = apicontract.Response[any]{Error: &apicontract.Problem{Code: code}, Meta: apicontract.Meta{WorkspaceID: workspace, Completeness: apicontract.Completeness{State: "pending", Reason: code}}}
	}
	if r, ok := v.(apicontract.Response[apicontract.Reference]); ok && r.Error != nil {
		status = 409
		if r.Error.Code == "forbidden" {
			status = 403
		}
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func (s *Service) Register(mux *http.ServeMux) {
	base := "/inspections/workspaces/{workspace}"
	route := func(method, path string, handle func(context.Context, access.Principal, string, *http.Request) (any, error)) {
		mux.HandleFunc(method+" "+base+path, func(w http.ResponseWriter, r *http.Request) {
			user, ok := s.Access.User(w, r)
			if !ok {
				return
			}
			v, err := handle(r.Context(), access.Principal{UserID: user}, r.PathValue("workspace"), r)
			reply(w, r.PathValue("workspace"), v, err)
		})
	}
	body := func(r *http.Request, v any) error {
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		if d.Decode(v) != nil {
			return ErrInvalid
		}
		return nil
	}
	route("GET", "/records/{id}", func(ctx context.Context, p access.Principal, w string, r *http.Request) (any, error) {
		version := r.URL.Query().Get("version")
		if version == "" {
			version = "head"
		}
		return s.Read(ctx, p, w, Ref{ID: r.PathValue("id"), Version: version})
	})
	route("POST", "/records/{id}", func(ctx context.Context, p access.Principal, w string, r *http.Request) (any, error) {
		var req Write
		if err := body(r, &req); err != nil {
			return nil, err
		}
		return s.Put(ctx, p, w, r.PathValue("id"), req)
	})
	route("POST", "/adopt", func(ctx context.Context, p access.Principal, w string, r *http.Request) (any, error) {
		var req Adopt
		if err := body(r, &req); err != nil {
			return nil, err
		}
		return s.Adopt(ctx, p, w, req)
	})
	route("POST", "/start/{id}", func(ctx context.Context, p access.Principal, w string, r *http.Request) (any, error) {
		var req Start
		if err := body(r, &req); err != nil {
			return nil, err
		}
		return s.Start(ctx, p, w, r.PathValue("id"), req)
	})
	route("GET", "/current/{id}", func(ctx context.Context, p access.Principal, w string, r *http.Request) (any, error) {
		return s.Current(ctx, p, w, r.PathValue("id"))
	})
	route("POST", "/judgments/{id}", func(ctx context.Context, p access.Principal, w string, r *http.Request) (any, error) {
		var req Decide
		if err := body(r, &req); err != nil {
			return nil, err
		}
		return s.Decide(ctx, p, w, r.PathValue("id"), req)
	})
	route("POST", "/query", func(ctx context.Context, p access.Principal, w string, r *http.Request) (any, error) {
		var req Request
		if err := body(r, &req); err != nil {
			return nil, err
		}
		return s.Query(ctx, p, w, req)
	})
}
