package distribution

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Edge keeps the Ory public routes and private administration on different listeners.
func Edge(web, site, api, hydra, kratos, hydraAdmin, kratosAdmin string) (http.Handler, http.Handler, error) {
	origin, err := url.Parse(site)
	if err != nil || origin.Scheme != "https" || origin.Host == "" {
		return nil, nil, errors.New("HTTPS site required")
	}
	proxy := func(target string, secure bool) (*httputil.ReverseProxy, error) {
		u, err := url.Parse(target)
		if err != nil || u.Host == "" {
			return nil, errors.New("invalid upstream")
		}
		p := &httputil.ReverseProxy{Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(u)
			r.SetXForwarded()
			r.Out.Host = origin.Host
			r.Out.Header.Set("X-Forwarded-Proto", "https")
			r.Out.Header.Set("X-Forwarded-Host", origin.Host)
		}}
		if secure {
			transport := http.DefaultTransport.(*http.Transport).Clone()
			transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: origin.Hostname()}
			p.Transport = transport
		}
		p.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) { http.Error(w, "upstream unavailable", 503) }
		return p, nil
	}
	back, err := proxy(api, true)
	if err != nil {
		return nil, nil, err
	}
	public := http.NewServeMux()
	private := http.NewServeMux()
	for _, route := range []struct {
		mux            *http.ServeMux
		prefix, target string
	}{
		{public, "/oidc/", hydra}, {public, "/auth/identity/", kratos},
		{private, "/hydra/", hydraAdmin}, {private, "/kratos/", kratosAdmin}, {private, "/identity/", kratos},
	} {
		p, e := proxy(route.target, false)
		if e != nil {
			return nil, nil, e
		}
		route.mux.Handle(route.prefix, http.StripPrefix(strings.TrimSuffix(route.prefix, "/"), p))
	}
	public.HandleFunc("GET /edge-healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Write([]byte("ok\n"))
	})
	files := http.FileServer(http.Dir(web))
	public.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		for _, prefix := range []string{"/admin/", "/hydra/", "/kratos/"} {
			if strings.HasPrefix(path, prefix) {
				http.NotFound(w, r)
				return
			}
		}
		isAPI := false
		for _, prefix := range []string{"/healthz", "/readyz", "/access/", "/contexts/", "/client-operations/", "/sync/", "/search/", "/graph/", "/github/", "/usage/", "/automation/", "/runs/", "/workflows/", "/hopper/", "/inspections/", "/publications/", "/workbench/", "/mcp", "/.well-known/oauth-protected-resource/"} {
			isAPI = isAPI || strings.HasPrefix(path, prefix)
		}
		for _, name := range []string{"config", "csrf", "start", "callback", "me", "logout", "broker/", "flow/", "mcp/"} {
			isAPI = isAPI || strings.HasPrefix(path, "/auth/"+name)
		}
		if isAPI {
			back.ServeHTTP(w, r)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			http.Error(w, "method not allowed", 405)
			return
		}
		relative := filepath.FromSlash(strings.TrimPrefix(path, "/"))
		if relative != "" && !filepath.IsLocal(relative) {
			http.NotFound(w, r)
			return
		}
		name := filepath.Join(web, relative)
		if info, e := os.Stat(name); e == nil && info.Mode().IsRegular() {
			files.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, r, filepath.Join(web, "index.html"))
	})
	return public, private, nil
}

func ServeEdge(ctx context.Context) error {
	site := os.Getenv("KOUJOU_SITE_URL")
	u, err := url.Parse(site)
	if err != nil {
		return err
	}
	if len(os.Args) == 3 && os.Args[1] == "check" {
		c := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname()}}}
		r, e := c.Get(os.Args[2])
		if e != nil {
			return errors.New("service unavailable")
		}
		r.Body.Close()
		if r.StatusCode != 200 {
			return errors.New("service unavailable")
		}
		return nil
	}
	pub, priv, err := Edge("/app/web", site, "https://127.0.0.1:8080", "http://hydra:4444", "http://kratos:4433", "http://hydra:4445", "http://kratos:4434")
	if err != nil {
		return err
	}
	port := u.Port()
	if port == "" {
		port = "443"
	}
	servers := []*http.Server{{Addr: ":" + port, Handler: pub, ReadHeaderTimeout: 10 * time.Second}, {Addr: "127.0.0.1:8081", Handler: priv, ReadHeaderTimeout: 10 * time.Second}}
	failed := make(chan error, 2)
	go func() { failed <- servers[0].ListenAndServeTLS("/config/cert.pem", "/config/key.pem") }()
	go func() { failed <- servers[1].ListenAndServe() }()
	select {
	case err = <-failed:
	case <-ctx.Done():
	}
	stop, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, s := range servers {
		_ = s.Shutdown(stop)
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
