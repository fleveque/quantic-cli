package auth

import (
	"errors"

	"github.com/zalando/go-keyring"
)

// SystemKeyring is the operating system's keyring: the Secret Service on
// Linux (GNOME Keyring, KWallet), the Keychain on macOS, the Credential
// Manager on Windows.
type SystemKeyring struct{}

func (SystemKeyring) Get(service, user string) (string, error) {
	v, err := keyring.Get(service, user)
	return v, notFound(err)
}

func (SystemKeyring) Set(service, user, secret string) error {
	return keyring.Set(service, user, secret)
}

func (SystemKeyring) Delete(service, user string) error {
	return notFound(keyring.Delete(service, user))
}

// notFound turns go-keyring's own "nothing there" into this package's, so
// the rest of the CLI never imports go-keyring.
func notFound(err error) error {
	if errors.Is(err, keyring.ErrNotFound) {
		return ErrNotFound
	}
	return err
}
