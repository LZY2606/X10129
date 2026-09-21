// Copyright 2026 The Bazel Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package starlark

import "fmt"

// This file defines iterState, the shared "iteration lease" machinery of the
// mutable container types: List and the hashtable underlying Dict and Set.
//
// An iterState tracks two pieces of information for exactly one container:
//
//   - active, the number of leases currently held by live iterators; and
//   - frozen, whether the container has been frozen.
//
// Each call to begin hands out one lease and returns an iterLease token
// whose release must run once; release is idempotent, so a deferred call
// is safe even after an explicit release. A lease stays live after the
// iterator is exhausted: the caller may retain the iterator after the last
// element, so exhaustion must not release the lease. Releasing is solely
// the responsibility of Iterator.Done (or the end of a go1.23 range loop,
// which pairs its begin with a deferred release).
//
// A frozen container hands out inert leases: begin and release do not touch
// active. Freezing makes a container permanently immutable, so its iterator
// count no longer gates any mutation, and it is safe for an iterator created
// before a freeze to release its lease after the freeze.

// iterState is the shared mutable state of a container that supports
// iteration: its active-iterator count and its frozen bit.
//
// The zero value describes a mutable container with no active iterators.
// An iterState must not be copied once its container has been exposed.
type iterState struct {
	frozen bool
	active uint32 // number of active iterators (ignored once frozen)
}

// begin acquires a single iteration lease on the container.
// The caller must ensure the lease's release runs exactly once,
// typically by embedding it in an Iterator or deferring its release.
func (s *iterState) begin() iterLease {
	if !s.frozen {
		s.active++
	}
	return iterLease{s: s}
}

// iterLease is the token returned by iterState.begin.
//
// It is embedded by the concrete Iterator implementations so that an
// iterator literally owns the lease it was granted: constructing the
// iterator is the acquisition, and Iterator.Done is the single release.
type iterLease struct {
	s *iterState // nil once the lease has been released
}

// release returns the lease to its container. It is idempotent, so an
// explicit Done followed by a deferred one (or vice versa) releases only
// once, and a zero-value lease may be released safely in error paths.
func (lease *iterLease) release() {
	s := lease.s
	if s == nil {
		return // already released, or never granted (zero value)
	}
	lease.s = nil
	if !s.frozen {
		s.active--
	}
}

// freeze marks the container frozen, making all future leases inert.
// Existing leases remain held until their owner calls Done, but a frozen
// container rejects every mutation regardless of its active count.
func (s *iterState) freeze() { s.frozen = true }

// isFrozen reports whether the container has been frozen.
func (s *iterState) isFrozen() bool { return s.frozen }

// checkMutable reports an error if the container must not be modified.
// kind is "list" or "hash table"; the messages are unchanged:
//
//	fmt.Errorf("cannot %s frozen %s", verb, kind)
//	fmt.Errorf("cannot %s %s during iteration", verb, kind)
func (s *iterState) checkMutable(kind, verb string) error {
	if s.frozen {
		return fmt.Errorf("cannot %s frozen %s", verb, kind)
	}
	if s.active > 0 {
		return fmt.Errorf("cannot %s %s during iteration", verb, kind)
	}
	return nil
}
