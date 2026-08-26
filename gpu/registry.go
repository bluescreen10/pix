package gpu

import (
	"fmt"
	"math"
	"sync"
)

type registeredBackend struct {
	instance Backend
	name     string
	priority int
}

var (
	backends   []registeredBackend
	backendsMu sync.Mutex
)

func RegisterBackend(instance Backend, name string, priority int) {
	backendsMu.Lock()
	defer backendsMu.Unlock()
	backends = append(backends, registeredBackend{
		instance: instance,
		priority: priority,
	})
}

func Instance(name *string) Backend {
	backendsMu.Lock()
	defer backendsMu.Unlock()

	// a specific backend was requested
	if name != nil && *name != "" {
		for _, b := range backends {
			if b.name == *name {
				return b.instance
			}
		}

		panic(fmt.Sprintf("there are no registered backends with '%s'", *name))
	}

	var instance Backend
	var maxPriority = math.MinInt

	for _, b := range backends {
		if b.priority > maxPriority {
			maxPriority = b.priority
			instance = b.instance
		}
	}

	if instance == nil {
		panic("there are no registered backends")
	}

	return instance
}
