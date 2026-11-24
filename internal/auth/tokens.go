package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"math/big"
	"strings"
)

const base62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// RandomString returns n characters from alphabet using crypto/rand.
func RandomString(n int, alphabet string) string {
	var sb strings.Builder
	max := big.NewInt(int64(len(alphabet)))
	for range n {
		i, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic(err) // crypto/rand failure is unrecoverable
		}
		sb.WriteByte(alphabet[i.Int64()])
	}
	return sb.String()
}

// NewSessionToken returns an opaque 256-bit token, base64url encoded.
func NewSessionToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// PATPrefix marks personal access tokens for secret scanners.
const PATPrefix = "blair_pat_"

// NewPAT returns a personal access token and its display prefix.
func NewPAT() (token, display string) {
	token = PATPrefix + RandomString(43, base62) // ~256 bits
	return token, token[:len(PATPrefix)+6]
}

// NewRecoveryCode returns a human-friendly single-use code like "k7f3-9qzt-2mxa".
func NewRecoveryCode() string {
	const alpha = "abcdefghjkmnpqrstuvwxyz23456789"
	s := RandomString(12, alpha)
	return s[0:4] + "-" + s[4:8] + "-" + s[8:12]
}

// NormalizeRecoveryCode lowercases and strips spaces for comparison.
func NormalizeRecoveryCode(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "")
	if len(s) == 12 && !strings.Contains(s, "-") {
		s = s[0:4] + "-" + s[4:8] + "-" + s[8:12]
	}
	return s
}

// HashToken is the stored form of sessions, PATs and invites (high-entropy inputs).
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// SecretBox encrypts small secrets at rest (TOTP seeds) with AES-256-GCM.
type SecretBox struct{ aead cipher.AEAD }

// NewSecretBox derives a purpose-bound key from the master secret with HKDF-SHA256.
func NewSecretBox(master []byte, purpose string) (*SecretBox, error) {
	if len(master) < 32 {
		return nil, errors.New("secret key must be at least 32 bytes")
	}
	key, err := hkdf.Key(sha256.New, master, nil, "blair:"+purpose, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &SecretBox{aead: aead}, nil
}

func (b *SecretBox) Seal(plain []byte) []byte {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic(err)
	}
	return b.aead.Seal(nonce, nonce, plain, nil)
}

func (b *SecretBox) Open(sealed []byte) ([]byte, error) {
	n := b.aead.NonceSize()
	if len(sealed) < n {
		return nil, errors.New("ciphertext too short")
	}
	return b.aead.Open(nil, sealed[:n], sealed[n:], nil)
}
