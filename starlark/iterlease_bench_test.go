// Copyright 2026 The Bazel Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package starlark_test

// Benchmarks of the ordinary iterator hot path for the three mutable
// container types. The exact allocation guarantee is pinned by
// TestIterationLeaseAllocs (testing.AllocsPerRun with a zero ceiling);
// these benchmarks report the human-readable cost under -bench.

import (
	"testing"

	. "go.starlark.net/starlark"
)

func BenchmarkIterateLease(b *testing.B) {
	const n = 1000
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
	var sink Value
	b.Run("list", func(b *testing.B) {
		l := mkList()
		b.ReportAllocs()
		for range b.N {
			it := l.Iterate()
			var x Value
			for it.Next(&x) {
				sink = x
			}
			it.Done()
		}
	})
	b.Run("dict", func(b *testing.B) {
		d := mkDict()
		b.ReportAllocs()
		for range b.N {
			it := d.Iterate()
			var x Value
			for it.Next(&x) {
				sink = x
			}
			it.Done()
		}
	})
	b.Run("set", func(b *testing.B) {
		s := mkSet()
		b.ReportAllocs()
		for range b.N {
			it := s.Iterate()
			var x Value
			for it.Next(&x) {
				sink = x
			}
			it.Done()
		}
	})
	_ = sink
}
