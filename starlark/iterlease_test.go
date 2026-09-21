// Copyright 2026 The Bazel Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package starlark_test

// Shared behavior tests of the iteration-lease mechanism used by the three
// mutable container types: List and the hashtable-based Dict and Set.
//
// Each container is exercised through one leaseCase adapting its small API
// differences, so every scenario below runs against all three types.
// See iterlease.go (package starlark) for the abstraction itself.

import (
	"strings"
	"testing"

	. "go.starlark.net/starlark"
)

// leaseCase adapts one mutable container type to the shared scenarios.
type leaseCase struct {
	name string
	// mutate performs a mutation that would disturb a live traversal
	// (append / insert a brand-new key / insert a brand-new element).
	mutate func(c leaseContainer) error
	// mutateFrozen is mutate as attempted after Freeze, and its exact
	// legacy error text.
	frozenErr string
	// iterErr is the exact legacy text when mutate runs during iteration.
	iterErr string
	empty   func() leaseContainer
	non     func() leaseContainer
	// deleteFirst removes the first element the container yields;
	// nil for containers without a direct delete in the shared suite.
	deleteFirst func(c leaseContainer) (bool, error)
	// reinsert restores the element removed by deleteFirst.
	reinsert func(c leaseContainer) error
}

// leaseContainer is the subset of the container API used by the tests.
type leaseContainer interface {
	Iterate() Iterator
	Len() int
	Freeze()
}

// drain reads every value of it and returns the values yielded.
func drain(it Iterator) []string {
	var got []string
	var x Value
	for it.Next(&x) {
		got = append(got, x.String())
	}
	return got
}

var leaseCases = []leaseCase{
	{
		name:      "list",
		frozenErr: "cannot append to frozen list",
		iterErr:   "cannot append to list during iteration",
		empty:     func() leaseContainer { return NewList(nil) },
		non: func() leaseContainer {
			return NewList([]Value{MakeInt(1), MakeInt(2), MakeInt(3)})
		},
		mutate: func(c leaseContainer) error {
			return c.(*List).Append(MakeInt(4))
		},
	},
	{
		name:      "dict",
		frozenErr: "cannot insert into frozen hash table",
		iterErr:   "cannot insert into hash table during iteration",
		empty:     func() leaseContainer { return NewDict(0) },
		non: func() leaseContainer {
			d := NewDict(3)
			d.SetKey(MakeInt(1), MakeInt(10))
			d.SetKey(MakeInt(2), MakeInt(20))
			d.SetKey(MakeInt(3), MakeInt(30))
			return d
		},
		mutate: func(c leaseContainer) error {
			return c.(*Dict).SetKey(MakeInt(4), MakeInt(40))
		},
		deleteFirst: func(c leaseContainer) (bool, error) {
			_, found, err := c.(*Dict).Delete(MakeInt(1))
			return found, err
		},
		reinsert: func(c leaseContainer) error {
			return c.(*Dict).SetKey(MakeInt(1), MakeInt(10))
		},
	},
	{
		name:      "set",
		frozenErr: "cannot insert into frozen hash table",
		iterErr:   "cannot insert into hash table during iteration",
		empty:     func() leaseContainer { return NewSet(0) },
		non: func() leaseContainer {
			s := NewSet(3)
			s.Insert(MakeInt(1))
			s.Insert(MakeInt(2))
			s.Insert(MakeInt(3))
			return s
		},
		mutate: func(c leaseContainer) error {
			return c.(*Set).Insert(MakeInt(4))
		},
		deleteFirst: func(c leaseContainer) (bool, error) {
			return c.(*Set).Delete(MakeInt(1))
		},
		reinsert: func(c leaseContainer) error {
			return c.(*Set).Insert(MakeInt(1))
		},
	},
}

