package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/novr/utsusemi/internal/config"
)

func writeConfig(path string, cfg *config.Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return writeConfigBytes(path, data)
}

func writeConfigBytes(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".utsusemi-config-*.yaml")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func confirmConfigOverwrite(path string, force bool, in io.Reader, out io.Writer) error {
	if force {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("config path %s is a directory", path)
	}
	interactive := false
	if file, ok := in.(*os.File); ok {
		interactive = isTerminal(file)
	}
	if !interactive {
		return fmt.Errorf("%s already exists; re-run with --force to update", path)
	}
	fmt.Fprintf(out, "%s already exists. update? [y/N]: ", path)
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && err != io.EOF {
		return err
	}
	answer := strings.TrimSpace(strings.ToLower(line))
	if answer != "y" && answer != "yes" {
		return fmt.Errorf("aborted; existing config left unchanged")
	}
	return nil
}

func printConfigureSuccess(path string, res configureSuccess) {
	if res.WroteConfig {
		fmt.Printf("wrote config to %s\n", path)
	} else {
		fmt.Printf("credential updated (config unchanged at %s)\n", path)
	}
	switch {
	case res.CredentialUpdated:
		if res.GitHubUser != "" {
			fmt.Printf("credential stored in keychain (GitHub user: %s)\n", res.GitHubUser)
		} else {
			fmt.Println("credential stored in keychain")
		}
	case res.CredentialMissing:
		fmt.Println("credential not configured; run configure again with a token or complete device flow")
	default:
		fmt.Println("config updated; credential unchanged")
	}
	fmt.Println("next: run `utsusemi validate`, then `utsusemi run`")
	fmt.Println("or start it in the background with `brew services start utsusemi`")
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
