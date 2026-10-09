// Package authtest has keyrings for tests, so that no test ever reaches the
// keyring of the machine it runs on. Clearing the environment isn't enough
// for that: the D-Bus library behind the Secret Service finds the session
// bus at /run/user/<uid>/bus whatever the environment says.
package authtest

import (
	"encoding/json"
	"errors"
	"os"
	"sync"

	"github.com/fleveque/quantic-cli/internal/auth"
)

// ErrNoSecretService is what the Secret Service's absence looks like.
var ErrNoSecretService = errors.New("The name org.freedesktop.secrets was not provided by any .service files")

// Keyring is a keyring in memory. With Err set, every call fails with it,
// as the real one does when no Secret Service is running.
type Keyring struct {
	Err error

	mu      sync.Mutex
	secrets map[string]string
}

func (k *Keyring) Get(service, user string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.Err != nil {
		return "", k.Err
	}
	v, ok := k.secrets[service+"/"+user]
	if !ok {
		return "", auth.ErrNotFound
	}
	return v, nil
}

func (k *Keyring) Set(service, user, secret string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.Err != nil {
		return k.Err
	}
	if k.secrets == nil {
		k.secrets = map[string]string{}
	}
	k.secrets[service+"/"+user] = secret
	return nil
}

func (k *Keyring) Delete(service, user string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.Err != nil {
		return k.Err
	}
	if _, ok := k.secrets[service+"/"+user]; !ok {
		return auth.ErrNotFound
	}
	delete(k.secrets, service+"/"+user)
	return nil
}

// Secret returns what the keyring holds for service and user, for a test to
// check.
func (k *Keyring) Secret(service, user string) (string, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	v, ok := k.secrets[service+"/"+user]
	return v, ok
}

// FileKeyring is a keyring in a JSON file, for tests that run the CLI as
// separate processes: what `auth login` stores, the next command finds.
type FileKeyring struct {
	Path string
	Err  error
}

func (k FileKeyring) Get(service, user string) (string, error) {
	m, err := k.read()
	if err != nil {
		return "", err
	}
	v, ok := m[service+"/"+user]
	if !ok {
		return "", auth.ErrNotFound
	}
	return v, nil
}

func (k FileKeyring) Set(service, user, secret string) error {
	m, err := k.read()
	if err != nil {
		return err
	}
	m[service+"/"+user] = secret
	return k.write(m)
}

func (k FileKeyring) Delete(service, user string) error {
	m, err := k.read()
	if err != nil {
		return err
	}
	if _, ok := m[service+"/"+user]; !ok {
		return auth.ErrNotFound
	}
	delete(m, service+"/"+user)
	return k.write(m)
}

func (k FileKeyring) read() (map[string]string, error) {
	if k.Err != nil {
		return nil, k.Err
	}
	m := map[string]string{}
	b, err := os.ReadFile(k.Path)
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	return m, json.Unmarshal(b, &m)
}

func (k FileKeyring) write(m map[string]string) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(k.Path, b, 0o600)
}
