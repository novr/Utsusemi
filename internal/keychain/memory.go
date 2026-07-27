package keychain

import "sync"

type MemoryStore struct {
	mu    sync.Mutex
	items map[string]string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{items: make(map[string]string)}
}

func memoryKey(service, account string) string {
	return service + "\x00" + account
}

func (m *MemoryStore) Get(service, account string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, ok := m.items[memoryKey(service, account)]
	if !ok {
		return "", ErrNotFound
	}
	return value, nil
}

func (m *MemoryStore) Set(service, account, secret string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[memoryKey(service, account)] = secret
	return nil
}

func (m *MemoryStore) Delete(service, account string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := memoryKey(service, account)
	if _, ok := m.items[key]; !ok {
		return ErrNotFound
	}
	delete(m.items, key)
	return nil
}
