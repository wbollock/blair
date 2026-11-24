// Package fracindex generates base-62 fractional index keys (SPEC §7.3):
// strings that sort between two neighbours, so a move rewrites one row.
package fracindex

import (
	"errors"
	"strings"
)

const digits = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// ErrOrder is returned when a >= b, or a key has a trailing zero digit.
var ErrOrder = errors.New("fracindex: invalid neighbours")

// Between returns a key strictly between a and b. Empty a means "start",
// empty b means "end".
func Between(a, b string) (string, error) {
	if b != "" && a >= b {
		return "", ErrOrder
	}
	if strings.HasSuffix(a, "0") || strings.HasSuffix(b, "0") {
		return "", ErrOrder
	}
	return midpoint(a, b), nil
}

// After returns a key after a (append to the end).
func After(a string) string { k, _ := Between(a, ""); return k }

// Before returns a key before b (prepend to the start).
func Before(b string) string { k, _ := Between("", b); return k }

func midpoint(a, b string) string {
	if b != "" {
		n := 0
		for n < len(b) {
			ca := byte('0')
			if n < len(a) {
				ca = a[n]
			}
			if ca != b[n] {
				break
			}
			n++
		}
		if n > 0 {
			rest := ""
			if n < len(a) {
				rest = a[n:]
			}
			return b[:n] + midpoint(rest, b[n:])
		}
	}
	da := 0
	if a != "" {
		da = strings.IndexByte(digits, a[0])
	}
	db := len(digits)
	if b != "" {
		db = strings.IndexByte(digits, b[0])
	}
	if db-da > 1 {
		return string(digits[(da+db+1)/2])
	}
	if b != "" && len(b) > 1 {
		return b[:1]
	}
	rest := ""
	if a != "" {
		rest = a[1:]
	}
	return string(digits[da]) + midpoint(rest, "")
}
