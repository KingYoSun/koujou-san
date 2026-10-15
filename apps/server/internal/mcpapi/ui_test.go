package mcpapi

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// Optional Computer Use fixture. Only this isolated test identity is forwarded;
// no development-user credential or browser certificate exception is needed.
func inspectUI(t *testing.T, client *http.Client, target, token string) {
	t.Helper()
	if os.Getenv("KOUJOU_TEST_MCP_UI") != "1" {
		return
	}
	if _, err := os.Stat("../../../client/dist/index.html"); err != nil {
		t.Fatal("build the common UI before UI inspection")
	}
	base, _ := url.Parse(target)
	proxy := httputil.NewSingleHostReverseProxy(base)
	proxy.Transport = client.Transport
	direct := proxy.Director
	proxy.Director = func(r *http.Request) {
		direct(r)
		r.Host = base.Host
		r.Header.Del("Cookie")
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Origin", target)
	}
	done := make(chan struct{}, 1)
	files := http.FileServer(http.Dir("../../../client/dist"))
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__fixture/finish" && r.Method == "POST" {
			select {
			case done <- struct{}{}:
			default:
			}
			w.WriteHeader(204)
			return
		}
		for _, prefix := range []string{"/auth/me", "/auth/config", "/auth/csrf", "/access/", "/mcp/control/", "/contexts/", "/sync/", "/healthz", "/readyz"} {
			if strings.HasPrefix(r.URL.Path, prefix) {
				proxy.ServeHTTP(w, r)
				return
			}
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			files.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, "../../../client/dist/index.html")
	}))
	defer fixture.Close()
	t.Logf("Computer Use fixture: %s (isolated identity, expires after five minutes); POST /__fixture/finish ends inspection", fixture.URL)
	select {
	case <-done:
		t.Log("Computer Use inspection finished")
	case <-time.After(5 * time.Minute):
		t.Fatal("UI inspection timed out")
	}
}
