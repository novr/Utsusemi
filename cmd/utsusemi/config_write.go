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
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
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
		return fmt.Errorf("%s already exists; re-run with --force to overwrite", path)
	}
	fmt.Fprintf(out, "%s already exists. overwrite? [y/N]: ", path)
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

func printConfigureSuccess(path string) {
	fmt.Printf("wrote config to %s\n", path)
	fmt.Println("credential stored in keychain")
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
