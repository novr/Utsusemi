package hostid

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Info struct {
	ID              string
	Hostname        string
	LocalHostName   string
	EffectivePrefix string
	Warnings        []string
}

func Load(stateDir string) string {
	return loadOrInit(stateDir)
}

func Collect(stateDir, vmNamePrefix string) Info {
	hostID := loadOrInit(stateDir)
	hostname, _ := os.Hostname()
	localName := localHostName()

	info := Info{
		ID:              hostID,
		Hostname:        hostname,
		LocalHostName:   localName,
		EffectivePrefix: vmNamePrefix + hostID + "-",
	}
	info.Warnings = warnings(hostID)
	return info
}

func ParseFromRunnerName(name, vmPrefix string) (hostID string, ok bool) {
	if !strings.HasPrefix(name, vmPrefix) {
		return "", false
	}
	rest := strings.TrimPrefix(name, vmPrefix)
	idx := strings.LastIndex(rest, "-")
	if idx <= 0 {
		return "", false
	}
	suffix := rest[idx+1:]
	if len(suffix) != 8 {
		return "", false
	}
	if _, err := hex.DecodeString(suffix); err != nil {
		return "", false
	}
	return rest[:idx], true
}

func loadOrInit(stateDir string) string {
	path := filepath.Join(stateDir, "host_id")
	if data, err := os.ReadFile(path); err == nil {
		if id := strings.TrimSpace(string(data)); id != "" {
			return id
		}
	}
	id := Sanitize(localHostName())
	if id == "" {
		hostname, _ := os.Hostname()
		id = Sanitize(hostname)
	}
	if id == "" {
		buf := make([]byte, 4)
		_, _ = rand.Read(buf)
		id = hex.EncodeToString(buf)
	}
	_ = os.MkdirAll(stateDir, 0o755)
	_ = os.WriteFile(path, []byte(id), 0o644)
	return id
}

func Sanitize(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	result := strings.Trim(b.String(), "-")
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	if len(result) > 24 {
		result = result[:24]
		result = strings.TrimRight(result, "-")
	}
	return result
}

func localHostName() string {
	out, err := exec.Command("scutil", "--get", "LocalHostName").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

var genericHostIDs = map[string]struct{}{
	"mac": {}, "macos": {}, "macbook": {}, "macbookpro": {}, "imac": {},
	"macmini": {}, "macstudio": {}, "localhost": {}, "computer": {},
}

func IsGeneric(id string) bool {
	_, ok := genericHostIDs[id]
	return ok
}

func warnings(hostID string) []string {
	var out []string
	if len(hostID) < 3 {
		out = append(out, "host identifier is very short; set a unique LocalHostName on each Mac")
	}
	if IsGeneric(hostID) {
		out = append(out, "host identifier looks generic ("+hostID+"); use a unique LocalHostName on each Mac to avoid multi-host collisions")
	}
	return out
}
