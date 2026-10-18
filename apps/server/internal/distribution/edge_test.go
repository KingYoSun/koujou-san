package distribution

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEdgePublicAndAdministrationStaySeparate(t *testing.T) {
	backend := func(kind string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Forwarded-Proto") != "https" || r.Header.Get("X-Forwarded-Host") != "example.test" || r.Header.Get("Forwarded") != "" {
				t.Error("untrusted forwarded headers")
			}
			io.WriteString(w, kind+":"+r.URL.Path)
		}))
	}
	api := backend("api")
	defer api.Close()
	pub := backend("public")
	defer pub.Close()
	admin := backend("private")
	defer admin.Close()
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "web"), 0700)
	os.WriteFile(filepath.Join(root, "web/index.html"), []byte("application"), 0600)
	os.WriteFile(filepath.Join(root, "key.pem"), []byte("private-key-fixture"), 0600)
	front, back, err := Edge(filepath.Join(root, "web"), "https://example.test", api.URL, pub.URL, pub.URL, admin.URL, admin.URL)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		handler    http.Handler
		path, want string
		status     int
	}{
		{front, "/auth/login", "application", 200}, {front, "/auth/config", "api:/auth/config", 200}, {front, "/oidc/.well-known/openid-configuration", "public:/.well-known/openid-configuration", 200}, {front, "/auth/identity/self-service/login/browser", "public:/self-service/login/browser", 200}, {back, "/hydra/admin/clients", "private:/admin/clients", 200}, {front, "/hydra/admin/clients", "", 404}, {front, "/admin/identities", "", 404}, {front, "/../key.pem", "", 307}, {front, "/key.pem", "application", 200}, {front, "/%2e%2e/key.pem", "", 404},
	} {
		r := httptest.NewRequest("GET", test.path, nil)
		r.Header.Set("X-Forwarded-Proto", "http")
		r.Header.Set("X-Forwarded-Host", "attacker")
		r.Header.Set("Forwarded", "proto=http;host=attacker")
		w := httptest.NewRecorder()
		test.handler.ServeHTTP(w, r)
		body := w.Body.String()
		if w.Code != test.status || test.want != "" && !strings.Contains(body, test.want) || strings.Contains(body, "private-key-fixture") {
			t.Fatalf("%s: %d %s", test.path, w.Code, body)
		}
	}
}
