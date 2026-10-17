package mcpapi

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
)

// These controls exist only in the disposable test server. No production fault
// endpoint, process signal, firewall rule or developer DB is involved.
type reconnectFixture struct{ drop atomic.Bool }

func (f *reconnectFixture) handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/report") && f.drop.CompareAndSwap(true, false) {
			saved := httptest.NewRecorder()
			next.ServeHTTP(saved, r)
			if saved.Code == 200 {
				conn, _, err := w.(http.Hijacker).Hijack()
				if err == nil {
					_ = conn.Close()
					return
				}
			}
			for key, values := range saved.Header() {
				w.Header()[key] = values
			}
			w.WriteHeader(saved.Code)
			_, _ = w.Write(saved.Body.Bytes())
			return
		}
		next.ServeHTTP(w, r)
	})
}
func databaseFixture(ctx context.Context, t *testing.T, a *access.Service, c *contexts.Service) func() (*runs.Service, error) {
	return func() (*runs.Service, error) {
		store, err := persistence.Open(ctx, os.Getenv("KOUJOU_TEST_DATABASE_URL"))
		if err != nil {
			return nil, err
		}
		t.Cleanup(store.Close)
		return runs.New(store, a, c), nil
	}
}
func (f *reconnectFixture) control(t *testing.T, current **httptest.Server, handler func(*runs.Service) http.Handler, reopen func() (*runs.Service, error)) func(string) error {
	t.Helper()
	var mu sync.Mutex
	address := (*current).Listener.Addr().String()
	tlsConfig := (*current).TLS.Clone()
	return func(action string) error {
		mu.Lock()
		defer mu.Unlock()
		switch action {
		case "drop-report-response":
			f.drop.Store(true)
		case "api-stop":
			(*current).Close()
		case "api-start":
			runService, err := reopen()
			if err != nil {
				return err
			}
			listener, err := net.Listen("tcp", address)
			if err != nil {
				return err
			}
			server := httptest.NewUnstartedServer(f.handler(handler(runService)))
			_ = server.Listener.Close()
			server.Listener = listener
			server.TLS = tlsConfig.Clone()
			server.StartTLS()
			*current = server
		}
		return nil
	}
}
