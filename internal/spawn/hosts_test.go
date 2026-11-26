package spawn

import (
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/novr/utsusemi/internal/config"
)

func TestFormatHostsMap(t *testing.T) {
	got, err := formatHostsMap([]config.HostsMapEntry{
		{IP: netip.MustParseAddr("10.0.0.5"), Host: "proxy.example"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "10.0.0.5 proxy.example\n" {
		t.Fatalf("%q", got)
	}
	if _, err := formatHostsMap([]config.HostsMapEntry{{IP: netip.MustParseAddr("10.0.0.5"), Host: "bad host"}}); err == nil {
		t.Fatal("expected bad host error")
	}
}

func TestHostsSetupScript(t *testing.T) {
	work := t.TempDir()
	hostsPath := filepath.Join(work, "hosts")
	if err := os.WriteFile(hostsPath, []byte("127.0.0.1 localhost\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(work, "calls.txt")
	bin := t.TempDir()
	writeFake(t, bin, "sudo", `#!/bin/bash
if [ "${1:-}" != -n ]; then echo 'sudo without -n' >&2; exit 3; fi
shift
"$@"
`)
	writeFake(t, bin, "tee", fmt.Sprintf(`#!/bin/bash
cat >> %s
`, shellQuote(hostsPath)))
	writeFake(t, bin, "dscacheutil", fmt.Sprintf(`#!/bin/bash
echo "$@" >> %s
if [ "$1" = -flushcache ]; then exit 0; fi
if [ "$1" = -q ] && [ "$2" = host ] && [ "$3" = -a ] && [ "$4" = name ]; then
  echo "name: $5"
  echo "ip_address: 10.0.0.5"
  exit 0
fi
exit 1
`, shellQuote(logPath)))
	writeFake(t, bin, "killall", `#!/bin/bash
exit 0
`)
	writeFake(t, bin, "grep", `#!/bin/bash
/usr/bin/grep "$@"
`)
	writeFake(t, bin, "mktemp", fmt.Sprintf(`#!/bin/bash
f=%s/tmp.$$
: > "$f"
echo "$f"
`, shellQuote(work)))

	cmd := exec.Command("bash", "-c", hostsSetupScript)
	cmd.Env = []string{"PATH=" + bin + ":/usr/bin:/bin", "HOME=" + work}
	cmd.Stdin = strings.NewReader("10.0.0.5 proxy.example\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	data, err := os.ReadFile(hostsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "10.0.0.5 proxy.example # utsusemi") {
		t.Fatalf("hosts=%q", data)
	}
}

func TestHostsSetupScriptRejectsBadLine(t *testing.T) {
	cmd := exec.Command("bash", "-c", hostsSetupScript)
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	cmd.Stdin = strings.NewReader("10.0.0.5 bad host\n")
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("expected failure: %s", out)
	}
}

func TestHostsSetupScriptRejectsLookupMiss(t *testing.T) {
	work := t.TempDir()
	hostsPath := filepath.Join(work, "hosts")
	if err := os.WriteFile(hostsPath, []byte("127.0.0.1 localhost\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	writeFake(t, bin, "sudo", `#!/bin/bash
if [ "${1:-}" != -n ]; then exit 3; fi
shift
"$@"
`)
	writeFake(t, bin, "tee", fmt.Sprintf(`#!/bin/bash
cat >> %s
`, shellQuote(hostsPath)))
	writeFake(t, bin, "dscacheutil", `#!/bin/bash
if [ "$1" = -flushcache ]; then exit 0; fi
if [ "$1" = -q ]; then
  echo "name: proxy.example"
  echo "ip_address: 10.0.0.9"
  exit 0
fi
exit 1
`)
	writeFake(t, bin, "killall", `#!/bin/bash
exit 0
`)
	writeFake(t, bin, "grep", `#!/bin/bash
/usr/bin/grep "$@"
`)
	writeFake(t, bin, "mktemp", fmt.Sprintf(`#!/bin/bash
f=%s/tmp.$$
: > "$f"
echo "$f"
`, shellQuote(work)))
	writeFake(t, bin, "sleep", `#!/bin/bash
exit 0
`)

	cmd := exec.Command("bash", "-c", hostsSetupScript)
	cmd.Env = []string{"PATH=" + bin + ":/usr/bin:/bin", "HOME=" + work}
	cmd.Stdin = strings.NewReader("10.0.0.5 proxy.example\n")
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("expected lookup miss failure: %s", out)
	} else if !strings.Contains(string(out), "did not return") {
		t.Fatalf("want lookup miss message: %s", out)
	}
}
