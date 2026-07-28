package status

import (
	"fmt"
	"strings"

	"github.com/novr/utsusemi/internal/credentialview"
)

func FormatText(r Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "target: %s\n", r.Target)
	fmt.Fprintf(&b, "agent: %s", r.Agent.State)
	if r.Agent.PID > 0 {
		fmt.Fprintf(&b, " (pid %d", r.Agent.PID)
		if r.Agent.State == AgentStale {
			b.WriteString(" dead")
		} else if r.Agent.Uptime != "" {
			fmt.Fprintf(&b, ", uptime %s", r.Agent.Uptime)
		}
		b.WriteString(")")
	}
	b.WriteByte('\n')
	if r.Agent.Warning != "" {
		fmt.Fprintf(&b, "warning: %s\n", r.Agent.Warning)
	}

	staleCount := 0
	for _, job := range r.Jobs {
		if job.Stale {
			staleCount++
		}
	}
	switch {
	case len(r.Jobs) == 0:
		b.WriteString("jobs: 0\n")
	case staleCount > 0:
		fmt.Fprintf(&b, "jobs: %d (%d stale)\n", len(r.Jobs), staleCount)
	default:
		fmt.Fprintf(&b, "jobs: %d\n", len(r.Jobs))
	}
	for _, job := range r.Jobs {
		line := fmt.Sprintf("  %s runner=%d (%s)", job.VMName, job.RunnerID, job.Age)
		if job.Stale {
			if job.AgentID != "" {
				line += fmt.Sprintf(", agent=%s", job.AgentID)
			}
			line += " (stale)"
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}

	fmt.Fprintf(&b, "vms: %d running, %d total (pool_size %d)\n", r.VMs.Running, r.VMs.Total, r.VMs.PoolSize)
	fmt.Fprintf(&b, "warming: %d", len(r.Warming))
	if len(r.Warming) > 0 {
		fmt.Fprintf(&b, " (%s)", strings.Join(r.Warming, ", "))
	}
	b.WriteByte('\n')

	fmt.Fprintf(&b, "disk: %.1f GB free — %s\n", r.Health.FreeDiskGB, r.Health.Status)
	b.WriteString(credentialview.StatusLine(r.Credential))
	return b.String()
}
