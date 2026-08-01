package core

import "github.com/haulmatic-dev/hydra/internal/registry"

// Registry exposes the daemon-owned worker registry. Nil when the registry
// could not be opened (fail-open mode); callers must tolerate nil.
func (o *Overseer) Registry() *registry.Store {
	return o.registryStore
}
