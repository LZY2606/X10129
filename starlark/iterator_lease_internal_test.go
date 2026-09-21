package starlark

import "testing"

func TestIteratorLeaseAllocsPerRun(t *testing.T) {
	const n = 64
	listElems := make([]Value, n)
	for i := range listElems {
		listElems[i] = MakeInt(i)
	}
	list := NewList(listElems)

	dict := NewDict(n)
	set := NewSet(n)
	for i := 0; i < n; i++ {
		k := MakeInt(i)
		if err := dict.SetKey(k, None); err != nil {
			t.Fatal(err)
		}
		if err := set.Insert(k); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name string
		run  func()
	}{
		{
			name: "List",
			run: func() {
				iter := list.Iterate()
				var x Value
				for iter.Next(&x) {
				}
				iter.Done()
			},
		},
		{
			name: "Dict",
			run: func() {
				iter := dict.Iterate()
				var x Value
				for iter.Next(&x) {
				}
				iter.Done()
			},
		},
		{
			name: "Set",
			run: func() {
				iter := set.Iterate()
				var x Value
				for iter.Next(&x) {
				}
				iter.Done()
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if allocs := testing.AllocsPerRun(100, tc.run); allocs > 1 {
				t.Fatalf("iteration allocated %v times per run, want at most 1 for the iterator itself", allocs)
			}
		})
	}
}
