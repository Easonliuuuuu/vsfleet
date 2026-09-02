package credentials

import (
	"context"
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

// KeyringService is the service name vcfleet registers under in the operating
// system secret store.
const KeyringService = "vcfleet"

// Keyring stores credentials in the operating system secret store: Keychain on
// macOS, Secret Service on Linux, Credential Manager on Windows.
type Keyring struct {
	// Service overrides KeyringService. Tests set this to stay isolated.
	Service string
}

// NewKeyring returns a Keyring provider using the default service name.
func NewKeyring() *Keyring { return &Keyring{} }

func (k *Keyring) service() string {
	if k.Service != "" {
		return k.Service
	}
	return KeyringService
}

func (k *Keyring) Scheme() string { return SchemeKeyring }

func (k *Keyring) Get(_ context.Context, ref Ref) (Credential, error) {
	secret, err := keyring.Get(k.service(), ref.Value)
	switch {
	case errors.Is(err, keyring.ErrNotFound):
		return Credential{}, fmt.Errorf("%w: %s", ErrNotFound, ref)
	case err != nil:
		return Credential{}, fmt.Errorf("read %s from system keyring: %w", ref, err)
	}
	return Credential{Password: secret}, nil
}

func (k *Keyring) Store(_ context.Context, ref Ref, c Credential) error {
	if err := keyring.Set(k.service(), ref.Value, c.Password); err != nil {
		return fmt.Errorf("write %s to system keyring: %w", ref, err)
	}
	return nil
}

func (k *Keyring) Delete(_ context.Context, ref Ref) error {
	err := keyring.Delete(k.service(), ref.Value)
	switch {
	case errors.Is(err, keyring.ErrNotFound):
		return fmt.Errorf("%w: %s", ErrNotFound, ref)
	case err != nil:
		return fmt.Errorf("delete %s from system keyring: %w", ref, err)
	}
	return nil
}
