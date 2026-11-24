package auth

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/argon2"
)

func TestPasswordRoundTrip(t *testing.T) {
	h, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Fatalf("unexpected PHC: %s", h)
	}
	ok, rehash, err := VerifyPassword("correct horse battery staple", h)
	if err != nil || !ok || rehash {
		t.Fatalf("verify = %v %v %v", ok, rehash, err)
	}
	ok, _, _ = VerifyPassword("wrong horse battery staple", h)
	if ok {
		t.Fatal("wrong password accepted")
	}
}

func TestPasswordNeedsRehashOnOldParams(t *testing.T) {
	salt := []byte("0123456789abcdef")
	key := argon2.IDKey([]byte("a long enough password"), salt, 2, 16*1024, 1, 32)
	enc := base64.RawStdEncoding
	weak := "$argon2id$v=19$m=16384,t=2,p=1$" + enc.EncodeToString(salt) + "$" + enc.EncodeToString(key)
	ok, rehash, err := VerifyPassword("a long enough password", weak)
	if err != nil || !ok {
		t.Fatalf("weak hash should still verify: %v %v", ok, err)
	}
	if !rehash {
		t.Fatal("weak params should request a rehash")
	}
}

func TestPasswordPolicy(t *testing.T) {
	cases := map[string]error{
		"short":                  ErrPasswordTooShort,
		"iloveyou1234":           ErrPasswordBreached,
		"ILoveYou1234":           ErrPasswordBreached,
		"plum-orbit-kettle-91":   nil,
		strings.Repeat("x", 257): ErrPasswordTooLong,
	}
	for pw, want := range cases {
		if got := CheckPasswordPolicy(pw); got != want {
			t.Errorf("CheckPasswordPolicy(%q) = %v, want %v", pw, got, want)
		}
	}
}

func TestTOTPRFC6238Vector(t *testing.T) {
	// RFC 6238 appendix B, SHA1 secret "12345678901234567890", T=59 -> 94287082 (8 digits); 6-digit = 287082.
	secret := b32.EncodeToString([]byte("12345678901234567890"))
	code, err := TOTPCodeAt(secret, time.Unix(59, 0))
	if err != nil {
		t.Fatal(err)
	}
	if code != "287082" {
		t.Fatalf("code = %s, want 287082", code)
	}
}

func TestTOTPReplayAndWindow(t *testing.T) {
	secret, _ := NewTOTPSecret()
	now := time.Unix(1_800_000_000, 0)
	code, _ := TOTPCodeAt(secret, now)
	step, ok := VerifyTOTP(secret, code, now, 0)
	if !ok {
		t.Fatal("valid code rejected")
	}
	if _, ok := VerifyTOTP(secret, code, now, step); ok {
		t.Fatal("replayed code accepted")
	}
	prev, _ := TOTPCodeAt(secret, now.Add(-30*time.Second))
	if _, ok := VerifyTOTP(secret, prev, now, 0); !ok {
		t.Fatal("code from previous step should be accepted")
	}
	old, _ := TOTPCodeAt(secret, now.Add(-90*time.Second))
	if _, ok := VerifyTOTP(secret, old, now, 0); ok && old != code && old != prev {
		t.Fatal("code from 3 steps ago accepted")
	}
}

func TestSecretBox(t *testing.T) {
	box, err := NewSecretBox(bytes.Repeat([]byte{7}, 32), "totp")
	if err != nil {
		t.Fatal(err)
	}
	sealed := box.Seal([]byte("JBSWY3DPEHPK3PXP"))
	if bytes.Contains(sealed, []byte("JBSWY3DP")) {
		t.Fatal("plaintext visible in ciphertext")
	}
	plain, err := box.Open(sealed)
	if err != nil || string(plain) != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("open = %q %v", plain, err)
	}
	other, _ := NewSecretBox(bytes.Repeat([]byte{7}, 32), "other-purpose")
	if _, err := other.Open(sealed); err == nil {
		t.Fatal("key for another purpose decrypted the secret")
	}
}

func TestTokens(t *testing.T) {
	tok, disp := NewPAT()
	if !strings.HasPrefix(tok, PATPrefix) || len(tok) != len(PATPrefix)+43 || !strings.HasPrefix(tok, disp) {
		t.Fatalf("bad PAT %q / %q", tok, disp)
	}
	rc := NewRecoveryCode()
	if NormalizeRecoveryCode(strings.ToUpper(strings.ReplaceAll(rc, "-", ""))) != rc {
		t.Fatalf("recovery code normalization failed for %s", rc)
	}
}
