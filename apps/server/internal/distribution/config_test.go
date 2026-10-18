package distribution

import (
	"archive/tar"
	"compress/gzip"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPrivateInitializationAndVerifiedRelease(t *testing.T) {
	base := t.TempDir()
	bundle := filepath.Join(base, "bundle")
	os.MkdirAll(filepath.Join(bundle, "templates"), 0700)
	for name, data := range map[string]string{"identity.schema.json": "{}", "postgres-init.sh": "#!/bin/sh\n", "bifrost.json": "{}"} {
		os.WriteFile(filepath.Join(bundle, "templates", name), []byte(data), 0600)
	}
	os.WriteFile(filepath.Join(bundle, "server-image.tar"), []byte("owned-image"), 0600)
	digest := sha256.Sum256([]byte("owned-image"))
	r := Release{Version: "0.1.0-preview.1", Commit: "owned", Image: "owned:one", ImageID: "sha256:owned", Schema: 23, Files: map[string]string{"server-image.tar": hex.EncodeToString(digest[:])}}
	jsonFile(filepath.Join(bundle, "release.json"), r)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	der, _ := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	pk, _ := x509.MarshalPKCS8PrivateKey(key)
	certFile := filepath.Join(base, "cert.pem")
	keyFile := filepath.Join(base, "key.pem")
	smtpFile := filepath.Join(base, "smtp")
	os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600)
	os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pk}), 0600)
	os.WriteFile(smtpFile, []byte("smtp://owned:password$$@mail.test:1025/"), 0600)
	root := filepath.Join(base, "state")
	if err := Init(bundle, root, "https://:secret@localhost:9443", smtpFile, certFile, keyFile, certFile); err == nil {
		t.Fatal("credential-bearing origin accepted")
	}
	if err := Init(bundle, root, "https://localhost:9443", smtpFile, certFile, keyFile, certFile); err != nil {
		t.Fatal(err)
	}
	s, err := loadSettings(root)
	if err != nil || len(s.Secrets["auth"]) != 32 {
		t.Fatal("private secrets not initialized", err)
	}
	var c struct {
		Services map[string]struct {
			Ports       []string
			Network     string `json:"network_mode"`
			Command     []string
			Environment map[string]string
		}
	}
	raw, _ := os.ReadFile(filepath.Join(root, "compose.json"))
	if json.Unmarshal(raw, &c) != nil {
		t.Fatal("invalid Compose")
	}
	for name, value := range c.Services {
		if name != "edge" && len(value.Ports) > 0 {
			t.Fatal("private service published", name)
		}
		for _, arg := range value.Command {
			if arg == "--dev" {
				t.Fatal("development authentication mode")
			}
		}
	}
	if c.Services["api"].Network != "service:edge" || strings.Contains(string(raw), "password$$") {
		t.Fatal("private layout or SMTP isolation")
	}
	for _, key := range []string{"KOUJOU_HYDRA_ADMIN_URL", "KOUJOU_KRATOS_PUBLIC_URL", "KOUJOU_KRATOS_ADMIN_URL"} {
		if !strings.HasPrefix(c.Services["api"].Environment[key], "http://127.0.0.1:8081/") {
			t.Errorf("%s cannot reach the loopback-only administration listener", key)
		}
	}
	info, _ := os.Stat(filepath.Join(root, "settings.json"))
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatal("settings are not private")
	}
	if err = Init(bundle, root, "https://localhost:9443", smtpFile, certFile, keyFile, certFile); err == nil {
		t.Fatal("overwrote existing deployment")
	}
	os.WriteFile(filepath.Join(bundle, "server-image.tar"), []byte("changed"), 0600)
	if _, err = LoadRelease(bundle); err == nil {
		t.Fatal("unverified image accepted")
	}
}

func TestBackupCannotWriteOutsideNewRestoreRoot(t *testing.T) {
	base := t.TempDir()
	file := filepath.Join(base, "invalid.tgz")
	f, _ := os.Create(file)
	z := gzip.NewWriter(f)
	a := tar.NewWriter(z)
	a.WriteHeader(&tar.Header{Name: "../escaped", Typeflag: tar.TypeReg, Mode: 0600, Size: 6})
	a.Write([]byte("secret"))
	a.Close()
	z.Close()
	f.Close()
	root := filepath.Join(base, "restore")
	os.Mkdir(root, 0700)
	if err := unpack(file, root); err == nil {
		t.Fatal("backup escaped its restore root")
	}
	if _, err := os.Stat(filepath.Join(base, "escaped")); !os.IsNotExist(err) {
		t.Fatal("outside file created")
	}
}
