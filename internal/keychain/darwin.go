//go:build darwin

package keychain

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

type DarwinStore struct{}

func New() Store {
	return DarwinStore{}
}

func (DarwinStore) Get(service, account string) (string, error) {
	cmd := exec.Command("security", "find-generic-password", "-s", service, "-a", account, "-w")
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 44 {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("keychain get: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (DarwinStore) Set(service, account, secret string) (err error) {
	_ = exec.Command("security", "delete-generic-password", "-s", service, "-a", account).Run()
	cmd := exec.Command("security", "add-generic-password", "-U", "-s", service, "-a", account, "-w", "-")
	cmd.Stdin = strings.NewReader(secret)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("keychain set: %w: %s", err, stderr.String())
	}
	return nil
}

func (DarwinStore) Delete(service, account string) error {
	cmd := exec.Command("security", "delete-generic-password", "-s", service, "-a", account)
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 44 {
			return ErrNotFound
		}
		return fmt.Errorf("keychain delete: %w", err)
	}
	return nil
}
