package workflows

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
)

func (s *Service) Capabilities(ctx context.Context, p access.Principal, w, device, version string) (runs.Runner, error) {
	return s.capabilities(ctx, s.Store.Database(), p, w, device, version)
}
func (s *Service) capabilities(ctx context.Context, q query, p access.Principal, w, device, version string) (runs.Runner, error) {
	var result runs.Runner
	var payload []byte
	id := "runner:" + device
	if version == "head" {
		if err := q.QueryRow(ctx, `SELECT payload FROM run_runners WHERE workspace_id=$1 AND device_id=$2`, w, device).Scan(&payload); err != nil {
			return result, access.ErrForbidden
		}
	} else {
		if err := q.QueryRow(ctx, `SELECT payload FROM shared_changes WHERE workspace_id=$1 AND resource_id=$2 AND version=$3 AND kind='runner-contact'`, w, id, version).Scan(&payload); err != nil {
			return result, access.ErrForbidden
		}
	}
	if json.Unmarshal(payload, &result) != nil {
		return result, ErrInvalid
	}
	if err := s.Access.Check(ctx, p, w, id, "get"); err != nil {
		return runs.Runner{}, err
	}
	if err := s.checkRefs(ctx, q, p, w, []Ref{result.Ref}); err != nil {
		return runs.Runner{}, err
	}
	return result, nil
}
func (s *Service) capabilityProblems(ctx context.Context, q query, p access.Principal, w string, d Definition) ([]Problem, []Ref, error) {
	problems := []Problem{}
	checked := []Ref{}
	if d.Schema != "run-definition-v1" || d.Spec == nil {
		return problems, checked, nil
	}
	if d.DeviceID == "" {
		if len(d.Required) > 0 {
			problems = append(problems, Problem{"/device_id", "capability_unconfirmed"})
		}
		return problems, checked, nil
	}
	runner, err := s.capabilities(ctx, q, p, w, d.DeviceID, "head")
	if err != nil {
		return nil, nil, err
	}
	checked = append(checked, runner.Ref)
	for _, connection := range runner.Connections {
		if connection.Kind == d.Spec.Input.Command.Kind && connection.Target == d.Spec.Input.Command.Target {
			gap := ""
			if !connection.Confirmed {
				gap = "capability_unconfirmed"
			} else {
				needs := append([]string{}, d.Required...)
				if d.Write != nil {
					needs = append(needs, "conditional-write")
				}
				gap = runs.CapabilityGap(connection, d.Spec.Input.Command, needs)
			}
			if gap != "" {
				problems = append(problems, Problem{"/required", gap})
			}
			return problems, checked, nil
		}
	}
	return append(problems, Problem{"/spec/input/command/target", "connection_missing"}), checked, nil
}
func prefixProblems(path string, problems []Problem) []Problem {
	for i := range problems {
		problems[i].Path = strings.TrimSuffix(path, "/") + problems[i].Path
	}
	return problems
}
