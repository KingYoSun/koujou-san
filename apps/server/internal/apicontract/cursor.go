package apicontract

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
)

// CursorCodec protects internal pagination state. Callers bind their actor,
// Workspace, query and projection position in the payload and verify them.
type CursorCodec struct{ crypt cipher.AEAD }

func NewCursorCodec(key []byte, purpose string) *CursorCodec {
	sum := sha256.Sum256(append([]byte(purpose+":"), key...))
	block, _ := aes.NewCipher(sum[:])
	crypt, _ := cipher.NewGCM(block)
	return &CursorCodec{crypt}
}
func (c *CursorCodec) Seal(value any, aad string) string {
	raw, _ := json.Marshal(value)
	nonce := make([]byte, c.crypt.NonceSize())
	_, _ = rand.Read(nonce)
	return base64.RawURLEncoding.EncodeToString(c.crypt.Seal(nonce, nonce, raw, []byte(aad)))
}
func (c *CursorCodec) Open(raw, aad string, value any) error {
	data, err := base64.RawURLEncoding.DecodeString(raw)
	n := c.crypt.NonceSize()
	if err != nil || len(data) < n {
		return errors.New("invalid cursor")
	}
	plain, err := c.crypt.Open(nil, data[:n], data[n:], []byte(aad))
	if err != nil {
		return err
	}
	return json.Unmarshal(plain, value)
}
