package auth_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/fleveque/quantic-cli/internal/auth"
	"github.com/fleveque/quantic-cli/internal/auth/authtest"
)

var errNoSecretService = authtest.ErrNoSecretService

func newStore(t *testing.T) (*auth.Store, *authtest.Keyring) {
	t.Helper()
	k := &authtest.Keyring{}
	return &auth.Store{Keyring: k, File: filepath.Join(t.TempDir(), "quantic", "tokens.json")}, k
}

func secret(t *testing.T, s string) auth.Secret {
	t.Helper()
	v, err := auth.ParseSecret(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func load(t *testing.T, s *auth.Store, host string) auth.Token {
	t.Helper()
	tok, err := s.Load(host)
	if err != nil {
		t.Fatalf("Load(%q): %v", host, err)
	}
	return tok
}

func TestSaveAndLoadUseTheKeyring(t *testing.T) {
	s, k := newStore(t)
	saved, err := s.Save("quantic.finance", secret(t, "qtc_one\n"))
	if err != nil || saved.Source != auth.FromKeyring || saved.KeyringErr != nil {
		t.Fatalf("Save = %+v, %v; want the keyring", saved, err)
	}
	if got, _ := k.Secret("quantic-cli", "quantic.finance"); got != "qtc_one" {
		t.Errorf("keyring holds %q, want qtc_one, trimmed", got)
	}
	if _, err := os.Stat(s.File); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the file exists (%v); a token in the keyring needs no file", err)
	}

	tok := load(t, s, "quantic.finance")
	if tok.Secret.Reveal() != "qtc_one" || tok.Source != auth.FromKeyring {
		t.Errorf("Load = %s from %s, want qtc_one from the keyring", tok.Secret.Reveal(), tok.Source)
	}
}

func TestEachHostHasItsOwnToken(t *testing.T) {
	s, _ := newStore(t)
	s.Save("quantic.finance", secret(t, "qtc_prod"))
	s.Save("localhost:4000", secret(t, "qtc_dev"))

	if got := load(t, s, "quantic.finance").Secret.Reveal(); got != "qtc_prod" {
		t.Errorf("quantic.finance: %s, want qtc_prod", got)
	}
	if got := load(t, s, "localhost:4000").Secret.Reveal(); got != "qtc_dev" {
		t.Errorf("localhost:4000: %s, want qtc_dev", got)
	}
	if _, err := s.Load("example.com"); !errors.Is(err, auth.ErrNotSignedIn) {
		t.Errorf("another host: %v, want ErrNotSignedIn", err)
	}
}

func TestEnvWinsOverWhatIsStored(t *testing.T) {
	s, _ := newStore(t)
	s.Save("quantic.finance", secret(t, "qtc_stored"))
	s.Env = "qtc_env"

	tok := load(t, s, "quantic.finance")
	if tok.Secret.Reveal() != "qtc_env" || tok.Source != auth.FromEnv {
		t.Errorf("Load = %s from %s, want qtc_env from QUANTIC_TOKEN", tok.Secret.Reveal(), tok.Source)
	}

	s.Env = "qtc_with space"
	_, err := s.Load("quantic.finance")
	if err == nil || !strings.HasPrefix(err.Error(), "QUANTIC_TOKEN: ") {
		t.Errorf("a malformed QUANTIC_TOKEN: %v, want an error naming it", err)
	}
}

// Without a keyring (a server, a container, a desktop with no Secret
// Service), the token goes to a file only its owner can read (design §4).
func TestWithoutAKeyringTheTokenGoesToAPrivateFile(t *testing.T) {
	s, k := newStore(t)
	k.Err = errNoSecretService

	saved, err := s.Save("quantic.finance", secret(t, "qtc_one"))
	if err != nil {
		t.Fatal(err)
	}
	if saved.Source != auth.FromFile || !errors.Is(saved.KeyringErr, errNoSecretService) {
		t.Errorf("Save = %+v, want the file, and why not the keyring", saved)
	}

	info, err := os.Stat(s.File)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode %04o, want 0600", perm)
	}
	dir, _ := os.Stat(filepath.Dir(s.File))
	if perm := dir.Mode().Perm(); perm != 0o700 {
		t.Errorf("directory mode %04o, want 0700", perm)
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(s.File), ".tokens-*"))
	if len(leftovers) > 0 {
		t.Errorf("temporary files left behind: %v", leftovers)
	}

	tok := load(t, s, "quantic.finance")
	if tok.Secret.Reveal() != "qtc_one" || tok.Source != auth.FromFile {
		t.Errorf("Load = %s from %s, want qtc_one from the file", tok.Secret.Reveal(), tok.Source)
	}
}

func TestAFileOthersCanReadIsRefused(t *testing.T) {
	s, k := newStore(t)
	k.Err = errNoSecretService
	s.Save("quantic.finance", secret(t, "qtc_one"))
	os.Chmod(s.File, 0o644)

	_, err := s.Load("quantic.finance")
	if err == nil || !strings.Contains(err.Error(), "can be read by other users (mode 0644)") {
		t.Fatalf("Load = %v, want a refusal", err)
	}
	if strings.Contains(err.Error(), "qtc_one") {
		t.Errorf("the error shows the token: %v", err)
	}

	// Replacing or removing the token is still allowed: that's what someone
	// who finds the file readable needs to do, and the new file is 0600.
	if _, err := s.Save("quantic.finance", secret(t, "qtc_two")); err != nil {
		t.Fatalf("Save over a readable file: %v", err)
	}
	if info, _ := os.Stat(s.File); info.Mode().Perm() != 0o600 {
		t.Errorf("after Save, mode %04o, want 0600", info.Mode().Perm())
	}
	os.Chmod(s.File, 0o644)
	if removed, err := s.Delete("quantic.finance"); err != nil || len(removed) != 1 {
		t.Errorf("Delete from a readable file = %v, %v", removed, err)
	}
}

