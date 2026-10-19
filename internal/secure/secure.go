package secure

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/altasci/altascitunnel/internal/model"
	"golang.org/x/crypto/argon2"
	"os"
	"path/filepath"
	"strings"
)

func Token() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func EqualDigest(secret, digest string) bool {
	a, e := hex.DecodeString(digest)
	if e != nil {
		return false
	}
	b, _ := hex.DecodeString(model.Digest([]byte(secret)))
	return subtle.ConstantTimeCompare(a, b) == 1
}

type Vault struct{ aead cipher.AEAD }

func New(key []byte) (*Vault, error) {
	if len(key) != 32 {
		return nil, errors.New("vault requires exactly 32 bytes")
	}
	b, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	a, e := cipher.NewGCM(b)
	return &Vault{a}, e
}
func Load(path string) (*Vault, error) {
	k, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	return New(k)
}
func GenerateKey(path string) error {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	b := make([]byte, 32)
	if _, e = rand.Read(b); e != nil {
		return e
	}
	if _, e = f.Write(b); e != nil {
		return e
	}
	return f.Sync()
}
func (v *Vault) Seal(id string, b []byte) string {
	n := make([]byte, v.aead.NonceSize())
	if _, e := rand.Read(n); e != nil {
		panic(e)
	}
	return base64.RawURLEncoding.EncodeToString(v.aead.Seal(n, n, b, []byte(id)))
}
func (v *Vault) Open(id, s string) ([]byte, error) {
	b, e := base64.RawURLEncoding.DecodeString(s)
	if e != nil || len(b) < v.aead.NonceSize() {
		return nil, errors.New("invalid ciphertext")
	}
	return v.aead.Open(nil, b[:v.aead.NonceSize()], b[v.aead.NonceSize():], []byte(id))
}
func Password(password string) string {
	salt := make([]byte, 16)
	if _, e := rand.Read(salt); e != nil {
		panic(e)
	}
	h := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
	return fmt.Sprintf("argon2id$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(h))
}
func CheckPassword(password, hash string) bool {
	s := strings.Split(hash, "$")
	if len(s) != 3 || s[0] != "argon2id" {
		return false
	}
	salt, e := base64.RawStdEncoding.DecodeString(s[1])
	if e != nil || len(salt) != 16 {
		return false
	}
	expected, e := base64.RawStdEncoding.DecodeString(s[2])
	if e != nil || len(expected) != 32 {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
	return subtle.ConstantTimeCompare(expected, actual) == 1
}
