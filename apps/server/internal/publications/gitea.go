package publications

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

func (r *Remote) lookupGitea(ctx context.Context, c Connection, tag string) Outcome {
	digest := strings.TrimSuffix(strings.TrimPrefix(tag, "<!-- koujou-san-request:"), " -->")
	var matches []Issue
	for page := 1; ; page++ {
		path := fmt.Sprintf("/repos/%s/issues?state=all&type=issues&q=%s&limit=100&page=%d", c.Repository, url.QueryEscape(digest), page)
		response, err := r.send(ctx, c, "GET", path, "", nil)
		if err != nil {
			return outcome("unknown", "lookup_interrupted", nil)
		}
		var items []Issue
		decodeErr := json.NewDecoder(response.Body).Decode(&items)
		_ = response.Body.Close()
		if response.StatusCode != 200 || decodeErr != nil {
			return outcome("unknown", "lookup_unavailable", nil)
		}
		for _, item := range items {
			if validIssue(c, item) && strings.Contains(item.Body, tag) {
				matches = append(matches, item)
			}
		}
		if len(matches) > 1 {
			return outcome("unknown", "lookup_unconfirmed", nil)
		}
		if len(items) < 100 {
			break
		}
	}
	if len(matches) != 1 {
		return outcome("unknown", "lookup_unconfirmed", nil)
	}
	confirmed := r.get(ctx, c, "", matches[0].Number)
	if confirmed.Issue != nil && !strings.Contains(confirmed.Issue.Body, tag) {
		return outcome("unknown", "marker_not_found", nil)
	}
	return confirmed
}

func (r *Remote) giteaLabelID(ctx context.Context, c Connection, name string) (int64, error) {
	for page := 1; ; page++ {
		response, err := r.send(ctx, c, "GET", fmt.Sprintf("/repos/%s/labels?limit=100&page=%d", c.Repository, page), "", nil)
		if err != nil {
			return 0, err
		}
		var labels []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		}
		decodeErr := json.NewDecoder(response.Body).Decode(&labels)
		_ = response.Body.Close()
		if response.StatusCode != 200 || decodeErr != nil {
			return 0, ErrState
		}
		for _, label := range labels {
			if label.Name == name && label.ID > 0 {
				return label.ID, nil
			}
		}
		if len(labels) < 100 {
			return 0, ErrInvalid
		}
	}
}
