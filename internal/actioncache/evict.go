package actioncache

import (
	"fmt"
	"strings"
)

// FreeDiskGBFunc reports free disk in GB (same units as provider.FreeDiskGB).
type FreeDiskGBFunc func() (float64, error)

// EvictForMinFree prepares the store and deletes LRU final entries until
// freeGB >= minFreeDiskGB. No-op when minFreeDiskGB <= 0.
func EvictForMinFree(stateDir string, minFreeDiskGB int, free FreeDiskGBFunc) error {
	if minFreeDiskGB <= 0 || free == nil {
		return nil
	}
	if strings.TrimSpace(stateDir) == "" {
		return fmt.Errorf("state dir is empty")
	}
	if err := PrepareDir(stateDir); err != nil {
		return err
	}
	store, err := NewStore(Dir(stateDir))
	if err != nil {
		return err
	}
	return store.EvictLRU(minFreeDiskGB, free)
}
