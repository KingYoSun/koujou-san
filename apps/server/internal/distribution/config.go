package distribution

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

//go:embed components.json
var componentJSON []byte

type Component struct {
	Image      string `json:"image"`
	License    string `json:"license"`
	Source     string `json:"source"`
	LicenseURL string `json:"license_url"`
}

type Release struct {
	Version string            `json:"version"`
	Commit  string            `json:"commit"`
	Image   string            `json:"image"`
	ImageID string            `json:"image_id"`
	Schema  int               `json:"schema"`
	Files   map[string]string `json:"files"`
}
type Settings struct {
	LLM     bool              `json:"llm_enabled"`
	Site    string            `json:"site"`
	SMTP    string            `json:"smtp"`
	Secrets map[string]string `json:"secrets"`
	UID     int               `json:"uid"`
	GID     int               `json:"gid"`
	Release Release           `json:"release"`
	Bundle  string            `json:"bundle"`
}
type object = map[string]any

func jsonFile(name string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(name, append(b, '\n'), 0600)
}
func LoadRelease(root string) (Release, error) {
	var r Release
	b, err := os.ReadFile(filepath.Join(root, "release.json"))
	if err != nil {
		return r, errors.New("release manifest unavailable")
	}
	if json.Unmarshal(b, &r) != nil || r.Version == "" || r.Commit == "" || r.Image == "" || r.ImageID == "" || r.Schema <= 0 || len(r.Files) == 0 {
		return r, errors.New("invalid release manifest")
	}
	for name, want := range r.Files {
		if !filepath.IsLocal(name) {
			return r, errors.New("invalid release path")
		}
		f, e := os.Open(filepath.Join(root, name))
		if e != nil {
			return r, e
		}
		h := sha256.New()
		_, e = io.Copy(h, f)
		f.Close()
		if e != nil || hex.EncodeToString(h.Sum(nil)) != want {
			return r, errors.New("release checksum mismatch")
		}
	}
	return r, nil
}
func loadSettings(root string) (Settings, error) {
	var s Settings
	b, e := os.ReadFile(filepath.Join(root, "settings.json"))
	if e != nil {
		return s, errors.New("deployment settings unavailable")
	}
	if json.Unmarshal(b, &s) != nil || s.Site == "" || s.Secrets["auth"] == "" {
		return s, errors.New("invalid deployment settings")
	}
	return s, nil
}
func project(root string) string {
	h := sha256.Sum256([]byte(root))
	return "koujou-" + hex.EncodeToString(h[:6])
}

