package workflows

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
)

// Each page is bounded by the selected execution position. Run pages use their
// fixed task references; missing/denied sources never produce a complete export.
func (s *Service) exportHTTP(w http.ResponseWriter, r *http.Request) {
	user, ok := s.Access.User(w, r)
	if !ok {
		return
	}
	p := access.Principal{UserID: user}
	ws := r.PathValue("workspace")
	ref := Ref{ID: r.PathValue("id"), Version: r.URL.Query().Get("version")}
	if !fixed(ref) {
		reply(w, ws, nil, ErrInvalid)
		return
	}
	root, err := s.Read(r.Context(), p, ws, ref)
	if err != nil {
		reply(w, ws, nil, err)
		return
	}
	if root.Kind != "execution" {
		reply(w, ws, nil, ErrInvalid)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="koujou-workflow.ndjson"`)
	w.Header().Set("Cache-Control", "no-store")
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	enc := json.NewEncoder(w)
	emit := func(v any) bool { return enc.Encode(v) == nil }
	failure := func(err error) {
		code := "source_unavailable"
		if err == access.ErrForbidden {
			code = "forbidden"
		}
		emit(map[string]any{"type": "error", "code": code})
	}
	if !emit(map[string]any{"type": "begin", "schema": "workflow-export-v1", "execution": root.Ref}) {
		return
	}
	refs := append([]Ref{root.Ref}, root.Refs...)
	seen := map[Ref]bool{}
	emitWorkflow := func(record Record) bool { return emit(map[string]any{"type": "workflow-record", "record": record}) }
	// Definitions, validation/adoption and all previously adopted roots are fixed
	// refs. The graph does not expand arbitrary Context or external histories.
	for len(refs) > 0 {
		current := refs[0]
		refs = refs[1:]
		if seen[current] {
			continue
		}
		seen[current] = true
		if !strings.HasPrefix(current.ID, "workflow-") && !strings.HasPrefix(current.ID, "run-definition:") && !strings.HasPrefix(current.ID, "adoption:") && !strings.HasPrefix(current.ID, "validation:") {
			continue
		}
		record, err := s.Read(r.Context(), p, ws, current)
		if err != nil {
			failure(err)
			return
		}
		if !emitWorkflow(record) {
			return
		}
		refs = append(refs, record.Refs...)
	}
	req := Request{Kind: "history", ID: root.ID, Version: root.Version, Limit: 100}
	for {
		page, err := s.page(r.Context(), p, ws, req, true)
		if err != nil {
			failure(err)
			return
		}
		for _, record := range page.Data.Items {
			if !emitWorkflow(record) {
				return
			}
			if record.Kind != "task" {
				continue
			}
			var task Task
			_ = json.Unmarshal(record.Value, &task)
			if task.Run == nil || task.Run.Version == "0" {
				continue
			}
			rq := runs.Request{Kind: "all", RunID: task.Run.ID, Version: task.Run.Version, Limit: 100}
			for {
				rp, err := s.Runs.ExportPage(r.Context(), p, ws, rq)
				if err != nil {
					failure(err)
					return
				}
				for _, run := range rp.Data.Items {
					if !emit(map[string]any{"type": "run-record", "record": run}) {
						return
					}
				}
				if rp.Data.Next == "" {
					break
				}
				rq.After = rp.Data.Next
			}
		}
		if page.Data.Next == "" {
			break
		}
		req.After = page.Data.Next
	}
	if _, err = s.Read(r.Context(), p, ws, root.Ref); err != nil {
		failure(err)
		return
	}
	emit(map[string]any{"type": "complete", "execution": root.Ref})
}
