// Package auth holds credential primitives: password hashing, TOTP,
// opaque tokens and secret encryption (SPEC §9).
package auth

import (
	"bufio"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters (SPEC §9.2). Raising them upgrades hashes on next login.
const (
	argonMemory  = 64 * 1024
	argonTime    = 3
	argonThreads = 2
	argonKeyLen  = 32
	argonSaltLen = 16
)

const (
	MinPasswordLen = 12
	MaxPasswordLen = 256
)

var (
	ErrPasswordTooShort = errors.New("password must be at least 12 characters")
	ErrPasswordTooLong  = errors.New("password must be at most 256 characters")
	ErrPasswordBreached = errors.New("password appears in a list of breached passwords")
)

//go:embed breached.txt
var breachedRaw string

var (
	breachedOnce sync.Once
	breached     map[string]struct{}
)

// CheckPasswordPolicy enforces length and the breached-password list. No composition rules.
func CheckPasswordPolicy(pw string) error {
	n := utf8.RuneCountInString(pw)
	if n < MinPasswordLen {
		return ErrPasswordTooShort
	}
	if n > MaxPasswordLen {
		return ErrPasswordTooLong
	}
	breachedOnce.Do(func() {
		breached = map[string]struct{}{}
		sc := bufio.NewScanner(strings.NewReader(breachedRaw))
		for sc.Scan() {
			breached[sc.Text()] = struct{}{}
		}
	})
	if _, ok := breached[strings.ToLower(pw)]; ok {
		return ErrPasswordBreached
	}
	return nil
}

// HashPassword returns a PHC-format argon2id hash.
func HashPassword(pw string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// VerifyPassword checks pw against a PHC hash. needsRehash reports outdated parameters.
func VerifyPassword(pw, phc string) (ok, needsRehash bool, err error) {
	parts := strings.Split(phc, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, false, errors.New("unsupported password hash")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, false, err
	}
	var m uint32
	var t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false, false, err
	}
	enc := base64.RawStdEncoding
	salt, err := enc.DecodeString(parts[4])
	if err != nil {
		return false, false, err
	}
	want, err := enc.DecodeString(parts[5])
	if err != nil {
		return false, false, err
	}
	got := argon2.IDKey([]byte(pw), salt, t, m, p, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return false, false, nil
	}
	rehash := version != argon2.Version || m != argonMemory || t != argonTime || p != argonThreads || len(want) != argonKeyLen
	return true, rehash, nil
}

// dummyHash is verified against when a login email is unknown, so timing does
// not reveal whether an account exists.
var dummyHash = func() string {
	h, _ := HashPassword("not-a-real-password-for-timing")
	return h
}()

// BurnPasswordTime spends the same time as a real verification.
func BurnPasswordTime(pw string) { _, _, _ = VerifyPassword(pw, dummyHash) }
