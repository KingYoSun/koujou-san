package distribution

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Deployment struct {
	Root     string
	Settings Settings
}

func Open(root string) (*Deployment, error) {
	root, e := filepath.Abs(root)
	if e != nil {
		return nil, e
	}
	root, e = filepath.EvalSymlinks(root)
	if e != nil {
		return nil, e
	}
	s, e := loadSettings(root)
	if e != nil {
		return nil, e
	}
	return &Deployment{root, s}, nil
}
func (d *Deployment) Lock() (func(), error) {
	path := filepath.Join(d.Root, ".operation")
	if os.Mkdir(path, 0700) != nil {
		return nil, errors.New("deployment operation already active")
	}
	return func() { _ = os.Remove(path) }, nil
}
func docker(ctx context.Context, in io.Reader, out io.Writer, args ...string) error {
	c := exec.CommandContext(ctx, "docker", args...)
	c.Stdin = in
	c.Stdout = out
	// Do not echo service configuration or credentials from subprocess failures.
	if c.Run() != nil {
		return errors.New("Docker operation failed")
	}
	return nil
}
func (d *Deployment) compose(ctx context.Context, in io.Reader, out io.Writer, args ...string) error {
	a := []string{"compose", "--project-name", project(d.Root), "--project-directory", d.Root, "-f", filepath.Join(d.Root, "compose.json")}
	return docker(ctx, in, out, append(a, args...)...)
}
func (d *Deployment) container(ctx context.Context, name string) (string, error) {
	var b strings.Builder
	if e := d.compose(ctx, nil, &b, "ps", "-a", "-q", name); e != nil {
		return "", e
	}
	id := strings.TrimSpace(b.String())
	if id == "" || strings.Contains(id, "\n") {
		return "", errors.New("deployment container unavailable")
	}
	var label strings.Builder
	if e := docker(ctx, nil, &label, "inspect", "--format", `{{index .Config.Labels "com.docker.compose.project"}}`, id); e != nil || strings.TrimSpace(label.String()) != project(d.Root) {
		return "", errors.New("container ownership mismatch")
	}
	return id, nil
}
func (d *Deployment) ensureImage(ctx context.Context) error {
	r, e := LoadRelease(d.Settings.Bundle)
	if e != nil {
		return e
	}
	if r.ImageID != d.Settings.Release.ImageID {
		return errors.New("deployment release changed")
	}
	var id strings.Builder
	if e = docker(ctx, nil, &id, "image", "inspect", "--format", "{{.Id}}", r.Image); e != nil || strings.TrimSpace(id.String()) != r.ImageID {
		if e = docker(ctx, nil, io.Discard, "load", "-i", filepath.Join(d.Settings.Bundle, "server-image.tar")); e != nil {
			return e
		}
		id.Reset()
		if e = docker(ctx, nil, &id, "image", "inspect", "--format", "{{.Id}}", r.Image); e != nil || strings.TrimSpace(id.String()) != r.ImageID {
			return errors.New("loaded image does not match release")
		}
	}
	return nil
}
func (d *Deployment) startGateway(ctx context.Context) error {
	if e := d.compose(ctx, nil, io.Discard, "--profile", "llm", "up", "-d", "--wait", "bifrost"); e != nil {
		return e
	}
	for attempt := 0; attempt < 30; attempt++ {
		if e := d.compose(ctx, nil, io.Discard, "run", "--rm", "--no-deps", "--entrypoint", "/app/edge", "edge", "check", "http://bifrost:8080/health"); e == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return errors.New("Gateway readiness unavailable")
}
func (d *Deployment) Up(ctx context.Context) error {
	if e := d.ensureImage(ctx); e != nil {
		return e
	}
	if d.Settings.LLM {
		if e := d.startGateway(ctx); e != nil {
			return e
		}
	}
	return d.compose(ctx, nil, io.Discard, "up", "-d", "--wait", "--wait-timeout", "240")
}
func (d *Deployment) EnableGateway() error {
	d.Settings.LLM = true
	return jsonFile(filepath.Join(d.Root, "settings.json"), d.Settings)
}
func (d *Deployment) Stop(ctx context.Context) error {
	return d.compose(ctx, nil, io.Discard, "--profile", "llm", "stop")
}
func (d *Deployment) Status(ctx context.Context, out io.Writer) error {
	return d.compose(ctx, nil, out, "ps", "--format", "json")
}
func (d *Deployment) Bootstrap(ctx context.Context, email, password string) error {
	if email == "" || password == "" {
		return errors.New("administrator email and private password file required")
	}
	f, e := os.Open(password)
	if e != nil {
		return errors.New("password file unavailable")
	}
	defer f.Close()
	return d.compose(ctx, f, io.Discard, "exec", "-T", "api", "/app/auth-bootstrap", "--admin-email", email, "--password-stdin")
}
func (d *Deployment) running(ctx context.Context) ([]string, error) {
	var b strings.Builder
	if e := d.compose(ctx, nil, &b, "--profile", "llm", "ps", "--status", "running", "--services"); e != nil {
		return nil, e
	}
	return strings.Fields(b.String()), nil
}
func (d *Deployment) freeze(ctx context.Context) ([]string, error) {
	running, e := d.running(ctx)
	if e != nil {
		return nil, e
	}
	stopped := []string{}
	for _, name := range running {
		if name != "postgres" && name != "edge" {
			stopped = append(stopped, name)
		}
	}
	if len(stopped) > 0 {
		e = d.compose(ctx, nil, io.Discard, append([]string{"--profile", "llm", "stop"}, stopped...)...)
	}
	return stopped, e
}
func (d *Deployment) resume(ctx context.Context, names []string) error {
	if len(names) == 0 {
		return nil
	}
	for _, name := range names {
		if name == "bifrost" {
			if e := d.startGateway(ctx); e != nil {
				return e
			}
		}
	}
	return d.compose(ctx, nil, io.Discard, append([]string{"--profile", "llm", "up", "-d", "--wait", "--wait-timeout", "240"}, names...)...)
}

func (d *Deployment) snapshot(ctx context.Context, dest string) error {
	stage, e := os.MkdirTemp(d.Root, ".snapshot-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(stage)
	if _, e = d.container(ctx, "postgres"); e != nil {
		return e
	}
	if e = os.Mkdir(filepath.Join(stage, "databases"), 0700); e != nil {
		return e
	}
	for _, db := range []string{"koujou", "kratos", "hydra"} {
		file, e := os.OpenFile(filepath.Join(stage, "databases", db+".dump"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		e = d.compose(ctx, nil, file, "exec", "-T", "postgres", "pg_dump", "-U", "postgres", "-d", db, "-Fc")
		closeErr := file.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
	}
	output, e := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return errors.New("new backup filename required")
	}
	complete := false
	defer func() {
		output.Close()
		if !complete {
			_ = os.Remove(dest)
		}
	}()
	zip := gzip.NewWriter(output)
	archive := tar.NewWriter(zip)
	for _, entry := range []struct{ root, name string }{{d.Root, "settings.json"}, {d.Root, "config"}, {d.Root, "bifrost"}, {stage, "databases"}} {
		e = filepath.WalkDir(filepath.Join(entry.root, entry.name), func(path string, item os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			info, e := item.Info()
			if e != nil {
				return e
			}
			if !info.IsDir() && !info.Mode().IsRegular() {
				return errors.New("backup sources must be regular files")
			}
			h, e := tar.FileInfoHeader(info, "")
			if e != nil {
				return e
			}
			h.Name, _ = filepath.Rel(entry.root, path)
			h.Name = filepath.ToSlash(h.Name)
			if info.IsDir() {
				h.Mode = 0700
			} else {
				h.Mode = 0600
			}
			if e = archive.WriteHeader(h); e != nil {
				return e
			}
			if info.IsDir() {
				return nil
			}
			f, e := os.Open(path)
			if e != nil {
				return e
			}
			_, e = io.Copy(archive, f)
			f.Close()
			return e
		})
		if e != nil {
			return e
		}
	}
	if e = archive.Close(); e != nil {
		return e
	}
	if e = zip.Close(); e != nil {
		return e
	}
	if e = output.Sync(); e != nil {
		return e
	}
	complete = true
	return nil
}
func (d *Deployment) Backup(ctx context.Context, dest string) error {
	stopped, e := d.freeze(ctx)
	if e != nil {
		return e
	}
	e = d.snapshot(ctx, dest)
	resumed := d.resume(context.WithoutCancel(ctx), stopped)
	if e != nil {
		return e
	}
	return resumed
}
func unpack(backup, dest string) error {
	f, e := os.Open(backup)
	if e != nil {
		return errors.New("backup unavailable")
	}
	defer f.Close()
	zip, e := gzip.NewReader(f)
	if e != nil {
		return errors.New("invalid backup")
	}
	defer zip.Close()
	archive := tar.NewReader(zip)
	for {
		h, e := archive.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		if !filepath.IsLocal(h.Name) || h.Typeflag != tar.TypeDir && h.Typeflag != tar.TypeReg {
			return errors.New("invalid backup member")
		}
		name := filepath.Join(dest, filepath.FromSlash(h.Name))
		if h.Typeflag == tar.TypeDir {
			if e = os.MkdirAll(name, 0700); e != nil {
				return e
			}
			continue
		}
		if e = os.MkdirAll(filepath.Dir(name), 0700); e != nil {
			return e
		}
		out, e := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return errors.New("duplicate backup member")
		}
		_, e = io.Copy(out, archive)
		out.Close()
		if e != nil {
			return e
		}
	}
	_, e = io.Copy(io.Discard, zip)
	return e
}
func (d *Deployment) restoreDatabases(ctx context.Context, source string) error {
	if e := d.compose(ctx, nil, io.Discard, "up", "-d", "--wait", "postgres"); e != nil {
		return e
	}
	if _, e := d.container(ctx, "postgres"); e != nil {
		return e
	}
	for _, db := range []string{"koujou", "kratos", "hydra"} {
		file, e := os.Open(filepath.Join(source, "databases", db+".dump"))
		if e != nil {
			return e
		}
		if e = d.compose(ctx, nil, io.Discard, "exec", "-T", "postgres", "dropdb", "-U", "postgres", "--force", db); e != nil {
			file.Close()
			return e
		}
		if e = d.compose(ctx, nil, io.Discard, "exec", "-T", "postgres", "createdb", "-U", "postgres", "-O", db, db); e != nil {
			file.Close()
			return e
		}
		e = d.compose(ctx, file, io.Discard, "exec", "-T", "postgres", "pg_restore", "-U", "postgres", "--single-transaction", "--exit-on-error", "-d", db)
		file.Close()
		if e != nil {
			return e
		}
	}
	return nil
}
func Restore(ctx context.Context, bundle, root, backup string) error {
	r, e := LoadRelease(bundle)
	if e != nil {
		return e
	}
	if _, e = os.Stat(root); !os.IsNotExist(e) {
		return errors.New("new restore directory required")
	}
	if e = os.MkdirAll(filepath.Dir(root), 0700); e != nil {
		return e
	}
	if e = os.Mkdir(root, 0700); e != nil {
		return e
	}
	if e = unpack(backup, root); e != nil {
		return e
	}
	s, e := loadSettings(root)
	if e != nil {
		return e
	}
	if r.ImageID != s.Release.ImageID {
		return errors.New("backup requires its original release bundle")
	}
	s.Bundle = bundle
	s.UID = os.Getuid()
	s.GID = os.Getgid()
	if e = jsonFile(filepath.Join(root, "settings.json"), s); e != nil {
		return e
	}
	if e = Render(root, s); e != nil {
		return e
	}
	d := &Deployment{root, s}
	if e = d.ensureImage(ctx); e != nil {
		return e
	}
	if e = d.restoreDatabases(ctx, root); e != nil {
		return e
	}
	// Restore dumps have served their purpose; remove only these own copies.
	return os.RemoveAll(filepath.Join(root, "databases"))
}
func (d *Deployment) Update(ctx context.Context, bundle string) error {
	r, e := LoadRelease(bundle)
	if e != nil {
		return e
	}
	if r.Schema < d.Settings.Release.Schema {
		return errors.New("schema downgrade requires restore with the original bundle")
	}
	stopped, e := d.freeze(ctx)
	if e != nil {
		return e
	}
	backup := filepath.Join(d.Root, "backup-before-update-"+time.Now().UTC().Format("20060102T150405.000000000")+".tgz")
	if e = d.snapshot(ctx, backup); e != nil {
		_ = d.resume(context.WithoutCancel(ctx), stopped)
		return e
	}
	original := d.Settings
	d.Settings.Release = r
	d.Settings.Bundle = bundle
	if e = Render(d.Root, d.Settings); e == nil {
		e = d.ensureImage(ctx)
	}
	if e == nil {
		e = d.compose(ctx, nil, io.Discard, "run", "--rm", "--no-deps", "api", "migrate")
	}
	if e != nil {
		stage, extractErr := os.MkdirTemp(d.Root, ".rollback-")
		if extractErr != nil {
			return extractErr
		}
		defer os.RemoveAll(stage)
		if extractErr = unpack(backup, stage); extractErr != nil {
			return extractErr
		}
		d.Settings = original
		if rollbackErr := Render(d.Root, original); rollbackErr != nil {
			return rollbackErr
		}
		if rollbackErr := d.restoreDatabases(context.WithoutCancel(ctx), stage); rollbackErr != nil {
			return errors.New("update failed; database recovery requires operator action with the retained backup")
		}
		if rollbackErr := d.resume(context.WithoutCancel(ctx), stopped); rollbackErr != nil {
			return errors.New("original data restored; service restart requires operator action")
		}
		return errors.New("update failed; original release and data restored; backup retained")
	}
	if e = jsonFile(filepath.Join(d.Root, "settings.json"), d.Settings); e != nil {
		return e
	}
	// Once the new application starts, do not silently roll back its external effects.
	if e = d.Up(ctx); e != nil {
		return errors.New("migration completed; new release startup failed; data and pre-update backup retained")
	}
	return nil
}

func PrintStatus(out io.Writer, s Settings) error {
	return json.NewEncoder(out).Encode(object{"version": s.Release.Version, "commit": s.Release.Commit, "schema": s.Release.Schema, "site": s.Site})
}

func Absolute(value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("path required")
	}
	return filepath.Abs(value)
}
