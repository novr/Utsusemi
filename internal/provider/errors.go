package provider

import (
	"errors"
	"strings"
)

// ErrNotRunning means the VM is listed as not running (guest agent not yet relevant).
var ErrNotRunning = errors.New("vm is not running")

// IsBenignMissing reports whether err indicates the VM (or path) is already gone.
// Tart stop/delete messages are matched by substring; used so teardown/reclaim
// do not treat an absent VM as a hard failure.
func IsBenignMissing(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	// Prefer Tart's "does not exist"; keep "no such file" for path-level races.
	// Avoid bare "not found" — too broad for unrelated failures.
	return strings.Contains(msg, "does not exist") ||
		strings.Contains(msg, "no such file")
}
