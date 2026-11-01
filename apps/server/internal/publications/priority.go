package publications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/automation"
	"github.com/KingYoSun/koujou-san/apps/server/internal/githubapp"
)

// ReadPriority shares the existing limited installation credential. Each call
// reads one events page; the caller persists continuation and reconciles writes.
func (r *Remote) ReadPriority(ctx context.Context, w, id string, number int64, page int) (automation.ExternalSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if time.Now().Before(r.resumeAt) {
		return automation.ExternalSnapshot{}, ErrState
	}
	out := automation.ExternalSnapshot{Labels: []string{}, Events: []automation.LabelEvent{}}
	c, ok := r.Connection(w, id)
	if !ok || number < 1 || page < 1 {
		return out, ErrInvalid
	}
	token, err := r.token(ctx, c)
	if err != nil {
		return out, err
	}
	path := "/repos/" + c.Repository + "/issues/" + fmt.Sprint(number)
	eventsPath := path + "/events?per_page=100&page=" + fmt.Sprint(page)
	if c.Provider == "gitea" {
		if c.ActorID < 1 {
			return out, ErrInvalid
		}
		eventsPath = path + "/timeline?limit=100&page=" + fmt.Sprint(page)
	}
	events, err := r.send(ctx, c, "GET", eventsPath, token, nil)
	if err != nil || events.StatusCode != 200 {
		return out, ErrState
	}
	defer events.Body.Close()
	var rows []struct {
		ID    int64  `json:"id"`
		Event string `json:"event"`
		Type  string `json:"type"`
		Body  string `json:"body"`
		User  struct {
			ID int64 `json:"id"`
		} `json:"user"`
		Label struct {
			Name string `json:"name"`
		} `json:"label"`
		App *struct {
			ID int64 `json:"id"`
		} `json:"performed_via_github_app"`
		Actor struct {
			ID   int64
			Type string
		} `json:"actor"`
	}
	if json.NewDecoder(io.LimitReader(events.Body, 2<<20)).Decode(&rows) != nil {
		return out, ErrState
	}
	for _, row := range rows {
		own := row.App != nil && strconv.FormatInt(row.App.ID, 10) == r.config.AppID
		if c.Provider == "gitea" {
			if row.Type != "label" {
				continue
			}
			if row.Body == "1" {
				row.Event = "labeled"
			} else if row.Body == "" {
				row.Event = "unlabeled"
			} else {
				return out, ErrState
			}
			own = row.User.ID == c.ActorID
		} else if row.App == nil && row.Actor.Type == "Bot" {
			if r.botID == 0 {
				r.botID, err = githubapp.Bot(ctx, r.config.AppID, r.config.key, token, r.request)
				if err != nil {
					return out, err
				}
			}
			own = row.Actor.ID == r.botID
		}
		out.Events = append(out.Events, automation.LabelEvent{ID: row.ID, Action: row.Event, Label: row.Label.Name, Own: own})
	}
	out.More = len(rows) == 100
	response, err := r.send(ctx, c, "GET", path, token, nil)
	if err != nil || response.StatusCode != 200 {
		return out, ErrState
	}
	defer response.Body.Close()
	var issue struct {
		Issue
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&issue) != nil || issue.ID <= 0 || issue.Number != number || !plainIssue(c, issue.Issue) || issue.UpdatedAt.IsZero() {
		return out, ErrState
	}
	for _, label := range issue.Labels {
		out.Labels = append(out.Labels, label.Name)
	}
	out.Title, out.Body, out.State, out.UpdatedAt = issue.Title, issue.Body, issue.State, issue.UpdatedAt
	return out, nil
}

func (r *Remote) WritePriority(ctx context.Context, w, id string, number int64, action, label string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if time.Now().Before(r.resumeAt) {
		return ErrState
	}
	c, ok := r.Connection(w, id)
	if !ok || number < 1 || label == "" {
		return ErrInvalid
	}
	token, err := r.token(ctx, c)
	if err != nil {
		return err
	}
	path := "/repos/" + c.Repository + "/issues/" + fmt.Sprint(number) + "/labels"
	method := http.MethodPost
	var body []byte
	if action == "labeled" {
		body = raw(map[string]any{"labels": []string{label}})
	} else if action == "unlabeled" {
		method = http.MethodDelete
		path += "/" + url.PathEscape(label)
	} else {
		return ErrInvalid
	}
	if c.Provider == "gitea" {
		id, err := r.giteaLabelID(ctx, c, label)
		if err != nil {
			return err
		}
		if action == "labeled" {
			body = raw(map[string]any{"labels": []int64{id}})
		} else {
			path = "/repos/" + c.Repository + "/issues/" + fmt.Sprint(number) + "/labels/" + fmt.Sprint(id)
		}
	}
	response, err := r.send(ctx, c, method, path, token, bytes.NewReader(body))
	if response != nil {
		defer response.Body.Close()
	}
	if err != nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return ErrState
	}
	return nil
}
