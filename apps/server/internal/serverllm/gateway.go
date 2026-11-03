// Package serverllm connects server-owned automation to configured model providers.
// It has no access to client or CLI credentials and owns no business decisions.
package serverllm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/chatgptplan"
	"github.com/KingYoSun/koujou-san/apps/server/internal/modelreply"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
)

var ErrUnavailable = errors.New("server_model_connection_unavailable")

type Model struct {
	Requested string   `json:"requested"`
	Actual    []string `json:"allowed_actual"`
}
type Connection struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	Endpoint       string  `json:"endpoint"`
	Revision       string  `json:"revision"`
	Models         []Model `json:"models"`
	KeyFile        string  `json:"-"`
	CAFile         string  `json:"-"`
	Type           string  `json:"type,omitempty"`
	CredentialFile string  `json:"-"`
	identity       string
	provider       *chatgptplan.Provider
	httpClient     *http.Client
}
type Gateway struct {
	ConfigFile string
	HTTP       *http.Client
	Store      *persistence.Store
}

func New(store *persistence.Store) *Gateway {
	return &Gateway{Store: store, ConfigFile: os.Getenv("KOUJOU_SERVER_LLM_CONFIG")}
}

func (g *Gateway) Connections(workspace string) ([]Connection, error) {
	return g.connections(workspace, false)
}
func (g *Gateway) connections(workspace string, catalog bool) ([]Connection, error) {
	out := []Connection{}
	if g == nil || g.ConfigFile == "" {
		return out, nil
	}
	b, err := os.ReadFile(g.ConfigFile)
	if err != nil {
		return nil, ErrUnavailable
	}
	var entries []struct {
		Connection
		Workspaces     []string `json:"workspaces"`
		KeyFile        string   `json:"key_file"`
		CAFile         string   `json:"ca_file"`
		CredentialFile string   `json:"credential_file"`
	}
	if json.Unmarshal(b, &entries) != nil {
		return nil, ErrUnavailable
	}
	grants := map[string]bool{}
	if !catalog && g.Store != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		rows, err := g.Store.Database().Query(ctx, `SELECT connection_id,allowed FROM server_model_workspace_grants WHERE workspace_id=$1`, workspace)
		if err != nil {
			return nil, ErrUnavailable
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			var allowed bool
			if rows.Scan(&id, &allowed) != nil {
				return nil, ErrUnavailable
			}
			grants[id] = allowed
		}
		if rows.Err() != nil {
			return nil, ErrUnavailable
		}
	}
	for _, e := range entries {
		p := e.Connection
		p.httpClient = g.HTTP
		if p.Type == "chatgpt" && p.Endpoint == "" {
			p.Endpoint = chatgptplan.Resource
		}
		u, err := url.Parse(p.Endpoint)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || p.ID == "" || len(p.Models) == 0 {
			return nil, ErrUnavailable
		}
		allowed := slices.Contains(e.Workspaces, workspace) || slices.Contains(e.Workspaces, "*")
		if granted, ok := grants[p.ID]; ok {
			allowed = granted
		}
		if !catalog && !allowed {
			continue
		}
		p.KeyFile = e.KeyFile
		p.CAFile = e.CAFile
		p.CredentialFile = e.CredentialFile
		if p.Type != "" && p.Type != "chatgpt" {
			return nil, ErrUnavailable
		}
		if p.Type == "chatgpt" {
			if p.Endpoint != chatgptplan.Resource || !filepath.IsAbs(p.CredentialFile) {
				return nil, ErrUnavailable
			}
			if credential, err := chatgptplan.Read(p.CredentialFile); err == nil && credential.Issuer == chatgptplan.Issuer {
				p.identity = credential.Identity()
			}
		}
		p.Revision = ""
		value, _ := json.Marshal(struct {
			Connection
			KeyFile, CAFile          string
			CredentialFile, Identity string `json:",omitempty"`
		}{p, e.KeyFile, e.CAFile, e.CredentialFile, p.identity})
		sum := sha256.Sum256(value)
		p.Revision = hex.EncodeToString(sum[:])
		out = append(out, p)
	}
	return out, nil
}

func (g *Gateway) Connection(workspace, id, revision string) (Connection, error) {
	entries, err := g.Connections(workspace)
	if err != nil {
		return Connection{}, err
	}
	for _, p := range entries {
		if p.ID == id && p.Revision == revision {
			return p, nil
		}
	}
	return Connection{}, ErrUnavailable
}

func (p Connection) Allows(requested string, actual []string) bool {
	return p.AllowsChain(requested, nil, actual)
}
func (p Connection) AllowsChain(requested string, fallbacks, actual []string) bool {
	if p.Type == "chatgpt" && len(fallbacks) != 0 {
		return false
	}
	allowed := []string{}
	for _, request := range append([]string{requested}, fallbacks...) {
		found := false
		for _, model := range p.Models {
			if model.Requested == request {
				allowed = append(allowed, model.Actual...)
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if len(actual) == 0 {
		return false
	}
	for _, name := range actual {
		if !slices.Contains(allowed, name) {
			return false
		}
	}
	return true
}

type Response struct {
	Terminal  string            `json:"terminal,omitempty"`
	Status    int               `json:"status"`
	Body      json.RawMessage   `json:"body"`
	RequestID string            `json:"request_id,omitempty"`
	TraceID   string            `json:"trace_id,omitempty"`
	Protocol  string            `json:"protocol,omitempty"`
	Request   json.RawMessage   `json:"request,omitempty"`
	Reply     *modelreply.Reply `json:"reply,omitempty"`
	Error     string            `json:"error,omitempty"`
}

func (p Connection) Call(ctx context.Context, request json.RawMessage, requestID string) (Response, error) {
	if p.Type == "chatgpt" {
		return p.responses(ctx, request, requestID)
	}
	key, err := os.ReadFile(p.KeyFile)
	if err != nil || len(bytes.TrimSpace(key)) == 0 {
		return Response{}, ErrUnavailable
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if p.CAFile != "" {
		cert, err := os.ReadFile(p.CAFile)
		if err != nil {
			return Response{}, ErrUnavailable
		}
		roots, _ := x509.SystemCertPool()
		if roots == nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(cert) {
			return Response{}, ErrUnavailable
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	}
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.Endpoint, "/")+"/chat/completions", bytes.NewReader(request))
	if err != nil {
		return Response{}, ErrUnavailable
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("x-bf-vk", strings.TrimSpace(string(key)))
	r.Header.Set("x-request-id", requestID)
	r.Header.Set("x-bf-cache-no-store", "true")
	r.Header.Set("x-bf-disable-content-logging", "true")
	response, err := client.Do(r)
	if err != nil {
		return Response{}, ErrUnavailable
	}
	defer response.Body.Close()
	b, err := io.ReadAll(response.Body)
	if err != nil {
		return Response{}, ErrUnavailable
	}
	if !json.Valid(b) {
		encoded, _ := json.Marshal(string(b))
		b = encoded
	}
	return Response{Status: response.StatusCode, Body: b, RequestID: response.Header.Get("x-bifrost-request-id"), TraceID: response.Header.Get("x-bifrost-trace-id")}, nil
}
