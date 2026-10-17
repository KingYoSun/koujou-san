package runs

import (
	"context"
	"errors"
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
)

func ValidRepository(r *Repository) bool {
	return r == nil || r.ID != "" && commitID.MatchString(r.Commit)
}

// SelectRunner only selects currently advertised, permitted candidates. Assign
// repeats all authority, input, connection and capability checks when committing.
func (s *Service) SelectRunner(ctx context.Context, p access.Principal, w, id, preferred string, repo *Repository, required []string) (string, error) {
	var q query = s.Store.Database()
	if s.tx != nil {
		q = s.tx
	}
	r, _, err := loadRun(ctx, q, w, id)
	if err != nil {
		return "", err
	}
	spec, err := s.input(ctx, q, w, r)
	if err != nil {
		return "", err
	}
	after := ""
	for {
		rows, err := q.Query(ctx, `SELECT r.device_id FROM run_runners r JOIN access_devices d ON d.workspace_id=r.workspace_id AND d.id=r.device_id WHERE r.workspace_id=$1 AND r.device_id>$2 AND ($3='' OR r.device_id=$3) AND d.enabled AND d.can_input AND d.can_report AND (r.payload->>'expires_at')::timestamptz>now() AND EXISTS(SELECT 1 FROM jsonb_array_elements(r.payload->'connections') c WHERE c->>'kind'=$4 AND c->>'target'=$5 AND c->>'confirmed'='true' AND c->'users' ? $6 AND ($7='' OR c->'repositories' ? $7)) ORDER BY r.device_id LIMIT 50`, w, after, preferred, spec.Input.Command.Kind, spec.Input.Command.Target, p.UserID, func() string {
			if repo != nil {
				return repo.ID
			}
			return ""
		}())
		if err != nil {
			return "", err
		}
		devices := []string{}
		for rows.Next() {
			var device string
			if err = rows.Scan(&device); err != nil {
				break
			}
			devices = append(devices, device)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return "", err
		}
		for _, device := range devices {
			_, _, reason, err := s.eligible(ctx, q, w, p.UserID, device, spec, repo, required)
			if err != nil && !errors.Is(err, access.ErrForbidden) {
				return "", err
			}
			if err == nil && reason == "" {
				return device, nil
			}
		}
		if len(devices) < 50 {
			return "", nil
		}
		after = devices[len(devices)-1]
	}
}
