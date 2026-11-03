package repositories

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/gitea"
	"github.com/jackc/pgx/v5"
)

type OAuthApp struct {
	Callback string `json:"callback_url"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	Instance string `json:"instance"`
	ClientID string `json:"client_id"`
	Version  string `json:"version"`
}
type oauthAppSecret struct {
	Secret string `json:"secret"`
	CA     string `json:"ca"`
}
type OAuthAppWrite struct {
	Name            string `json:"name"`
	Instance        string `json:"instance"`
	ClientID        string `json:"client_id"`
	Secret          string `json:"client_secret"`
	CA              string `json:"ca"`
	ExpectedVersion string `json:"expected_version"`
}

func (s *Service) serverAdmin(ctx context.Context, p access.Principal) error {
	if p.DeviceID != "" || p.OperationID != "" {
		return access.ErrForbidden
	}
	var allowed bool
	if err := s.Store.Database().QueryRow(ctx, `SELECT administrator FROM auth_users WHERE id=$1`, p.UserID).Scan(&allowed); err != nil || !allowed {
		return access.ErrForbidden
	}
	return nil
}
func (s *Service) oauthApp(ctx context.Context, id string, within ...pgx.Tx) (OAuthApp, oauthAppSecret, error) {
	var app OAuthApp
	var secret oauthAppSecret
	var spec, encrypted []byte
	var version int64
	var q interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	} = s.Store.Database()
	if len(within) > 0 {
		q = within[0]
	}
	err := q.QueryRow(ctx, `SELECT spec,secret,version FROM forge_oauth_apps WHERE id=$1`, id).Scan(&spec, &encrypted, &version)
	if err != nil {
		return app, secret, err
	}
	if json.Unmarshal(spec, &app) != nil {
		return app, secret, ErrInvalid
	}
	app.Version = strconv.FormatInt(version, 10)
	err = s.openValue("", "oauth-app:"+id, encrypted, &secret)
	return app, secret, err
}
func (s *Service) OAuthApps(ctx context.Context, p access.Principal, w, after string) (Page[OAuthApp], error) {
	out := Page[OAuthApp]{Items: []OAuthApp{}}
	if w == "" {
		if err := s.serverAdmin(ctx, p); err != nil {
			return out, err
		}
	} else if err := s.Access.Check(ctx, p, w, "", "manage"); err != nil {
		return out, err
	}
	rows, err := s.Store.Database().Query(ctx, `SELECT spec,version FROM forge_oauth_apps WHERE id>$1 ORDER BY id LIMIT 51`, after)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var app OAuthApp
		var spec []byte
		var version int64
		if err = rows.Scan(&spec, &version); err != nil {
			return out, err
		}
		if json.Unmarshal(spec, &app) != nil {
			return out, ErrInvalid
		}
		app.Version = strconv.FormatInt(version, 10)
		app.Callback, _ = s.callbackURL()
		out.Items = append(out.Items, app)
	}
	if len(out.Items) > 50 {
		out.Items = out.Items[:50]
		out.Next = out.Items[49].ID
	}
	return out, rows.Err()
}
func (s *Service) PutOAuthApp(ctx context.Context, p access.Principal, id string, req OAuthAppWrite) (OAuthApp, error) {
	var out OAuthApp
	if err := s.serverAdmin(ctx, p); err != nil {
		return out, err
	}
	origin, err := gitea.Origin(req.Instance)
	if err != nil || id == "" || strings.TrimSpace(req.Name) == "" || req.ClientID == "" {
		return out, ErrInvalid
	}
	u, _ := url.Parse(origin)
	if u.Scheme != "https" && u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" {
		return out, ErrInvalid
	}
	version, err := strconv.ParseInt(req.ExpectedVersion, 10, 64)
	if err != nil || version < 0 {
		return out, ErrInvalid
	}
	secret := oauthAppSecret{req.Secret, req.CA}
	if version > 0 {
		old, saved, err := s.oauthApp(ctx, id)
		if err != nil {
			return out, err
		}
		if old.Instance != origin || old.ClientID != req.ClientID {
			return out, ErrState
		}
		if secret.Secret == "" {
			secret.Secret = saved.Secret
		}
		if secret.CA == "" {
			secret.CA = saved.CA
		}
	}
	if secret.Secret == "" {
		return out, ErrInvalid
	}
	if _, err = gitea.FromCredentials(origin, secret.Secret, secret.CA); err != nil {
		return out, ErrInvalid
	}
	encrypted, err := s.seal("", "oauth-app:"+id, secret)
	if err != nil {
		return out, err
	}
	out = OAuthApp{ID: id, Name: req.Name, Instance: origin, ClientID: req.ClientID, Version: strconv.FormatInt(version+1, 10)}
	err = pgx.BeginFunc(ctx, s.Store.Database(), func(tx pgx.Tx) error {
		var admin bool
		if tx.QueryRow(ctx, `SELECT administrator FROM auth_users WHERE id=$1 FOR SHARE`, p.UserID).Scan(&admin) != nil || !admin {
			return access.ErrForbidden
		}
		tag, err := tx.Exec(ctx, `INSERT INTO forge_oauth_apps SELECT $1,1,$2,$3 WHERE $4::bigint=0 OR EXISTS(SELECT 1 FROM forge_oauth_apps WHERE id=$1) ON CONFLICT(id) DO UPDATE SET version=forge_oauth_apps.version+1,spec=excluded.spec,secret=excluded.secret WHERE forge_oauth_apps.version=$4`, id, raw(out), encrypted, version)
		if err == nil && tag.RowsAffected() != 1 {
			return ErrState
		}
		return err
	})
	return out, err
}
