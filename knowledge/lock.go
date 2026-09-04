package knowledge

import "sync"

// fileLocks manages per-file mutexes for safe concurrent YAML access.
type fileLocks struct {
	mu    sync.Mutex
	locks map[string]*sync.RWMutex
}

// NewFileLocks creates a new file lock manager.
func NewFileLocks() *fileLocks {
	return &fileLocks{
		locks: make(map[string]*sync.RWMutex),
	}
}

// Get returns the RWMutex for the given file path, creating it if needed.
func (fl *fileLocks) Get(filePath string) *sync.RWMutex {
	fl.mu.Lock()
	defer fl.mu.Unlock()

	m, ok := fl.locks[filePath]
	if !ok {
		m = &sync.RWMutex{}
		fl.locks[filePath] = m
	}
	return m
}