func Init(bundle, root, site, smtpFile, cert, key, ca string) error {
	r, err := LoadRelease(bundle)
	if err != nil {
		return err
	}
	u, err := url.Parse(site)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return errors.New("HTTPS origin required")
	}
	smtp, err := os.ReadFile(smtpFile)
	if err != nil {
		return errors.New("SMTP file unavailable")
	}
	mail, err := url.Parse(strings.TrimSpace(string(smtp)))
	if err != nil || (mail.Scheme != "smtp" && mail.Scheme != "smtps") || mail.Host == "" {
		return errors.New("SMTP URI required")
	}
	crt, err := os.ReadFile(cert)
	if err != nil {
		return errors.New("certificate unavailable")
	}
	pk, err := os.ReadFile(key)
	if err != nil {
		return errors.New("certificate key unavailable")
	}
	pair, err := tls.X509KeyPair(crt, pk)
	if err != nil {
		return errors.New("invalid TLS key pair")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil || leaf.VerifyHostname(u.Hostname()) != nil {
		return errors.New("TLS certificate does not match the site")
	}
	trust, err := os.ReadFile(ca)
	if err != nil {
		return errors.New("CA bundle unavailable")
	}
	if _, err = os.Stat(root); !os.IsNotExist(err) {
		return errors.New("new deployment directory required")
	}
	s := Settings{Site: strings.TrimRight(site, "/"), SMTP: mail.String(), Secrets: map[string]string{}, UID: os.Getuid(), GID: os.Getgid(), Release: r, Bundle: bundle}
	for _, name := range []string{"postgres", "koujou", "kratos", "hydra", "auth", "web", "cookie", "cipher", "hydra-system", "arcade", "gateway-admin", "gateway-worker", "gateway-encryption"} {
		b := make([]byte, 24)
		if _, err = rand.Read(b); err != nil {
			return err
		}
		s.Secrets[name] = base64.RawURLEncoding.EncodeToString(b)
	}
	if err = os.MkdirAll(filepath.Dir(root), 0700); err != nil {
		return err
	}
	if err = os.Mkdir(root, 0700); err != nil {
		return errors.New("new deployment directory required")
	}
	if err = os.MkdirAll(filepath.Join(root, "config/connectors"), 0700); err != nil {
		return err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	s.Bundle, err = filepath.EvalSymlinks(bundle)
	if err != nil {
		return err
	}
	for name, data := range map[string][]byte{"cert.pem": crt, "key.pem": pk, "ca.pem": trust} {
		if err = os.WriteFile(filepath.Join(root, "config", name), data, 0600); err != nil {
			return err
		}
	}
	for name, data := range map[string]string{"server-models.json": "[]\n", "gateway-worker": s.Secrets["gateway-worker"], "github-observer.json": "", "github-publisher.json": ""} {
		if err = os.WriteFile(filepath.Join(root, "config/connectors", name), []byte(data), 0600); err != nil {
			return err
		}
	}
	if err = jsonFile(filepath.Join(root, "settings.json"), s); err != nil {
		return err
	}
	return Render(root, s)
}

func Render(root string, s Settings) error {
	var components map[string]Component
	if json.Unmarshal(componentJSON, &components) != nil {
		return errors.New("invalid component manifest")
	}
	for _, dir := range []string{"config/connectors", "search", "postgres", "tmp", "bifrost"} {
		if e := os.MkdirAll(filepath.Join(root, dir), 0700); e != nil {
			return e
		}
	}
	u, err := url.Parse(s.Site)
	if err != nil {
		return err
	}
	port := u.Port()
	if port == "" {
		port = "443"
	}
	if n, e := strconv.Atoi(port); e != nil || n < 1 || n > 65535 {
		return errors.New("invalid HTTPS port")
	}
	config := filepath.Join(root, "config")
	for _, name := range []string{"identity.schema.json", "postgres-init.sh"} {
		b, e := os.ReadFile(filepath.Join(s.Bundle, "templates", name))
		if e != nil {
			return e
		}
		mode := os.FileMode(0600)
		if name == "postgres-init.sh" {
			mode = 0644
		}
		if e = os.WriteFile(filepath.Join(config, name), b, mode); e != nil {
			return e
		}
	}
	if err = os.Chmod(filepath.Join(config, "postgres-init.sh"), 0644); err != nil {
		return err
	}
	flows := object{}
	for _, kind := range []string{"login", "registration", "settings", "recovery", "verification", "error"} {
		flows[kind] = object{"ui_url": s.Site + "/auth/" + kind}
	}
	flows["login"].(object)["after"] = object{"password": object{"hooks": []object{{"hook": "require_verified_address"}}}}
	flows["settings"].(object)["required_aal"] = "highest_available"
	for _, kind := range []string{"recovery", "verification"} {
		flows[kind].(object)["enabled"] = true
		flows[kind].(object)["use"] = "code"
	}
	flows["logout"] = object{"after": object{"default_browser_return_url": s.Site + "/"}}
	kratos := object{"version": "v0.13.0", "serve": object{"public": object{"base_url": s.Site + "/auth/identity/"}, "admin": object{"base_url": "http://kratos:4434/"}, "tls": object{"allow_termination_from": []string{"0.0.0.0/0"}}}, "selfservice": object{"default_browser_return_url": s.Site + "/", "allowed_return_urls": []string{s.Site + "/"}, "methods": object{"password": object{"enabled": true}, "totp": object{"enabled": true, "config": object{"issuer": "koujou-san"}}, "code": object{"enabled": true, "config": object{"passwordless_enabled": false}}}, "flows": flows}, "session": object{"lifespan": "168h", "whoami": object{"required_aal": "highest_available"}}, "courier": object{"smtp": object{"connection_uri": s.SMTP}}, "identity": object{"default_schema_id": "default", "schemas": []object{{"id": "default", "url": "file:///config/identity.schema.json"}}}, "log": object{"level": "warn", "format": "json", "leak_sensitive_values": false}, "secrets": object{"cookie": []string{s.Secrets["cookie"]}, "cipher": []string{s.Secrets["cipher"]}}, "ciphers": object{"algorithm": "xchacha20-poly1305"}}
	hydra := object{"serve": object{"cookies": object{"same_site_mode": "Lax"}, "tls": object{"allow_termination_from": []string{"0.0.0.0/0"}}}, "urls": object{"self": object{"issuer": s.Site + "/oidc/"}, "login": s.Site + "/auth/login", "consent": s.Site + "/auth/consent", "logout": s.Site + "/auth/logout"}, "ttl": object{"access_token": "15m", "refresh_token": "168h", "login_consent_request": "15m"}, "oauth2": object{"pkce": object{"enforced": true}}, "oidc": object{"subject_identifiers": object{"supported_types": []string{"public"}}}, "secrets": object{"system": []string{s.Secrets["hydra-system"]}}, "log": object{"level": "warn", "format": "json", "leak_sensitive_values": false}}
	for name, value := range map[string]any{"kratos.json": kratos, "hydra.json": hydra} {
		if err = jsonFile(filepath.Join(config, name), value); err != nil {
			return err
		}
	}
	bf, err := os.ReadFile(filepath.Join(s.Bundle, "templates/bifrost.json"))
	if err != nil {
		return err
	}
	if _, err = os.Stat(filepath.Join(root, "bifrost/config.json")); os.IsNotExist(err) {
		if err = os.WriteFile(filepath.Join(root, "bifrost/config.json"), bf, 0600); err != nil {
			return err
		}
	}
	caMount := config + ":/config:ro"
	probe := func(command ...string) object {
		return object{"test": append([]string{"CMD"}, command...), "interval": "5s", "timeout": "5s", "retries": 36, "start_period": "10s"}
	}
	dep := func(condition string) object { return object{"condition": condition} }
	oryEnv := func(db string) object {
		return object{"DSN": "postgres://" + db + ":" + s.Secrets[db] + "@postgres:5432/" + db + "?sslmode=disable", "SSL_CERT_FILE": "/config/ca.pem"}
	}
	apiEnv := object{"KOUJOU_DATABASE_URL": "postgres://koujou:" + s.Secrets["koujou"] + "@postgres:5432/koujou?sslmode=disable", "KOUJOU_AUTH_ENABLED": "1", "KOUJOU_API_ADDR": "127.0.0.1:8080", "KOUJOU_SITE_URL": s.Site, "KOUJOU_PUBLIC_API_URL": s.Site, "KOUJOU_OIDC_ISSUER": s.Site + "/oidc/", "KOUJOU_HYDRA_ADMIN_URL": "http://127.0.0.1:8081/hydra", "KOUJOU_KRATOS_PUBLIC_URL": "http://127.0.0.1:8081/identity", "KOUJOU_KRATOS_ADMIN_URL": "http://127.0.0.1:8081/kratos", "KOUJOU_WEB_CLIENT_ID": "koujou-web", "KOUJOU_WEB_CLIENT_SECRET": s.Secrets["web"], "KOUJOU_AUTH_KEY": s.Secrets["auth"], "KOUJOU_TLS_CERT": "/config/cert.pem", "KOUJOU_TLS_KEY": "/config/key.pem", "SSL_CERT_FILE": "/config/ca.pem", "KOUJOU_SEARCH_DIR": "/data/search", "TMPDIR": "/data/tmp", "KOUJOU_GRAPH_URL": "http://arcadedb:2480", "KOUJOU_GRAPH_USER": "root", "KOUJOU_GRAPH_PASSWORD": s.Secrets["arcade"], "KOUJOU_GRAPH_DATABASE": "koujou", "KOUJOU_SERVER_LLM_CONFIG": "/config/connectors/server-models.json"}
	uid := fmt.Sprintf("%d:%d", s.UID, s.GID)
	edgeNetwork := object{}
	if net.ParseIP(u.Hostname()) == nil && u.Hostname() != "localhost" {
		edgeNetwork["aliases"] = []string{u.Hostname()}
	}
	services := object{
		"postgres": object{"image": components["postgres"].Image, "environment": object{"POSTGRES_USER": "postgres", "POSTGRES_DB": "postgres", "POSTGRES_PASSWORD": s.Secrets["postgres"], "KOUJOU_DB_PASSWORD": s.Secrets["koujou"], "KRATOS_DB_PASSWORD": s.Secrets["kratos"], "HYDRA_DB_PASSWORD": s.Secrets["hydra"]}, "volumes": []string{filepath.Join(root, "postgres") + ":/var/lib/postgresql/data", filepath.Join(config, "postgres-init.sh") + ":/docker-entrypoint-initdb.d/10-koujou.sh:ro"}, "healthcheck": probe("pg_isready", "-U", "postgres"), "restart": "unless-stopped"},
		"arcadedb": object{"image": components["arcadedb"].Image, "environment": object{"JAVA_OPTS": "-Darcadedb.server.rootPassword=" + s.Secrets["arcade"] + " -Darcadedb.server.defaultDatabases=koujou[root] -Darcadedb.maxPageRAM=16 -Darcadedb.txWalFlush=1", "ARCADEDB_JMX": ""}, "volumes": []string{"arcade-config:/home/arcadedb/config", "arcade-data:/home/arcadedb/databases", "arcade-backups:/home/arcadedb/backups", "arcade-log:/home/arcadedb/log"}, "healthcheck": probe("wget", "-qO-", "http://127.0.0.1:2480/api/v1/ready"), "mem_limit": "1g", "restart": "unless-stopped"},
		"edge":     object{"image": s.Release.Image, "entrypoint": []string{"/app/edge"}, "environment": object{"KOUJOU_SITE_URL": s.Site, "SSL_CERT_FILE": "/config/ca.pem"}, "user": uid, "ports": []string{port + ":" + port}, "volumes": []string{caMount}, "networks": object{"default": edgeNetwork}, "healthcheck": probe("/app/edge", "check", s.Site+"/edge-healthz"), "restart": "unless-stopped"},
		"api":      object{"image": s.Release.Image, "network_mode": "service:edge", "entrypoint": []string{"/app/api"}, "environment": apiEnv, "user": uid, "volumes": []string{caMount, filepath.Join(root, "search") + ":/data/search", filepath.Join(root, "tmp") + ":/data/tmp"}, "depends_on": object{"postgres": dep("service_healthy"), "arcadedb": dep("service_healthy"), "kratos": dep("service_healthy"), "hydra": dep("service_healthy"), "edge": dep("service_healthy")}, "healthcheck": probe("/app/edge", "check", "https://127.0.0.1:8080/readyz"), "restart": "unless-stopped"},
		"bifrost":  object{"profiles": []string{"llm"}, "env_file": []object{{"path": filepath.Join(config, "connectors/provider.env"), "required": false}}, "image": components["bifrost"].Image, "user": uid, "environment": object{"BIFROST_ADMIN_PASSWORD": s.Secrets["gateway-admin"], "BIFROST_WORKER_KEY": s.Secrets["gateway-worker"], "BIFROST_ENCRYPTION_KEY": s.Secrets["gateway-encryption"]}, "volumes": []string{filepath.Join(root, "bifrost") + ":/app/data"}, "restart": "unless-stopped"},
	}
	for _, name := range []string{"kratos", "hydra"} {
		image := components[name].Image
		cfg := "/config/" + name + ".json"
		services[name+"-migrate"] = object{"image": image, "user": uid, "command": []string{"migrate", "sql", "-e", "--yes", "--config", cfg}, "environment": oryEnv(name), "volumes": []string{caMount}, "depends_on": object{"postgres": dep("service_healthy")}, "restart": "no"}
		command := []string{"serve", "all", "--config", cfg}
		adminPort := "4445"
		if name == "kratos" {
			command = []string{"serve", "--watch-courier", "--config", cfg}
			adminPort = "4434"
		}
		services[name] = object{"image": image, "user": uid, "command": command, "environment": oryEnv(name), "volumes": []string{caMount}, "depends_on": object{name + "-migrate": dep("service_completed_successfully")}, "healthcheck": probe(name, "remote", "status", "--endpoint", "http://127.0.0.1:"+adminPort), "restart": "unless-stopped"}
	}
	// Compose interpolates dollar signs even in JSON. Values are data, not expressions.
	encoded, err := json.Marshal(object{"name": project(root), "services": services, "volumes": object{"arcade-config": object{}, "arcade-data": object{}, "arcade-backups": object{}, "arcade-log": object{}}})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "compose.json"), []byte(strings.ReplaceAll(string(encoded), "$", "$$")), 0600)
}
