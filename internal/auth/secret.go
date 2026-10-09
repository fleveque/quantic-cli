// Package auth finds, stores and forgets the API token (design §4).
//
// A token comes from QUANTIC_TOKEN, the system keyring, or a file, in that
// order. Each is kept per Quantic host, so a token for a local Phoenix server
// never replaces the one for quantic.finance.
package auth

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
)

// Secret is a token that can't be printed by accident; only Reveal gives
// the token, and only the code that sets the Authorization header calls it.
//
// encoding/json, and log/slog's JSON handler, can't see an unexported field,
// so to them a Secret is {}. fmt can: it reads unexported fields through
// reflection, which is what Format below is for.
type Secret struct {
	// Behind a pointer, for the one case Format can't cover: fmt can't call
	// a method through an unexported field, so a Secret in one, printed with
	// %+v, is printed field by field. A pointer below the top level is
	// printed as its address, so even then the token isn't.
	value *string
}

// ParseSecret checks that s looks like a token, after trimming the newline a
// file or a paste leaves. It never puts s in its error.
func ParseSecret(s string) (Secret, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Secret{}, errors.New("the token is empty")
	}
	// A space, a tab or a control character would end up in an HTTP header,
	// where it either breaks the request or means something else.
	if strings.ContainsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return Secret{}, errors.New("the token has a space or a control character in it; a token is one word")
	}
	return Secret{value: &s}, nil
}

// Reveal returns the token itself, for the Authorization header.
func (s Secret) Reveal() string {
	if s.value == nil {
		return ""
	}
	return *s.value
}

// IsZero reports whether there is no token.
func (s Secret) IsZero() bool { return s.value == nil }

const redacted = "[redacted]"

// Format is fmt's hook for every verb: %v, %s, %q, %x, %d, %+v, %#v. Even a
// struct holding a Secret, printed with %+v, shows [redacted] in its place.
// log/slog's text handler prints with fmt, so this covers it too.
func (Secret) Format(f fmt.State, _ rune) { io.WriteString(f, redacted) }
