package filesystem

import "sync"

// keyLockRegistry coordinates operations for one storage key without making
// unrelated media wait for each other.
type keyLockRegistry struct {
	mutex sync.Mutex
	locks map[string]*referencedKeyLock
}

type referencedKeyLock struct {
	mutex      sync.RWMutex
	references int
}

func newKeyLockRegistry() *keyLockRegistry {
	return &keyLockRegistry{locks: make(map[string]*referencedKeyLock)}
}

func (registry *keyLockRegistry) acquireRead(key string) func() {
	keyLock := registry.reference(key)
	keyLock.mutex.RLock()

	return func() {
		keyLock.mutex.RUnlock()
		registry.unreference(key, keyLock)
	}
}

func (registry *keyLockRegistry) acquireWrite(key string) func() {
	keyLock := registry.reference(key)
	keyLock.mutex.Lock()

	return func() {
		keyLock.mutex.Unlock()
		registry.unreference(key, keyLock)
	}
}

func (registry *keyLockRegistry) reference(key string) *referencedKeyLock {
	registry.mutex.Lock()
	defer registry.mutex.Unlock()

	keyLock := registry.locks[key]
	if keyLock == nil {
		keyLock = &referencedKeyLock{}
		registry.locks[key] = keyLock
	}
	keyLock.references++

	return keyLock
}

func (registry *keyLockRegistry) unreference(
	key string,
	keyLock *referencedKeyLock,
) {
	registry.mutex.Lock()
	defer registry.mutex.Unlock()

	keyLock.references--
	if keyLock.references == 0 {
		delete(registry.locks, key)
	}
}
