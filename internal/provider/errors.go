package provider

import "strings"

// IsBenignMissing reports whether err indicates the VM (or path) is already gone.
// Tart stop/delete messages are matched by substring; used so teardown/reclaim
// do not treat an absent VM as a hard failure.
func IsBenignMissing(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "does not exist") ||
		strings.Contains(msg, "not found") ||
		strings.Contains(msg, "no such file")
}
