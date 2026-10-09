package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Keyring is the part of a system keyring the CLI uses: one secret per
// service and user. SystemKeyring is the real one; tests use their own.
type Keyring interface {
	Get(service, user string) (string, error)
	Set(service, user, secret string) error
	Delete(service, user string) error
}

// ErrNotFound is what a Keyring returns when it holds nothing for that
// service and user. Any other error means the keyring itself failed.
var ErrNotFound = errors.New("not in the keyring")

// ErrNotSignedIn means there is no token for the host anywhere.
var ErrNotSignedIn = errors.New("not signed in")

// service is the name the CLI's tokens are filed under in the keyring.
const service = "quantic-cli"

// Source says where a token came from, for messages and `auth status`.
type Source string

const (
	FromEnv     Source = "QUANTIC_TOKEN"
	FromKeyring Source = "keyring"
	FromFile    Source = "file"
)

// Token is a token and where it came from.
type Token struct {
	Secret Secret
	Source Source
}

// Store finds and keeps tokens, one per Quantic host ("quantic.finance",
// "localhost:4000").
type Store struct {
	// Env is QUANTIC_TOKEN's value; set, it wins over anything stored.
	Env string

	Keyring Keyring

	// File is where tokens go when the keyring can't take them (design §4):
	// $XDG_CONFIG_HOME/quantic/tokens.json, readable only by its owner.
	File string
}

// Load finds the token for host: QUANTIC_TOKEN, then the keyring, then the
// file. With none, it returns ErrNotSignedIn.
func (s *Store) Load(host string) (Token, error) {
	if s.Env != "" {
		secret, err := ParseSecret(s.Env)
		if err != nil {
			return Token{}, fmt.Errorf("QUANTIC_TOKEN: %w", err)
		}
		return Token{secret, FromEnv}, nil
	}

	// A keyring that fails (no Secret Service running, say) is the same as an
	// empty one here: the token may be in the file, put there because of it.
	if v, err := s.Keyring.Get(service, host); err == nil {
		secret, err := ParseSecret(v)
		if err != nil {
			return Token{}, fmt.Errorf("the token in the keyring: %w", err)
		}
		return Token{secret, FromKeyring}, nil
	}

	tokens, err := s.readFile(true)
	if err != nil {
		return Token{}, err
	}
	if v, ok := tokens[host]; ok {
		secret, err := ParseSecret(v)
		if err != nil {
			return Token{}, fmt.Errorf("the token in %s: %w", s.File, err)
		}
		return Token{secret, FromFile}, nil
	}
	return Token{}, ErrNotSignedIn
}

// Saved says where Save put a token. KeyringErr is why it isn't in the
// keyring, when it went to the file instead.
type Saved struct {
	Source     Source
	KeyringErr error
}

// Save keeps secret for host in the keyring or, if the keyring fails, in
// the file. Either way, no other copy for host is left behind to be found
// first.
func (s *Store) Save(host string, secret Secret) (Saved, error) {
	keyringErr := s.Keyring.Set(service, host, secret.Reveal())
	if keyringErr == nil {
		// An older token in the file would only be found if the keyring
		// failed later, and then it would be the wrong one.
		if err := s.removeFromFile(host); err != nil {
			return Saved{}, err
		}
		return Saved{Source: FromKeyring}, nil
	}

	tokens, err := s.readFile(false)
	if err != nil {
		return Saved{}, err
	}
	tokens[host] = secret.Reveal()
	if err := s.writeFile(tokens); err != nil {
		return Saved{}, err
	}
	return Saved{Source: FromFile, KeyringErr: keyringErr}, nil
}

// Delete forgets host's token in the keyring and the file, and returns
// where it was. QUANTIC_TOKEN is the caller's to unset.
func (s *Store) Delete(host string) ([]Source, error) {
	var removed []Source
	keyringErr := s.Keyring.Delete(service, host)
	switch {
	case keyringErr == nil:
		removed = append(removed, FromKeyring)
	case errors.Is(keyringErr, ErrNotFound):
		keyringErr = nil
	}

	tokens, err := s.readFile(false)
	if err != nil {
		return removed, err
	}
	if _, ok := tokens[host]; ok {
		if err := s.removeFromFile(host); err != nil {
			return removed, err
		}
		removed = append(removed, FromFile)
	}

	// The keyring failed and the file had nothing: the token may still be in
	// the keyring, and saying "you weren't signed in" could be false.
	if keyringErr != nil && len(removed) == 0 {
		return nil, fmt.Errorf("the keyring: %w", keyringErr)
	}
	return removed, nil
}

// readFile returns the file's tokens by host: none if there is no file.
// With private set, a file anyone else can read is refused, as ssh refuses a
// private key that is: the token in it may already have leaked. Only using a
// token needs that; removing one, or rewriting the file (which writeFile
// makes 0600), is always safe.
func (s *Store) readFile(private bool) (map[string]string, error) {
	tokens := map[string]string{}
	if s.File == "" {
		return tokens, nil
	}
	f, err := os.Open(s.File)
	if errors.Is(err, fs.ErrNotExist) {
		return tokens, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if perm := info.Mode().Perm(); private && perm&0o077 != 0 {
		return nil, fmt.Errorf("%s can be read by other users (mode %04o); "+
			"run chmod 600 on it, and if others may have read it, revoke its tokens in Quantic", s.File, perm)
	}
	if err := json.NewDecoder(f).Decode(&tokens); err != nil {
		return nil, fmt.Errorf("%s: %w", s.File, err)
	}
	return tokens, nil
}

func (s *Store) removeFromFile(host string) error {
	tokens, err := s.readFile(false)
	if err != nil {
		return err
	}
	if _, ok := tokens[host]; !ok {
		return nil
	}
	delete(tokens, host)
	if len(tokens) == 0 {
		return os.Remove(s.File)
	}
	return s.writeFile(tokens)
}

// writeFile replaces the file in one step: written to a temporary file next
// to it, then renamed over it. A crash leaves the old file or the new one,
// never half of either. os.CreateTemp makes the file 0600 from the start,
// so the token is never readable by others, not even for a moment.
func (s *Store) writeFile(tokens map[string]string) error {
	if s.File == "" {
		return errors.New("there's no keyring, and nowhere for a token file: neither $XDG_CONFIG_HOME nor $HOME is set")
	}
	dir := filepath.Dir(s.File)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tokens-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // after a successful rename, there's nothing to remove

	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(tokens); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.File)
}
