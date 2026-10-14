package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthz(t *testing.T) {
	server := httptest.NewServer(NewHandler())
	defer server.Close()

	response, err := server.Client().Get(server.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("healthz status = %d", response.StatusCode)
	}
	if response.Header.Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("healthz content type = %q", response.Header.Get("Content-Type"))
	}
	var body struct {
		Status  string `json:"status"`
		Service string `json:"service"`
		Stage   string `json:"stage"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "ok" || body.Service != "koujou-san-api" || body.Stage != "bootstrap" {
		t.Fatalf("unexpected health response: %+v", body)
	}
}

func TestHealthzDoesNotExposeApplicationRoutes(t *testing.T) {
	for _, path := range []string{"/", "/workspaces", "/runs", "/healthz/other"} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			NewHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code != http.StatusNotFound {
				t.Fatalf("%s status = %d, want 404", path, response.Code)
			}
		})
	}
}

func TestHealthzRejectsWriteMethods(t *testing.T) {
	response := httptest.NewRecorder()
	NewHandler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/healthz", nil))
	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("unexpected method response: status=%d allow=%q", response.Code, response.Header().Get("Allow"))
	}
}

func TestHealthzCorsRemainsLocal(t *testing.T) {
	for _, origin := range []string{"http://127.0.0.1:1420", "http://tauri.localhost", "https://example.com"} {
		t.Run(origin, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			request.Header.Set("Origin", origin)
			response := httptest.NewRecorder()
			NewHandler().ServeHTTP(response, request)
			allowed := response.Header().Get("Access-Control-Allow-Origin")
			if origin == "https://example.com" && allowed != "" {
				t.Fatalf("external origin was allowed: %q", allowed)
			}
			if origin != "https://example.com" && allowed != origin {
				t.Fatalf("local origin was not allowed: %q", allowed)
			}
		})
	}
}
