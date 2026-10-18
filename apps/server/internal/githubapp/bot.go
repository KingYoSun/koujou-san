package githubapp

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strconv"
)

// Bot resolves a numeric actor from the authenticated App, never an event's name.
func Bot(ctx context.Context, appID string, key *rsa.PrivateKey, installation string, request Request) (int64, error) {
	identity, err := jwt(appID, key)
	if err != nil {
		return 0, err
	}
	read := func(path, token string, value any) error {
		response, err := request(ctx, "GET", path, token, nil)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode != 200 || json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(value) != nil {
			return errors.New("app_identity_unavailable")
		}
		return nil
	}
	var app struct {
		ID   int64
		Slug string
	}
	if err = read("/app", identity, &app); err != nil || strconv.FormatInt(app.ID, 10) != appID || app.Slug == "" {
		return 0, errors.New("app_identity_unavailable")
	}
	var actor struct {
		ID          int64
		Login, Type string
	}
	login := app.Slug + "[bot]"
	if err = read("/users/"+url.PathEscape(login), installation, &actor); err != nil || actor.ID <= 0 || actor.Type != "Bot" || actor.Login != login {
		return 0, errors.New("app_bot_unavailable")
	}
	return actor.ID, nil
}
