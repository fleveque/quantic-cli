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
// reflection. What stops it is the pointer below.
type Secret struct {
	// Behind a pointer because fmt prints a pointer below the top level as
	// its address: whatever the verb, and however deep a Secret is nested,
	// fmt shows 0xc000…, never the token. Format makes that [redacted].
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

// Format is fmt's hook for every verb, so an error message that includes a
// Secret says [redacted] rather than an address. fmt can't call it through
// an unexported field of another struct; there, the pointer shows instead.
func (Secret) Format(f fmt.State, _ rune) { io.WriteString(f, redacted) }