// TestIterationLease runs the full matrix of shared lease scenarios for
// List, Dict, and Set.
func TestIterationLease(t *testing.T) {
	for _, tc := range leaseCases {
		t.Run(tc.name, func(t *testing.T) {
			// Empty and non-empty containers both establish a real lease.
			for _, mk := range []struct {
				label string
				mk    func() leaseContainer
				want  []string
			}{
				{"empty", tc.empty, nil},
				{"nonempty", tc.non, []string{"1", "2", "3"}},
			} {
				t.Run(mk.label, func(t *testing.T) {
					c := mk.mk()

					// A complete traversal releases the lease, so mutation
					// works again afterwards (exhaustion is not Done, but
					// the caller did call Done).
					it := c.Iterate()
					if got := drain(it); strings.Join(got, ",") != strings.Join(mk.want, ",") {
						t.Fatalf("traversal = %v, want %v", got, mk.want)
					}
					if it.Next(new(Value)) {
						t.Fatalf("Next after exhaustion returned true")
					}
					it.Done()
					if err := tc.mutate(c); err != nil {
						t.Fatalf("mutation after Done failed: %v", err)
					}

					// Two nested iterators: neither release alone frees the
					// container; mutation is rejected until both are done.
					outer := c.Iterate()
					inner := c.Iterate()
					var x Value
					outer.Next(&x)
					inner.Next(&x)
					if err := tc.mutate(c); err == nil || err.Error() != tc.iterErr {
						t.Fatalf("mutate with 2 active iterators: got %v, want %q", err, tc.iterErr)
					}
					outer.Done()
					if err := tc.mutate(c); err == nil || err.Error() != tc.iterErr {
						t.Fatalf("mutate with 1 remaining iterator: got %v, want %q", err, tc.iterErr)
					}
					inner.Done()
					if err := tc.mutate(c); err != nil {
						t.Fatalf("mutation after both Done failed: %v", err)
					}

					// One iterator released before exhaustion (early Done):
					// its lease is gone immediately, even mid-traversal.
					early := c.Iterate()
					early.Next(&x)
					early.Done()
					if err := tc.mutate(c); err != nil {
						t.Fatalf("mutation after early Done failed: %v", err)
					}

					// Repeated Done is idempotent: it never over-releases
					// and never panics, and a new iterator afterwards works.
					again := c.Iterate()
					again.Next(&x)
					again.Done()
					again.Done()
					again.Done()
					next := c.Iterate()
					if got := drain(next); len(got) != c.Len() {
						t.Fatalf("after repeated Done, traversal len = %d, want %d", len(got), c.Len())
					}
					next.Done()
					next.Done() // idempotent even after the lease was reused

					// Exhaustion without Done keeps the lease live:
					// mutation must still be rejected.
					leaked := c.Iterate()
					drain(leaked)
					if err := tc.mutate(c); err == nil || err.Error() != tc.iterErr {
						t.Fatalf("mutate after exhaustion without Done: got %v, want %q", err, tc.iterErr)
					}
					// The retained iterator can still be released late,
					// restoring mutability.
					leaked.Done()
					if err := tc.mutate(c); err != nil {
						t.Fatalf("mutation after late Done failed: %v", err)
					}

					// Freeze: mutation is rejected with the frozen wording,
					// including during a live iteration, and existing
					// iterators still release cleanly afterwards.
					live := c.Iterate()
					live.Next(&x)
					c.Freeze()
					if err := tc.mutate(c); err == nil || err.Error() != tc.frozenErr {
						t.Fatalf("mutate frozen container: got %v, want %q", err, tc.frozenErr)
					}
					live.Done()
					live.Done() // idempotent across the freeze boundary
					// An iterator created after freeze is inert but valid.
					post := c.Iterate()
					drain(post)
					post.Done()
					post.Done()
				})
			}
		})
	}
}

// TestIterationLeaseDeferDone verifies the common "defer Done" error-path
// idiom: a mutation attempted while the deferred release is still pending
// fails, the deferred Done releases exactly once, and the container is
// mutable again. A second (explicit) Done in the same path is harmless.
func TestIterationLeaseDeferDone(t *testing.T) {
	for _, tc := range leaseCases {
		t.Run(tc.name, func(t *testing.T) {
			c := tc.non()

			mutateWhileIterating := func() (err error) {
				it := c.Iterate()
				defer func() { it.Done() }()
				var x Value
				it.Next(&x)
				err = tc.mutate(c) // must be rejected: lease still live
				return err
			}

			if err := mutateWhileIterating(); err == nil || err.Error() != tc.iterErr {
				t.Fatalf("mutate under deferred Done: got %v, want %q", err, tc.iterErr)
			}
			if err := tc.mutate(c); err != nil {
				t.Fatalf("mutation after deferred release failed: %v", err)
			}
		})
	}
}

// TestIterationLeaseDeleteReinsert exercises the hashtable-specific
// invalidation: deleting or inserting while an iterator is live is
// rejected; once the lease is released, deleting a key and reinserting it
// works again and preserves the existing insertion-order semantics
// (a reinserted entry moves to the end; this behavior is unchanged).
func TestIterationLeaseDeleteReinsert(t *testing.T) {
	for _, tc := range leaseCases {
		if tc.deleteFirst == nil {
			continue // List has no key-deletion primitive
		}
		t.Run(tc.name, func(t *testing.T) {
			c := tc.non()

			// Deletion during iteration is rejected.
			it := c.Iterate()
			var x Value
			it.Next(&x)
			if found, err := tc.deleteFirst(c); err == nil {
				t.Fatalf("delete during iteration succeeded (found=%v)", found)
			} else if !strings.Contains(err.Error(), "during iteration") {
				t.Fatalf("delete during iteration: got %v, want during iteration", err)
			}
			it.Done()

			// After Done: delete and reinsert is permitted.
			found, err := tc.deleteFirst(c)
			if err != nil || !found {
				t.Fatalf("delete after Done: found=%v err=%v", found, err)
			}
			if err := tc.reinsert(c); err != nil {
				t.Fatalf("reinsert after delete: %v", err)
			}
			// Nothing else changed; the removed key is present again.
			if c.Len() != 3 {
				t.Fatalf("Len after delete+reinsert = %d, want 3", c.Len())
			}
			it2 := c.Iterate()
			got := drain(it2)
			it2.Done()
			want := []string{"2", "3", "1"} // existing insertion-order behavior
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("order after delete+reinsert = %v, want %v", got, want)
			}

			// Freeze after delete+reinsert: further deletes and inserts
			// are rejected with the frozen wording.
			c.Freeze()
			if _, err := tc.deleteFirst(c); err == nil || err.Error() == tc.iterErr {
				t.Fatalf("delete frozen container: got %v, want frozen error", err)
			}
			if err := tc.reinsert(c); err == nil || err.Error() != tc.frozenErr {
				t.Fatalf("reinsert frozen container: got %v, want %q", err, tc.frozenErr)
			}
		})
	}
}