// Once the keyring works again, saving there removes the file's copy, which
// would otherwise be found the next time the keyring failed.
func TestSavingToTheKeyringRemovesTheFilesCopy(t *testing.T) {
	s, k := newStore(t)
	k.Err = errNoSecretService
	s.Save("quantic.finance", secret(t, "qtc_old"))
	s.Save("localhost:4000", secret(t, "qtc_dev"))

	k.Err = nil
	s.Save("quantic.finance", secret(t, "qtc_new"))

	var inFile map[string]string
	b, _ := os.ReadFile(s.File)
	json.Unmarshal(b, &inFile)
	if _, ok := inFile["quantic.finance"]; ok || inFile["localhost:4000"] != "qtc_dev" {
		t.Errorf("file = %v, want only localhost:4000's token", inFile)
	}
}

func TestDelete(t *testing.T) {
	s, k := newStore(t)
	s.Save("quantic.finance", secret(t, "qtc_one"))

	removed, err := s.Delete("quantic.finance")
	if err != nil || len(removed) != 1 || removed[0] != auth.FromKeyring {
		t.Fatalf("Delete = %v, %v; want [keyring]", removed, err)
	}
	if _, err := s.Load("quantic.finance"); !errors.Is(err, auth.ErrNotSignedIn) {
		t.Errorf("after Delete, Load = %v, want ErrNotSignedIn", err)
	}

	removed, err = s.Delete("quantic.finance")
	if err != nil || len(removed) != 0 {
		t.Errorf("Delete again = %v, %v; want nothing removed, no error", removed, err)
	}

	k.Err = errNoSecretService
	s.Save("quantic.finance", secret(t, "qtc_two"))
	removed, err = s.Delete("quantic.finance")
	if err != nil || len(removed) != 1 || removed[0] != auth.FromFile {
		t.Errorf("Delete with no keyring = %v, %v; want [file]", removed, err)
	}
	if _, err := os.Stat(s.File); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("an empty file was left behind (%v)", err)
	}

	// The keyring is down and the file has nothing: the token may be in the
	// keyring still, so "nothing to remove" would be a guess.
	if _, err := s.Delete("quantic.finance"); !errors.Is(err, errNoSecretService) {
		t.Errorf("Delete with a failing keyring and no file = %v, want the keyring's error", err)
	}
}

func TestParseSecret(t *testing.T) {
	for _, bad := range []string{"", "  \n", "qtc_a b", "qtc_a\tb", "qtc_a\x00b", "qtc_a\nb"} {
		if _, err := auth.ParseSecret(bad); err == nil {
			t.Errorf("ParseSecret(%q) = nil error", bad)
		} else if strings.Contains(err.Error(), "qtc_") {
			t.Errorf("ParseSecret(%q)'s error shows the token: %v", bad, err)
		}
	}
}

// The token can't be printed by accident: not by fmt with any verb, not
// inside a struct, not in JSON, not in a log line.
func TestSecretNeverPrints(t *testing.T) {
	s := secret(t, "qtc_supersecret")
	tok := auth.Token{Secret: s, Source: auth.FromKeyring}

	var out []string
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%d"} {
		out = append(out, fmt.Sprintf(verb, s), fmt.Sprintf(verb, tok), fmt.Sprintf(verb, &tok))
	}
	out = append(out, fmt.Sprint(s), fmt.Sprintln(tok))
	// fmt can't call Format through an unexported field; see Secret.
	type holder struct{ tok auth.Secret }
	out = append(out, fmt.Sprintf("%+v", holder{s}), fmt.Sprintf("%#v", holder{s}))
	b, err := json.Marshal(tok)
	if err != nil {
		t.Fatal(err)
	}
	out = append(out, string(b))
	var log strings.Builder
	slog.New(slog.NewTextHandler(&log, nil)).Info("loaded", "token", s, "tok", tok)
	slog.New(slog.NewJSONHandler(&log, nil)).Info("loaded", "token", s)
	out = append(out, log.String())

	for _, o := range out {
		if strings.Contains(o, "supersecret") || strings.Contains(o, "7375706572") /* hex */ {
			t.Errorf("printed the token: %s", o)
		}
	}
	if !strings.Contains(fmt.Sprintf("%+v", tok), "[redacted]") {
		t.Errorf("%%+v = %+v, want [redacted] in the token's place", tok)
	}
}

// SystemKeyring speaks go-keyring's language to go-keyring and this
// package's to the rest: "nothing there" is ErrNotFound either way.
// go-keyring's mock replaces the real keyring for the whole process.
func TestSystemKeyring(t *testing.T) {
	keyring.MockInit()
	k := auth.SystemKeyring{}

	if _, err := k.Get("quantic-cli", "quantic.finance"); !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("Get of nothing = %v, want ErrNotFound", err)
	}
	if err := k.Delete("quantic-cli", "quantic.finance"); !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("Delete of nothing = %v, want ErrNotFound", err)
	}
	if err := k.Set("quantic-cli", "quantic.finance", "qtc_one"); err != nil {
		t.Fatal(err)
	}
	if v, err := k.Get("quantic-cli", "quantic.finance"); v != "qtc_one" || err != nil {
		t.Errorf("Get = %q, %v", v, err)
	}

	keyring.MockInitWithError(errNoSecretService)
	if _, err := k.Get("quantic-cli", "quantic.finance"); !errors.Is(err, errNoSecretService) {
		t.Errorf("a failing keyring's Get = %v, want its own error, not ErrNotFound", err)
	}
}
