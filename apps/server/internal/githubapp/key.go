package githubapp

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
)

func ReadKey(path string) (*rsa.PrivateKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("GitHub private key unreadable")
	}
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, errors.New("GitHub private key invalid")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	if value, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if key, ok := value.(*rsa.PrivateKey); ok {
			return key, nil
		}
	}
	return nil, errors.New("GitHub RSA private key required")
}
