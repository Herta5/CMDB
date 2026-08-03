package collector

import (
	"fmt"
	"sync"
)

var (
	registry   = make(map[string]Collector)
	registryMu sync.RWMutex
)

// Register adds a collector to the global registry. Call from init().
func Register(name string, c Collector) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("collector %q already registered", name))
	}
	registry[name] = c
}

// Get retrieves a collector by name.
func Get(name string) (Collector, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	c, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("collector %q not found", name)
	}
	return c, nil
}

// List returns all registered collector names.
func List() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}