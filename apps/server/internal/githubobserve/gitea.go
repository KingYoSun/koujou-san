package githubobserve

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

func (r *Remote) observeStatus(ctx context.Context, t Target, s *Snapshot) error {
	for page := 1; ; page++ {
		var status struct {
			SHA      string `json:"sha"`
			State    string `json:"state"`
			Total    int    `json:"total_count"`
			Statuses []struct {
				ID      int64  `json:"id"`
				Name    string `json:"context"`
				State   string `json:"status"`
				Updated string `json:"updated_at"`
			} `json:"statuses"`
		}
		if err := r.get(ctx, t, fmt.Sprintf("/repos/%s/commits/%s/status?limit=100&page=%d", t.Repository, t.ExternalID, page), &status); err != nil {
			return err
		}
		if status.SHA != t.ExternalID || status.State == "" {
			return errors.New("invalid_source_response")
		}
		if page > 1 && s.Conclusion != status.State {
			return errors.New("changed_during_read")
		}
		s.Commit, s.Conclusion, s.State = status.SHA, status.State, "completed"
		if status.State == "pending" {
			s.State = "in_progress"
		}
		s.URL = t.Instance + "/" + t.Repository + "/commit/" + status.SHA
		for _, item := range status.Statuses {
			state := "completed"
			if item.State == "pending" {
				state = "in_progress"
			}
			s.Jobs = append(s.Jobs, Job{ID: item.ID, Name: item.Name, Status: state, Conclusion: item.State})
			if item.Updated > s.SourceUpdatedAt {
				s.SourceUpdatedAt = item.Updated
			}
		}
		if status.Total == 0 {
			return errors.New("status_unobserved")
		}
		if len(s.Jobs) >= status.Total {
			return nil
		}
		if len(status.Statuses) == 0 {
			return errors.New("fetch_interrupted")
		}
	}
}

func (s *Service) giteaWebhookHTTP(w http.ResponseWriter, r *http.Request) {
	if s.config == nil {
		w.WriteHeader(404)
		return
	}
	for _, client := range s.config.clients {
		if client.WebhookID() != r.PathValue("instance") || len(client.Secret) == 0 {
			continue
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 25<<20))
		if err != nil {
			w.WriteHeader(400)
			return
		}
		mac := hmac.New(sha256.New, client.Secret)
		_, _ = mac.Write(body)
		signature, err := hex.DecodeString(r.Header.Get("X-Gitea-Signature"))
		if err != nil || !hmac.Equal(signature, mac.Sum(nil)) {
			w.WriteHeader(401)
			return
		}
		var event struct {
			Repository struct {
				ID int64 `json:"id"`
			} `json:"repository"`
		}
		if json.Unmarshal(body, &event) != nil {
			w.WriteHeader(400)
			return
		}
		_, err = s.store.Database().Exec(r.Context(), `UPDATE github_targets SET due_at=LEAST(due_at,now()) WHERE active AND spec->>'provider'='gitea' AND spec->>'instance'=$1 AND repository_id=$2`, client.URL, event.Repository.ID)
		if err != nil {
			w.WriteHeader(503)
			return
		}
		select {
		case s.wake <- struct{}{}:
		default:
		}
		w.WriteHeader(202)
		return
	}
	w.WriteHeader(404)
}
