package keychain

import "errors"

var ErrNotFound = errors.New("credential not found")

type Store interface {
	Get(service, account string) (string, error)
	Set(service, account, secret string) error
	Delete(service, account string) error
}
