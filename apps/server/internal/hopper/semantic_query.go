package hopper

import (
	"context"
	"encoding/json"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/serverllm"
)

func (s *Service) Connections(ctx context.Context, p access.Principal, w string) ([]serverllm.Connection, error) {
	if err := s.Access.Check(ctx, p, w, "", "read"); err != nil {
		return nil, err
	}
	return s.Gateway.Connections(w)
}

func compactSemanticRecord(r Record) Record {
	switch r.Kind {
	case "model-input":
		var input SemanticInput
		if json.Unmarshal(r.Value, &input) != nil {
			return r
		}
		r.Value = raw(map[string]any{"state": input.State, "reason": input.Reason, "source": map[string]any{"target": input.Source.Target, "event": input.Source.Event}, "definition": input.Definition, "manifest": input.Manifest, "digest": input.Digest, "evidence_count": len(input.Evidence)})
	case "model-attempt":
		var attempt modelAttempt
		if json.Unmarshal(r.Value, &attempt) != nil {
			return r
		}
		if attempt.Response != nil {
			attempt.Response.Body = nil
		}
		r.Value = raw(attempt)
	default:
		return r
	}
	r.Refs = []Ref{}
	r.MetadataOnly = true
	return r
}
