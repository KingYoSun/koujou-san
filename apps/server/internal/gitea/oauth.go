package gitea

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type OAuthToken struct {
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token"`
	Type    string `json:"token_type"`
	Expires int64  `json:"expires_in"`
}

// Exchange sends secrets only to the explicitly registered instance and never
// follows redirects. Credentials and endpoint error bodies are not returned.
func Exchange(ctx context.Context, instance, certificate string, form url.Values) (OAuthToken, error) {
	var out OAuthToken
	c, err := newClient(instance, "", certificate)
	if err != nil {
		return out, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.URL+"/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return out, ErrConfig
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return out, ErrConfig
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&out) != nil || out.Access == "" || out.Expires <= 0 || !strings.EqualFold(out.Type, "bearer") {
		return OAuthToken{}, ErrConfig
	}
	return out, nil
}
