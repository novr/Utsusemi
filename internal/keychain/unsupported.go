//go:build !darwin

package keychain

import "fmt"

type UnsupportedStore struct{}

func New() Store {
	return UnsupportedStore{}
}

func (UnsupportedStore) Get(_, _ string) (string, error) {
	return "", fmt.Errorf("keychain is only supported on darwin")
}

func (UnsupportedStore) Set(_, _, _ string) error {
	return fmt.Errorf("keychain is only supported on darwin")
}

func (UnsupportedStore) Delete(_, _ string) error {
	return fmt.Errorf("keychain is only supported on darwin")
}
