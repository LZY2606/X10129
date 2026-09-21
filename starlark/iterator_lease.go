package starlark

import "fmt"

// mutableContainer is the common mutable state shared by a container and all
// of its iterators.
type mutableContainer struct {
	frozen          bool
	activeIterators uint32
}

func (m *mutableContainer) freeze() {
	m.frozen = true
}

func (m *mutableContainer) checkMutable(verb, kind string) error {
	if m.frozen {
		return fmt.Errorf("cannot %s frozen %s", verb, kind)
	}
	if m.activeIterators > 0 {
		return fmt.Errorf("cannot %s %s during iteration", verb, kind)
	}
	return nil
}

// iterationLease records one active iterator's ownership of one mutable
// container. A frozen container does not require a lease.
type iterationLease struct {
	container *mutableContainer
}

func (m *mutableContainer) lease() iterationLease {
	if m.frozen {
		return iterationLease{}
	}
	m.activeIterators++
	return iterationLease{container: m}
}

// release releases the lease exactly once. It is safe to call repeatedly.
func (lease *iterationLease) release() {
	if lease.container != nil {
		lease.container.activeIterators--
		lease.container = nil
	}
}
