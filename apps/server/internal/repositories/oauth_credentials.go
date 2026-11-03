package repositories

import (
	"context"
	"net/url"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/gitea"
	"github.com/jackc/pgx/v5"
)

type oauthCredential struct {
	App     string    `json:"app"`
	Refresh string    `json:"refresh"`
	Expires time.Time `json:"expires"`
}

func (s *Service) oauthClient(ctx context.Context, w, id string, c Connection, saved credential) (*gitea.Client, error) {
	if saved.OAuth == nil {
		return gitea.FromCredentials(c.Instance, saved.Token, saved.CA)
	}
	if saved.OAuth.Expires.After(time.Now().Add(30 * time.Second)) {
		return gitea.FromOAuthCredentials(c.Instance, saved.Token, saved.CA)
	}
	err := pgx.BeginFunc(ctx, s.Store.Database(), func(tx pgx.Tx) error {
		var encrypted []byte
		if err := tx.QueryRow(ctx, `SELECT credential FROM forge_connections WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, w, id).Scan(&encrypted); err != nil {
			return err
		}
		var err error
		saved, err = s.open(w, id, encrypted)
		if err != nil {
			return err
		}
		if saved.OAuth == nil {
			return ErrState
		}
		if saved.OAuth.Expires.After(time.Now().Add(30 * time.Second)) {
			return nil
		}
		app, secret, err := s.oauthApp(ctx, saved.OAuth.App, tx)
		if err != nil {
			return err
		}
		if app.Instance != c.Instance || saved.OAuth.Refresh == "" {
			return ErrState
		}
		refreshed, err := gitea.Exchange(ctx, app.Instance, secret.CA, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {saved.OAuth.Refresh}, "client_id": {app.ClientID}, "client_secret": {secret.Secret}})
		if err != nil {
			return err
		}
		if refreshed.Refresh == "" {
			return ErrState
		}
		saved.Token = refreshed.Access
		saved.OAuth.Refresh = refreshed.Refresh
		saved.OAuth.Expires = time.Now().Add(time.Duration(refreshed.Expires) * time.Second)
		encrypted, err = s.seal(w, id, saved)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE forge_connections SET credential=$3 WHERE workspace_id=$1 AND id=$2`, w, id, encrypted)
		return err
	})
	if err != nil {
		return nil, err
	}
	return gitea.FromOAuthCredentials(c.Instance, saved.Token, saved.CA)
}
