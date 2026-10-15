package graphindex

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// Faults affect only the dedicated test server's HTTP responses. Successful
// commits really reach ArcadeDB before their acknowledgement is discarded.
type graphFault struct {
	mu               sync.Mutex
	mode             string
	nodeBatches      int
	reached, release chan struct{}
}

func (f *graphFault) arm(mode string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mode = mode
	f.nodeBatches = 0
}
func (f *graphFault) proxy(t *testing.T, address string) *httptest.Server {
	t.Helper()
	target, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) { w.WriteHeader(503) }
	proxy.ModifyResponse = func(r *http.Response) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		mode := f.mode
		commit := strings.Contains(r.Request.URL.Path, "/commit/")
		raw, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(raw))
		req, _ := r.Request.GetBody()
		var command []byte
		if req != nil {
			command, _ = io.ReadAll(req)
			_ = req.Close()
		}
		sql := ""
		if len(command) > 0 {
			sql = string(command)
		}
		if mode == "lost_commit" && commit {
			f.mode = ""
			return errors.New("fixture lost committed response")
		}
		if mode == "pause_snapshot" && strings.Contains(sql, "FOREACH (row IN :nodes)") {
			f.mode = ""
			close(f.reached)
			<-f.release
		}
		if mode == "fail_second_batch" && strings.Contains(sql, "FOREACH (row IN :nodes)") {
			f.nodeBatches++
			if f.nodeBatches == 3 {
				f.mode = ""
				return errors.New("fixture interrupted snapshot")
			}
		}
		if strings.Contains(sql, "FROM KoujouEdge") && strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-ndjson") {
			if mode == "truncated" {
				raw = bytes.Replace(raw, []byte(`"truncated":false`), []byte(`"truncated":true`), 1)
				f.mode = ""
			}
			if mode == "cut" {
				lines := bytes.Split(raw, []byte("\n"))
				raw = nil
				for _, line := range lines {
					if !bytes.Contains(line, []byte(`"stats"`)) {
						raw = append(raw, line...)
						raw = append(raw, '\n')
					}
				}
				f.mode = ""
			}
			r.Body = io.NopCloser(bytes.NewReader(raw))
			r.ContentLength = int64(len(raw))
			r.Header.Del("Content-Length")
		}
		return nil
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(raw))
		f.mu.Lock()
		mode := f.mode
		if mode == "before_checkpoint" && bytes.Contains(raw, []byte("UPDATE KoujouHeader SET")) && bytes.Contains(raw, []byte("cp_sequence")) {
			f.mode = ""
			f.mu.Unlock()
			w.WriteHeader(503)
			return
		}
		f.mu.Unlock()
		r.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(raw)), nil }
		proxy.ServeHTTP(w, r)
	}))
}
