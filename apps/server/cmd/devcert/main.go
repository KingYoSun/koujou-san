// Creates a local development certificate without importing it into OS trust.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

func main() {
	dir := os.Args[1]
	if err := os.MkdirAll(dir, 0700); err != nil {
		panic(err)
	}
	certPath, keyPath := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if _, err := os.Stat(certPath); err == nil {
		if _, err = os.Stat(keyPath); err != nil {
			panic("development key missing")
		}
		return
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		panic(err)
	}
	template := x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "koujou-san local development"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(365 * 24 * time.Hour), DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true, IsCA: true}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		panic(err)
	}
	for target, block := range map[string]*pem.Block{certPath: {Type: "CERTIFICATE", Bytes: der}, keyPath: {Type: "PRIVATE KEY", Bytes: private}} {
		file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			panic(err)
		}
		if err = pem.Encode(file, block); err != nil {
			panic(err)
		}
		if err = file.Close(); err != nil {
			panic(err)
		}
	}
}
