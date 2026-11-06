package repositories

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
)

type ContentRequest struct {
	Repository string `json:"repository"`
	Commit     string `json:"commit,omitempty"`
	Path       string `json:"path,omitempty"`
	Page       int    `json:"page,omitempty"`
	Offset     int64  `json:"offset,omitempty"`
}
type TreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
	Size int64  `json:"size"`
}
type Content struct {
	Repository string      `json:"repository"`
	Commit     string      `json:"commit"`
	Path       string      `json:"path"`
	URL        string      `json:"url"`
	State      string      `json:"state"`
	Entries    []TreeEntry `json:"entries,omitempty"`
	Text       string      `json:"text,omitempty"`
	Offset     int64       `json:"offset,omitempty"`
	NextOffset int64       `json:"next_offset,omitempty"`
	NextPage   int         `json:"next_page,omitempty"`
}

var contentCommit = regexp.MustCompile(`^[0-9a-f]{40}$`)

// ReadContent uses the registered origin and credentials; callers cannot provide a URL.
// Trees are paged, and file content is streamed in bounded UTF-8 chunks.
func (s *Service) ReadContent(ctx context.Context, p access.Principal, w string, req ContentRequest) (Content, error) {
	out := Content{Repository: req.Repository, Commit: req.Commit, Path: req.Path, Offset: req.Offset, State: "unavailable"}
	if req.Page < 0 || req.Offset < 0 || (req.Commit != "" && !contentCommit.MatchString(req.Commit)) || (req.Path != "" && (path.Clean(req.Path) != req.Path || strings.HasPrefix(req.Path, "/") || strings.Contains(req.Path, "\\") || req.Path == ".." || strings.HasPrefix(req.Path, "../"))) {
		return out, ErrInvalid
	}
	r, err := s.Read(ctx, p, w, req.Repository)
	if err != nil {
		return out, err
	}
	current, client, err := s.Repository(ctx, w, r.ID)
	if err != nil || current.Connection != r.Connection || current.RepositoryID != r.RepositoryID {
		return out, ErrState
	}
	var remote struct {
		ID       int64  `json:"id"`
		FullName string `json:"full_name"`
		Branch   string `json:"default_branch"`
	}
	if err = client.Get(ctx, "/repos/"+r.FullName, &remote); err != nil {
		return out, nil
	}
	if remote.ID != r.RepositoryID || remote.FullName != r.FullName {
		return out, ErrState
	}
	if req.Commit == "" {
		var commit struct {
			SHA string `json:"sha"`
		}
		if err = client.Get(ctx, "/repos/"+r.FullName+"/commits/"+url.PathEscape(remote.Branch), &commit); err != nil {
			return out, nil
		}
		if !contentCommit.MatchString(commit.SHA) {
			return out, ErrState
		}
		req.Commit, out.Commit = commit.SHA, commit.SHA
	}
	parts := strings.Split(req.Path, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	escaped := strings.Join(parts, "/")
	out.URL = strings.TrimRight(r.Instance, "/") + "/" + r.FullName + "/src/commit/" + req.Commit + "/" + escaped
	if req.Path == "" {
		if req.Page == 0 {
			req.Page = 1
		}
		var tree struct {
			Entries   []TreeEntry `json:"tree"`
			Truncated bool        `json:"truncated"`
		}
		if err = client.Get(ctx, "/repos/"+r.FullName+"/git/trees/"+req.Commit+"?recursive=true&per_page=50&page="+strconv.Itoa(req.Page), &tree); err != nil {
			return out, nil
		}
		out.Entries, out.State = tree.Entries, "read"
		if tree.Truncated {
			out.NextPage = req.Page + 1
		}
	} else {
		const chunk = 32768
		response, err := client.ReadRange(ctx, "/repos/"+r.FullName+"/raw/"+escaped+"?ref="+req.Commit, fmt.Sprintf("bytes=%d-%d", req.Offset, req.Offset+chunk))
		if err != nil {
			return out, nil
		}
		defer response.Body.Close()
		if response.StatusCode == 206 {
			var start, end, total int64
			if n, err := fmt.Sscanf(response.Header.Get("Content-Range"), "bytes %d-%d/%d", &start, &end, &total); err != nil || n != 3 || start != req.Offset || end < start || end > req.Offset+chunk || total <= end {
				return out, ErrState
			}
		} else if response.StatusCode != 200 || req.Offset != 0 {
			return out, nil
		}
		data, err := io.ReadAll(io.LimitReader(response.Body, chunk+1))
		if err != nil {
			return out, nil
		}
		more := len(data) > chunk
		if more {
			data = data[:chunk]
		}
		// Keep a multibyte character at a chunk boundary for the next request.
		for i := 0; more && !utf8.Valid(data) && i < utf8.UTFMax-1 && len(data) > 0; i++ {
			data = data[:len(data)-1]
		}
		if !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
			out.State = "binary"
			return out, nil
		}
		out.Text, out.State = string(data), "read"
		if more {
			out.NextOffset = req.Offset + int64(len(data))
		}
	}
	latest, err := s.Read(ctx, p, w, r.ID)
	if err != nil || latest.Version != r.Version {
		return Content{}, access.ErrForbidden
	}
	return out, nil
}
