package services

import "sync"

// keyedMutex сериализует работу по ключу и очищает неиспользуемые блокировки.
type keyedMutex struct {
	mu    sync.Mutex
	locks map[int64]*keyedMutexEntry
}

type keyedMutexEntry struct {
	mu   sync.Mutex
	refs int
}

func (m *keyedMutex) Lock(key int64) func() {
	if key <= 0 {
		return func() {}
	}

	entry := m.acquire(key)
	entry.mu.Lock()

	return func() {
		m.release(key, entry)
	}
}

func (m *keyedMutex) acquire(key int64) *keyedMutexEntry {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.locks == nil {
		m.locks = make(map[int64]*keyedMutexEntry)
	}

	entry := m.locks[key]
	if entry == nil {
		entry = &keyedMutexEntry{}
		m.locks[key] = entry
	}
	entry.refs++

	return entry
}

func (m *keyedMutex) release(key int64, entry *keyedMutexEntry) {
	if entry == nil {
		return
	}

	entry.mu.Unlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	current := m.locks[key]
	if current != entry {
		return
	}

	entry.refs--
	if entry.refs == 0 {
		delete(m.locks, key)
	}
}

func (m *keyedMutex) len() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.locks)
}
