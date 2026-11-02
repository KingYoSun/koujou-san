package chatgptplan

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"time"
)

type Model struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"display_name"`
	Visibility  string `json:"visibility,omitempty"`
}

func (p Provider) Models(ctx context.Context, file string) ([]Model, error) {
	c, err := p.Access(ctx, file)
	if err != nil {
		return nil, err
	}
	r, _ := http.NewRequestWithContext(ctx, "GET", p.Resource+"/models", nil)
	r.Header.Set("Authorization", "Bearer "+c.AccessToken)
	response, err := p.client().Do(r)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer response.Body.Close()
	var result struct {
		Models []Model `json:"models"`
	}
	if response.StatusCode != 200 || json.NewDecoder(response.Body).Decode(&result) != nil {
		return nil, ErrUnavailable
	}
	models := []Model{}
	for _, model := range result.Models {
		if model.Visibility == "list" {
			models = append(models, model)
		}
	}
	return models, nil
}

// Command performs an explicit operator action, never a model inference.
func Command(ctx context.Context, command string, args []string, out io.Writer) error {
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	f.SetOutput(out)
	file := f.String("credentials-file", "", "absolute path to the server-specific private SIWC credential file")
	port := f.Int("callback-port", 14557, "loopback OAuth callback port; SSH forwarding may use the same port")
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if !filepath.IsAbs(*file) || f.NArg() != 0 {
		return errors.New("--credentials-file must be an absolute private file path")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	p := Default()
	switch command {
	case "model-login":
		if err := p.Login(ctx, *file, *port, func(address string) error {
			_, err := fmt.Fprintln(out, "Continue with ChatGPT:\n"+address)
			return err
		}); err != nil {
			return err
		}
		credential, err := Read(*file)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, "ChatGPTプランの専用認証を保存しました。アカウント: "+credential.Email)
		return err
	case "model-models":
		models, err := p.Models(ctx, *file)
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(models)
	case "model-logout":
		confirmed, err := p.Logout(ctx, *file)
		if err != nil {
			return err
		}
		if confirmed {
			_, err = fmt.Fprintln(out, "ChatGPTプラン接続を切断し、提供元の失効を確認しました。")
		} else {
			_, err = fmt.Fprintln(out, "端末の認証を削除しました。提供元の失効は未確認です。ChatGPT設定で接続を確認してください。")
		}
		return err
	}
	return errors.New("unknown model authentication command")
}
