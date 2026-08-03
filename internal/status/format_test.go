package status

import (
	"strings"
	"testing"

	"github.com/novr/utsusemi/internal/credentialview"
)

func TestFormatTextRunning(t *testing.T) {
	out := FormatText(Report{
		Target: "org:my-org (group 1)",
		Agent: AgentInfo{
			State:  AgentRunning,
			PID:    12345,
			Uptime: "2h15m",
		},
		Jobs: []Job{
			{VMName: "utsusemi-a1b2", RunnerID: 42, Age: "12m"},
		},
		VMs:      VMsInfo{Running: 1, Total: 1, PoolSize: 4},
		Draining: nil,
		Health:   HealthInfo{FreeDiskGB: 42.1, Status: "ok"},
		Credential: credentialview.Info{
			Mode:       "hosted_app",
			Present:    true,
			GitHubUser: "octocat",
			ExpiresIn:  "18h0m",
		},
	})

	for _, want := range []string{
		"target: org:my-org (group 1)",
		"agent: running (pid 12345, uptime 2h15m)",
		"jobs: 1",
		"utsusemi-a1b2 runner=42",
		"draining: 0",
		"disk: 42.1 GB free — ok",
		"credential: user octocat",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "warming:") {
		t.Fatalf("output still uses warming label:\n%s", out)
	}
}

func TestFormatTextDrainingVMs(t *testing.T) {
	out := FormatText(Report{
		Target:   "org:my-org (group 1)",
		Agent:    AgentInfo{State: AgentRunning, PID: 1},
		VMs:      VMsInfo{Running: 2, Total: 2, PoolSize: 1},
		Draining: []string{"utsusemi-drain-a", "utsusemi-drain-b"},
		Health:   HealthInfo{FreeDiskGB: 42.1, Status: "ok"},
		Credential: credentialview.Info{
			Mode:    "github_pat",
			Present: true,
		},
	})
	want := "draining: 2 (utsusemi-drain-a, utsusemi-drain-b)"
	if !strings.Contains(out, want) {
		t.Fatalf("output missing %q:\n%s", want, out)
	}
}

func TestFormatTextStaleJobs(t *testing.T) {
	out := FormatText(Report{
		Target: "org:my-org (group 1)",
		Agent:  AgentInfo{State: AgentStale, PID: 12345},
		Jobs: []Job{
			{VMName: "utsusemi-old", RunnerID: 99, Age: "2h", Stale: true, AgentID: "deadbeef"},
		},
		VMs:    VMsInfo{Running: 1, Total: 1, PoolSize: 4},
		Health: HealthInfo{FreeDiskGB: 42.1, Status: "ok"},
		Credential: credentialview.Info{
			Mode:    "github_pat",
			Present: true,
		},
	})

	if !strings.Contains(out, "jobs: 1 (1 stale)") {
		t.Fatalf("unexpected output:\n%s", out)
	}
	if !strings.Contains(out, "agent: stale (pid 12345 dead)") {
		t.Fatalf("unexpected output:\n%s", out)
	}
	if !strings.Contains(out, "(stale)") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestFormatTextStoppedWarning(t *testing.T) {
	out := FormatText(Report{
		Target: "org:my-org (group 1)",
		Agent: AgentInfo{
			State:   AgentStopped,
			PID:     12345,
			Warning: "agent.json present but utsusemi is not running",
		},
		Jobs:       []Job{},
		Draining:   []string{},
		Health:     HealthInfo{FreeDiskGB: 42.1, Status: "ok"},
		Credential: credentialview.Info{Mode: "github_pat"},
	})
	if !strings.Contains(out, "warning: agent.json present but utsusemi is not running") {
		t.Fatalf("unexpected output:\n%s", out)
	}
	if !strings.Contains(out, "draining: 0") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestFormatTextMounts(t *testing.T) {
	out := FormatText(Report{
		Target: "repo:owner/app",
		Agent:  AgentInfo{State: AgentStopped},
		Mounts: []string{"/Users/me/cache", "/Users/me/toolchains:ro"},
		Health: HealthInfo{FreeDiskGB: 42.1, Status: "ok"},
		Credential: credentialview.Info{
			Mode:    "github_pat",
			Present: true,
		},
	})
	want := "mounts: /Users/me/cache, /Users/me/toolchains:ro"
	if !strings.Contains(out, want) {
		t.Fatalf("output missing %q:\n%s", want, out)
	}
}
