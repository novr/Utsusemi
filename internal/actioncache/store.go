package actioncache

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/novr/utsusemi/internal/instancelock"
)

const (
	metaName     = "meta.json"
	blobName     = "archive.bin"
	blocksDir    = "blocks"
	pendingName  = "pending"
	finalName    = "final"
	storeLockRel = "store.lock"
	// pendingTTL: abandoned Create (upload never finalized) must not lock the key forever.
	pendingTTL = 15 * time.Minute
)

type entryMeta struct {
	Repo      string    `json:"repo"`
	Key       string    `json:"key"`
	Version   string    `json:"version"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
	UsedAt    time.Time `json:"used_at"`
	Final     bool      `json:"final"`
}

// Store is a host-directory cache of Actions cache archives.
type Store struct {
	root string
}

func NewStore(root string) (*Store, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("store root is empty")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &Store{root: root}, nil
}

func (s *Store) Root() string { return s.root }

func entryID(repo, key, version string) string {
	sum := sha256.Sum256([]byte(repo + "\x00" + version + "\x00" + key))
	return hex.EncodeToString(sum[:])
}

func (s *Store) entryDir(id string) string {
	return filepath.Join(s.root, id)
}

func (s *Store) withStoreLock(fn func() error) error {
	lock, err := instancelock.AcquireBlocking(filepath.Join(s.root, storeLockRel))
	if err != nil {
		return err
	}
	defer lock.Release()
	return fn()
}

// Reserve creates a pending entry. ok=false means key+version already exists
// as final, or as a fresh pending upload. Stale pending entries (older than
// pendingTTL) are reclaimed so a failed Azure upload cannot lock the key forever.
func (s *Store) Reserve(repo, key, version string) (id string, ok bool, err error) {
	if key == "" || version == "" {
		return "", false, fmt.Errorf("key and version are required")
	}
	repo = normalizeRepo(repo)
	id = entryID(repo, key, version)
	err = s.withStoreLock(func() error {
		dir := s.entryDir(id)
		metaPath := filepath.Join(dir, metaName)
		if existing, loadErr := loadMeta(metaPath); loadErr == nil && existing != nil {
			if existing.Final {
				ok = false
				return nil
			}
			age := time.Since(existing.CreatedAt)
			if age >= 0 && age < pendingTTL {
				ok = false
				return nil
			}
			if rmErr := os.RemoveAll(dir); rmErr != nil {
				return rmErr
			}
		}
		if mkErr := os.MkdirAll(filepath.Join(dir, blocksDir), 0o755); mkErr != nil {
			return mkErr
		}
		now := time.Now().UTC()
		meta := entryMeta{
			Repo:      repo,
			Key:       key,
			Version:   version,
			Size:      -1,
			CreatedAt: now,
			UsedAt:    now,
			Final:     false,
		}
		if writeErr := writeMeta(metaPath, meta); writeErr != nil {
			_ = os.RemoveAll(dir)
			return writeErr
		}
		_ = os.WriteFile(filepath.Join(dir, pendingName), []byte("1"), 0o644)
		ok = true
		return nil
	})
	return id, ok, err
}

func (s *Store) PutBlock(id, blockID string, data []byte) error {
	return s.PutBlockFrom(id, blockID, bytes.NewReader(data))
}

func (s *Store) PutSingle(id string, data []byte) error {
	return s.PutSingleFrom(id, bytes.NewReader(data))
}

func (s *Store) PutBlockFrom(id, blockID string, r io.Reader) error {
	dir := s.entryDir(id)
	safe := sanitizeBlockID(blockID)
	dest := filepath.Join(dir, blocksDir, safe)
	tmp, err := s.beginPendingWrite(id, dir)
	if err != nil {
		return err
	}
	if err := writeReaderFile(tmp, r); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return s.finishPendingWrite(id, tmp, dest)
}

func (s *Store) PutSingleFrom(id string, r io.Reader) error {
	dir := s.entryDir(id)
	dest := filepath.Join(dir, blobName)
	tmp, err := s.beginPendingWrite(id, dir)
	if err != nil {
		return err
	}
	if err := writeReaderFile(tmp, r); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return s.finishPendingWrite(id, tmp, dest)
}

// beginPendingWrite checks the entry is writable under the store lock, then
// returns a temp path. The caller copies bytes without holding the lock.
func (s *Store) beginPendingWrite(id, dir string) (tmp string, err error) {
	err = s.withStoreLock(func() error {
		meta, loadErr := loadMeta(filepath.Join(dir, metaName))
		if loadErr != nil {
			return loadErr
		}
		if meta.Final {
			return fmt.Errorf("entry already finalized")
		}
		if mkErr := os.MkdirAll(filepath.Join(dir, blocksDir), 0o755); mkErr != nil {
			return mkErr
		}
		f, createErr := os.CreateTemp(dir, id+".*.tmp")
		if createErr != nil {
			return createErr
		}
		tmp = f.Name()
		return f.Close()
	})
	return tmp, err
}

func (s *Store) finishPendingWrite(id, tmp, dest string) error {
	return s.withStoreLock(func() error {
		defer os.Remove(tmp)
		meta, err := loadMeta(filepath.Join(s.entryDir(id), metaName))
		if err != nil {
			return err
		}
		if meta.Final {
			return fmt.Errorf("entry already finalized")
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		return os.Rename(tmp, dest)
	})
}

func writeReaderFile(path string, r io.Reader) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, r)
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(path)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return closeErr
	}
	return nil
}

func (s *Store) CommitBlocks(id string, blockIDs []string) error {
	dir := s.entryDir(id)
	var blockPaths []string
	if err := s.withStoreLock(func() error {
		meta, err := loadMeta(filepath.Join(dir, metaName))
		if err != nil {
			return err
		}
		if meta.Final {
			return fmt.Errorf("entry already finalized")
		}
		blockPaths = make([]string, 0, len(blockIDs))
		for _, bid := range blockIDs {
			blockPaths = append(blockPaths, filepath.Join(dir, blocksDir, sanitizeBlockID(bid)))
		}
		return nil
	}); err != nil {
		return err
	}

	tmp, err := s.beginPendingWrite(id, dir)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	for _, path := range blockPaths {
		in, openErr := os.Open(path)
		if openErr != nil {
			_ = f.Close()
			_ = os.Remove(tmp)
			return openErr
		}
		_, copyErr := io.Copy(f, in)
		_ = in.Close()
		if copyErr != nil {
			_ = f.Close()
			_ = os.Remove(tmp)
			return copyErr
		}
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}

	outPath := filepath.Join(dir, blobName)
	return s.withStoreLock(func() error {
		defer os.Remove(tmp)
		meta, err := loadMeta(filepath.Join(dir, metaName))
		if err != nil {
			return err
		}
		if meta.Final {
			return fmt.Errorf("entry already finalized")
		}
		if err := os.Rename(tmp, outPath); err != nil {
			return err
		}
		_ = os.RemoveAll(filepath.Join(dir, blocksDir))
		_ = os.MkdirAll(filepath.Join(dir, blocksDir), 0o755)
		return nil
	})
}

// Finalize marks the entry readable. sizeBytes must match archive size when >0.
func (s *Store) Finalize(repo, key, version string, sizeBytes int64) (entryIDOut string, ok bool, err error) {
	repo = normalizeRepo(repo)
	id := entryID(repo, key, version)
	err = s.withStoreLock(func() error {
		dir := s.entryDir(id)
		metaPath := filepath.Join(dir, metaName)
		meta, loadErr := loadMeta(metaPath)
		if loadErr != nil {
			ok = false
			return nil
		}
		if meta.Final {
			ok = false
			return nil
		}
		st, statErr := os.Stat(filepath.Join(dir, blobName))
		if statErr != nil {
			return fmt.Errorf("archive missing: %w", statErr)
		}
		if sizeBytes > 0 && st.Size() != sizeBytes {
			return fmt.Errorf("size mismatch: got %d want %d", st.Size(), sizeBytes)
		}
		meta.Size = st.Size()
		meta.Final = true
		meta.UsedAt = time.Now().UTC()
		if writeErr := writeMeta(metaPath, *meta); writeErr != nil {
			return writeErr
		}
		_ = os.WriteFile(filepath.Join(dir, finalName), []byte("1"), 0o644)
		_ = os.Remove(filepath.Join(dir, pendingName))
		ok = true
		entryIDOut = id
		return nil
	})
	return entryIDOut, ok, err
}

type hit struct {
	ID    string
	Key   string
	Path  string
	Size  int64
	UsedAt time.Time
}

// Lookup finds an exact key then restore-key prefixes for the same version.
func (s *Store) Lookup(repo, primary string, restoreKeys []string, version string) (*hit, error) {
	repo = normalizeRepo(repo)
	keys := make([]string, 0, 1+len(restoreKeys))
	keys = append(keys, primary)
	keys = append(keys, restoreKeys...)

	var best *hit
	err := s.withStoreLock(func() error {
		for i, want := range keys {
			if want == "" {
				continue
			}
			exact := i == 0
			matches, listErr := s.listFinal(repo, version)
			if listErr != nil {
				return listErr
			}
			var candidates []hit
			for _, m := range matches {
				if exact {
					if m.Key == want {
						candidates = append(candidates, m)
					}
					continue
				}
				if strings.HasPrefix(m.Key, want) {
					candidates = append(candidates, m)
				}
			}
			if len(candidates) == 0 {
				continue
			}
			sort.Slice(candidates, func(i, j int) bool {
				return candidates[i].UsedAt.After(candidates[j].UsedAt)
			})
			h := candidates[0]
			h.UsedAt = time.Now().UTC()
			metaPath := filepath.Join(s.entryDir(h.ID), metaName)
			if meta, loadErr := loadMeta(metaPath); loadErr == nil && meta != nil {
				meta.UsedAt = h.UsedAt
				_ = writeMeta(metaPath, *meta)
			}
			best = &h
			return nil
		}
		return nil
	})
	return best, err
}

func (s *Store) ArchivePath(id string) string {
	return filepath.Join(s.entryDir(id), blobName)
}

func (s *Store) listFinal(repo, version string) ([]hit, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, err
	}
	var out []hit
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		meta, loadErr := loadMeta(filepath.Join(s.entryDir(e.Name()), metaName))
		if loadErr != nil || meta == nil || !meta.Final {
			continue
		}
		if meta.Repo != repo || meta.Version != version {
			continue
		}
		out = append(out, hit{
			ID:     e.Name(),
			Key:    meta.Key,
			Path:   s.ArchivePath(e.Name()),
			Size:   meta.Size,
			UsedAt: meta.UsedAt,
		})
	}
	return out, nil
}

// EvictLRU deletes oldest-used final entries until freeBytesFn reports at least
// needFreeGB free, or the store is empty. Stale pending entries (abandoned
// uploads older than pendingTTL) are always removed first.
func (s *Store) EvictLRU(needFreeGB int, freeBytesFn func() (float64, error)) error {
	if needFreeGB <= 0 || freeBytesFn == nil {
		return nil
	}
	return s.withStoreLock(func() error {
		if err := s.purgeStalePendingLocked(); err != nil {
			return err
		}
		for {
			freeGB, err := freeBytesFn()
			if err != nil {
				return err
			}
			if freeGB >= float64(needFreeGB) {
				return nil
			}
			victim, ok, err := s.oldestFinalLocked()
			if err != nil {
				return err
			}
			if !ok {
				return nil
			}
			if err := os.RemoveAll(s.entryDir(victim)); err != nil {
				return err
			}
		}
	})
}

func (s *Store) purgeStalePendingLocked() error {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return err
	}
	now := time.Now()
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		meta, loadErr := loadMeta(filepath.Join(s.entryDir(e.Name()), metaName))
		if loadErr != nil || meta == nil || meta.Final {
			continue
		}
		age := now.Sub(meta.CreatedAt)
		if age < pendingTTL {
			continue
		}
		if rmErr := os.RemoveAll(s.entryDir(e.Name())); rmErr != nil {
			return rmErr
		}
	}
	return nil
}

func (s *Store) oldestFinalLocked() (id string, ok bool, err error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return "", false, err
	}
	var bestID string
	var bestTime time.Time
	found := false
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		meta, loadErr := loadMeta(filepath.Join(s.entryDir(e.Name()), metaName))
		if loadErr != nil || meta == nil || !meta.Final {
			continue
		}
		if !found || meta.UsedAt.Before(bestTime) {
			found = true
			bestID = e.Name()
			bestTime = meta.UsedAt
		}
	}
	return bestID, found, nil
}

func normalizeRepo(repo string) string {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return "default"
	}
	return repo
}

func sanitizeBlockID(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}

func loadMeta(path string) (*entryMeta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var meta entryMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

func writeMeta(path string, meta entryMeta) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
