// Copyright 2026 The Bazel Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package starlark_test

// Allocation guard for the iteration-lease refactoring.
//
// The normal Next hot path must not allocate per element, and a full
// traversal must not allocate more than it did before the refactoring
// (empirically zero: the iterator stays stack-resident and escapes only
// through interface boxes that the compiler stack-allocates). We pin that
// with testing.AllocsPerRun rather than a Benchmark, so a noisy machine
// cannot turn the check into a flaky comparison: the bound is an exact
// integer ceiling rather than a relative measurement.

import (
	"testing"

	. "go.starlark.net/starlark"
)

// iterLeaseAllocCeiling is the maximum number of heap allocations permitted
// for one full traversal (Iterate + Next until exhaustion + Done).
// It must stay zero: iterator construction is stack allocated in the
// common pattern, and each Next only writes through a *Value.
const iterLeaseAllocCeiling = 0

// checkAllocs pins an exact ceiling on heap allocations for one traversal.
// AllocsPerRun returns an average, so with a ceiling of zero the test can
// pass only if every run allocates nothing: there is no room for noise.
func checkAllocs(t *testing.T, f func()) {
	t.Helper()
	if allocs := testing.AllocsPerRun(100, f); allocs > iterLeaseAllocCeiling {
		t.Fatalf("traversal: %v allocs/run, want <= %d", allocs, iterLeaseAllocCeiling)
	}
}

func TestIterationLeaseAllocs(t *testing.T) {
	const n = 256 // large enough that any per-element cost is obvious

	mkList := func() *List {
		l := NewList(make([]Value, 0, n))
		for i := 0; i < n; i++ {
			l.Append(MakeInt(i))
		}
		return l
	}
	mkDict := func() *Dict {
		d := NewDict(n)
		for i := 0; i < n; i++ {
			d.SetKey(MakeInt(i), MakeInt(i))
		}
		return d
	}
	mkSet := func() *Set {
		s := NewSet(n)
		for i := 0; i < n; i++ {
			s.Insert(MakeInt(i))
		}
		return s
	}

	// Each case closes over a concrete *List/*Dict/*Set: going through an
	// interface here would itself force the returned iterator to escape (2
	// allocs), obscuring the cost we are trying to guard.
	for _, tc := range []struct {
		name     string
		traverse func()
	}{
		{"list", func() {
			l := mkList()
			traverse := func() {
				it := l.Iterate()
				var x Value
				for it.Next(&x) {
				}
				it.Done()
			}
			traverse() // warm up
			checkAllocs(t, traverse)
		}},
		{"dict", func() {
			d := mkDict()
			traverse := func() {
				it := d.Iterate()
				var x Value
				for it.Next(&x) {
				}
				it.Done()
			}
			traverse()
			checkAllocs(t, traverse)
		}},
		{"set", func() {
			s := mkSet()
			traverse := func() {
				it := s.Iterate()
				var x Value
				for it.Next(&x) {
				}
				it.Done()
			}
			traverse()
			checkAllocs(t, traverse)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) { tc.traverse() })
	}

	// Early-Done traversals follow the same path: only the number of Next
	// calls differs, so they must meet the same ceiling.
	t.Run("early_done", func(t *testing.T) {
		l := mkList()
		allocs := testing.AllocsPerRun(100, func() {
			it := l.Iterate()
			var x Value
			for i := 0; i < 4 && it.Next(&x); i++ {
			}
			it.Done()
		})
		if allocs > iterLeaseAllocCeiling {
			t.Fatalf("early-Done traversal: %v allocs/run, want <= %d",
				allocs, iterLeaseAllocCeiling)
		}
	})

	// Exhaustion followed by Done (rather than Done alone) must not free
	// or allocate anything extra on the hot path.
	t.Run("exhausted_then_done", func(t *testing.T) {
		s := mkSet()
		allocs := testing.AllocsPerRun(100, func() {
			it := s.Iterate()
			var x Value
			for it.Next(&x) {
			}
			for it.Next(&x) { // reported exhausted; still no allocation
				t.Fatal("unexpected value after exhaustion")
			}
			it.Done()
		})
		if allocs > iterLeaseAllocCeiling {
			t.Fatalf("exhausted traversal: %v allocs/run, want <= %d",
				allocs, iterLeaseAllocCeiling)
		}
	})
}

// TestIterationLeaseGo123Allocs guards the push-iterator paths
// (*List).Elements, (*Set).Elements and (*Dict).Entries.
//
// The ceilings below equal the measured pre-refactoring values (list 0,
// set 0, dict 2 -- the dict path closes over two function values). In
// every case the allocation is a fixed cost per traversal, never one per
// element; a per-element regression would multiply the average with n.
// AllocsPerRun, rather than a benchmark, makes the bound exact and immune
// to environmental noise.
func TestIterationLeaseGo123Allocs(t *testing.T) {
	const n = 256

	l := NewList(make([]Value, 0, n))
	for i := 0; i < n; i++ {
		l.Append(MakeInt(i))
	}
	d := NewDict(n)
	for i := 0; i < n; i++ {
		d.SetKey(MakeInt(i), MakeInt(i))
	}
	s := NewSet(n)
	for i := 0; i < n; i++ {
		s.Insert(MakeInt(i))
	}

	allocs := testing.AllocsPerRun(100, func() {
		for range l.Elements() {
		}
	})
	if allocs > 0 {
		t.Fatalf("range list Elements: %v allocs/run, want <= 0", allocs)
	}
	allocs = testing.AllocsPerRun(100, func() {
		for range s.Elements() {
		}
	})
	if allocs > 0 {
		t.Fatalf("range set Elements: %v allocs/run, want <= 0", allocs)
	}
	allocs = testing.AllocsPerRun(100, func() {
		for range d.Entries() {
		}
	})
	if allocs > 2 {
		t.Fatalf("range dict Entries: %v allocs/run, want <= 2", allocs)
	}

	// Early termination pays the same fixed cost and never per element.
	l0 := l
	allocs = testing.AllocsPerRun(100, func() {
		count := 0
		for range l0.Elements() {
			count++
			if count == 4 {
				break
			}
		}
	})
	if allocs > 0 {
		t.Fatalf("early-break list Elements: %v allocs/run, want <= 0", allocs)
	}
}
