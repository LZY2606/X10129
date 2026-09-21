// Copyright 2026 The Bazel Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package starlark

import "testing"

func TestIterStateLeaseLifecycle(t *testing.T) {
	var s iterState // zero value: mutable, no active iterator

	if err := s.checkMutable("list", "append to"); err != nil {
		t.Fatalf("fresh state not mutable: %v", err)
	}

	// An empty container still establishes a real lease: even with no
	// elements, begin increments the active count and gates mutation.
	a := s.begin()
	if err := s.checkMutable("list", "append to"); err == nil {
		t.Fatalf("mutation with one active lease was not rejected")
	} else if err.Error() != "cannot append to list during iteration" {
		t.Fatalf("got %q", err.Error())
	}

	// Nested leases nest: releasing the outer one does not free the state.
	b := s.begin()
	a.release()
	if err := s.checkMutable("hash table", "insert into"); err == nil {
		t.Fatalf("mutation with one remaining lease was not rejected")
	}
	b.release()
	if err := s.checkMutable("hash table", "insert into"); err != nil {
		t.Fatalf("mutation after all leases released: %v", err)
	}

	// Exhaustion is not release: a caller who never calls Done keeps the
	// lease (covered here by simply never releasing a token).
	held := s.begin()
	if err := s.checkMutable("list", "clear"); err == nil {
		t.Fatalf("unreleased lease did not gate mutation")
	}

	// Freeze while a lease is live follows the existing contract: the
	// state becomes immutable and releases become inert/idempotent.
	s.freeze()
	if err := s.checkMutable("list", "clear"); err == nil {
		t.Fatalf("frozen state was mutable")
	} else if err.Error() != "cannot clear frozen list" {
		t.Fatalf("got %q", err.Error())
	}
	held.release()
	held.release() // idempotent across freeze

	// begin after freeze returns an inert lease; releasing it is a no-op
	// and must never underflow the (possibly stale) active count.
	inert := s.begin()
	inert.release()
	inert.release()
}

func TestIterStateLeaseIdempotent(t *testing.T) {
	var s iterState
	lease := s.begin()
	lease.release()
	for i := 0; i < 3; i++ {
		lease.release() // repeat calls must be no-ops
	}
	// A zero-value lease must be releasable too (defensive error paths).
	var zero iterLease
	zero.release()
	if s.active != 0 {
		t.Fatalf("active = %d, want 0", s.active)
	}
	if err := s.checkMutable("list", "append to"); err != nil {
		t.Fatalf("state not mutable after releases: %v", err)
	}
}
