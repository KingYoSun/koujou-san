// Package gitea owns the explicit instance and server-side credentials used by
// observation and issue publication. Business state remains in their services.
package gitea

import (
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
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type Instance struct {
	URL               string `json:"url"`
	TokenFile         string `json:"token_file"`
	CAFile            string `json:"ca_file,omitempty"`
	WebhookSecretFile string `json:"webhook_secret_file,omitempty"`
}

type Client struct {
	URL      string
	Secret   []byte
	resumeAt atomic.Int64
	token    string
	http     *http.Client
}

var ErrConfig = errors.New("Gitea configuration invalid")

func Origin(value string) (string, error) {
	u, err := url.Parse(value)
	if err != nil {
		return "", ErrConfig
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", ErrConfig
	}
	u.Host = strings.ToLower(u.Host)
	return strings.TrimRight(u.String(), "/"), nil
}

func SourceURL(instance, repository, source string) bool {
	base, err := url.Parse(instance)
	if err != nil {
		return false
	}
	target, err := url.Parse(source)
	return err == nil && target.User == nil && strings.EqualFold(target.Scheme, base.Scheme) && strings.EqualFold(target.Host, base.Host) && strings.HasPrefix(target.Path, strings.TrimRight(base.Path, "/")+"/"+repository+"/")
}

func (i Instance) Open() (*Client, error) {
	origin, err := Origin(i.URL)
	if err != nil {
		return nil, err
	}
	token, err := os.ReadFile(i.TokenFile)
	if err != nil || len(strings.TrimSpace(string(token))) == 0 {
		return nil, ErrConfig
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if i.CAFile != "" {
		cert, err := os.ReadFile(i.CAFile)
		if err != nil {
			return nil, ErrConfig
		}
		roots, _ := x509.SystemCertPool()
		if roots == nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(cert) {
			return nil, ErrConfig
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	}
	c := &Client{URL: origin, token: strings.TrimSpace(string(token)), http: &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	if i.WebhookSecretFile != "" {
		c.Secret, err = os.ReadFile(i.WebhookSecretFile)
		if err != nil || len(c.Secret) == 0 {
			return nil, ErrConfig
		}
	}
	return c, nil
}

func (c *Client) ResumeAt() time.Time { return time.Unix(0, c.resumeAt.Load()) }

func (c *Client) Request(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	if time.Now().Before(c.ResumeAt()) {
		return nil, errors.New("rate_limited")
	}
	r, err := http.NewRequestWithContext(ctx, method, c.URL+"/api/v1"+path, body)
	if err != nil {
		return nil, errors.New("invalid_gitea_request")
	}
	r.Header.Set("Authorization", "token "+c.token)
	r.Header.Set("Accept", "application/json")
	r.Header.Set("User-Agent", "koujou-san-gitea")
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(r)
	if err != nil {
		return nil, errors.New("connection_failed")
	}
	if seconds, err := strconv.Atoi(response.Header.Get("Retry-After")); err == nil && seconds > 0 {
		c.resumeAt.Store(time.Now().Add(time.Duration(seconds) * time.Second).UnixNano())
	}
	return response, nil
}

func (c *Client) Get(ctx context.Context, path string, into any) error {
	r, err := c.Request(ctx, "GET", path, nil)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		reason := "source_unavailable"
		switch r.StatusCode {
		case 401:
			reason = "authentication_failed"
		case 403, 404:
			reason = "not_accessible"
		case 429:
			reason = "rate_limited"
		}
		return errors.New(reason)
	}
	if json.NewDecoder(r.Body).Decode(into) != nil {
		return errors.New("invalid_source_response")
	}
	return nil
}

func (c *Client) Repository(ctx context.Context, repository string, expected int64) error {
	var repo struct {
		ID       int64  `json:"id"`
		FullName string `json:"full_name"`
	}
	if err := c.Get(ctx, "/repos/"+repository, &repo); err != nil {
		return err
	}
	if repo.ID != expected || repo.FullName != repository {
		return errors.New("repository_not_allowed")
	}
	return nil
}

func (c *Client) WebhookID() string {
	h := sha256.Sum256([]byte(c.URL))
	return hex.EncodeToString(h[:])
}

// Resource keeps every existing GitHub reference unchanged.
func Resource(id string) string {
	if strings.HasPrefix(id, "gitea:") {
		return id
	}
	return "github:" + id
}

func IsProvider(kind string) bool { return kind == "github" || kind == "gitea" }
