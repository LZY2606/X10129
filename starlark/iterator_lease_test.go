package starlark_test

import (
	"testing"

	"go.starlark.net/starlark"
)

type iteratorLeaseCase struct {
	name            string
	makeEmpty       func() starlark.Value
	makeFull        func() starlark.Value
	mutate          func(starlark.Value) error
	mutatingError   string
	frozenError     string
	deletingError   string
	iterateSeq      func(starlark.Value, func(starlark.Value) bool)
	deleteKey       func(starlark.Value) error
	reinsertKey     func(starlark.Value) error
	reinsertedOrder []string
}

func iteratorLeaseCases() []iteratorLeaseCase {
	one, two, three := starlark.MakeInt(1), starlark.MakeInt(2), starlark.MakeInt(3)
	return []iteratorLeaseCase{
		{
			name:          "List",
			makeEmpty:     func() starlark.Value { return starlark.NewList(nil) },
			makeFull:      func() starlark.Value { return starlark.NewList([]starlark.Value{one, two, three}) },
			mutate:        func(v starlark.Value) error { return v.(*starlark.List).Append(starlark.MakeInt(9)) },
			mutatingError: "cannot append to list during iteration",
			frozenError:   "cannot append to frozen list",
			iterateSeq: func(v starlark.Value, yield func(starlark.Value) bool) {
				for x := range v.(*starlark.List).Elements() {
					if !yield(x) {
						break
					}
				}
			},
		},
		{
			name:      "Dict",
			makeEmpty: func() starlark.Value { return starlark.NewDict(0) },
			makeFull: func() starlark.Value {
				d := starlark.NewDict(3)
				d.SetKey(one, starlark.MakeInt(10))
				d.SetKey(two, starlark.MakeInt(20))
				d.SetKey(three, starlark.MakeInt(30))
				return d
			},
			mutate: func(v starlark.Value) error {
				return v.(*starlark.Dict).SetKey(starlark.MakeInt(4), starlark.MakeInt(40))
			},
			mutatingError: "cannot insert into hash table during iteration",
			frozenError:   "cannot insert into frozen hash table",
			iterateSeq: func(v starlark.Value, yield func(starlark.Value) bool) {
				for k := range v.(*starlark.Dict).Entries() {
					if !yield(k) {
						break
					}
				}
			},
			deletingError: "cannot delete from hash table during iteration",
			deleteKey: func(v starlark.Value) error {
				_, _, err := v.(*starlark.Dict).Delete(one)
				return err
			},
			reinsertKey: func(v starlark.Value) error {
				return v.(*starlark.Dict).SetKey(one, starlark.MakeInt(40))
			},
			reinsertedOrder: []string{"2", "3", "1"},
		},
		{
			name:      "Set",
			makeEmpty: func() starlark.Value { return starlark.NewSet(0) },
			makeFull: func() starlark.Value {
				s := starlark.NewSet(3)
				s.Insert(one)
				s.Insert(two)
				s.Insert(three)
				return s
			},
			mutate:        func(v starlark.Value) error { return v.(*starlark.Set).Insert(starlark.MakeInt(4)) },
			mutatingError: "cannot insert into hash table during iteration",
			frozenError:   "cannot insert into frozen hash table",
			iterateSeq: func(v starlark.Value, yield func(starlark.Value) bool) {
				for x := range v.(*starlark.Set).Elements() {
					if !yield(x) {
						break
					}
				}
			},
			deletingError: "cannot delete from hash table during iteration",
			deleteKey: func(v starlark.Value) error {
				_, err := v.(*starlark.Set).Delete(one)
				return err
			},
			reinsertKey: func(v starlark.Value) error {
				return v.(*starlark.Set).Insert(one)
			},
			reinsertedOrder: []string{"2", "3", "1"},
		},
	}
}

func TestIteratorLeaseLifecycle(t *testing.T) {
	for _, tc := range iteratorLeaseCases() {
		t.Run(tc.name, func(t *testing.T) {
			for _, empty := range []bool{true, false} {
				name := "non-empty"
				want := []string{"1", "2", "3"}
				if empty {
					name = "empty"
					want = nil
				}
				t.Run(name, func(t *testing.T) {
					v := tc.makeFull()
					if empty {
						v = tc.makeEmpty()
					}

					iter := starlark.Iterate(v)
					if iter == nil {
						t.Fatal("Iterate returned nil")
					}
					assertIteratorError(t, tc.mutate(v), tc.mutatingError)
					if got := drainIterator(iter); !equalStrings(got, want) {
						t.Fatalf("iteration = %v, want %v", got, want)
					}
					assertIteratorError(t, tc.mutate(v), tc.mutatingError)
					iter.Done()
					if err := tc.mutate(v); err != nil {
						t.Fatalf("mutation after Done: %v", err)
					}
					if empty {
						if err := mutateDuringSeq(v, tc.iterateSeq, tc.mutate); err == nil || err.Error() != tc.mutatingError {
							t.Fatalf("empty push iterator error = %v, want %q", err, tc.mutatingError)
						}
						if err := tc.mutate(v); err != nil {
							t.Fatalf("mutation after empty push iterator: %v", err)
						}
					}
				})
			}

			t.Run("nested iterators and exhaustion", func(t *testing.T) {
				v := tc.makeFull()
				first := starlark.Iterate(v)
				second := starlark.Iterate(v)
				var x starlark.Value
				if !first.Next(&x) || x.String() != "1" {
					t.Fatalf("first iterator = %v, want 1", x)
				}
				if !second.Next(&x) || x.String() != "1" {
					t.Fatalf("second iterator = %v, want 1", x)
				}

				assertIteratorError(t, tc.mutate(v), tc.mutatingError)
				first.Done()
				first.Done()
				assertIteratorError(t, tc.mutate(v), tc.mutatingError)

				if got := drainIterator(second); !equalStrings(got, []string{"2", "3"}) {
					t.Fatalf("second iterator = %v, want [2 3]", got)
				}
				assertIteratorError(t, tc.mutate(v), tc.mutatingError)
				second.Done()
				second.Done()
				if err := tc.mutate(v); err != nil {
					t.Fatalf("mutation after both Done calls: %v", err)
				}
			})

			t.Run("freeze with active iterator", func(t *testing.T) {
				v := tc.makeFull()
				iter := starlark.Iterate(v)
				var x starlark.Value
				iter.Next(&x)
				v.Freeze()
				if got := drainIterator(iter); !equalStrings(got, []string{"2", "3"}) {
					t.Fatalf("iteration after Freeze = %v, want [2 3]", got)
				}
				assertIteratorError(t, tc.mutate(v), tc.frozenError)
				iter.Done()
				iter.Done()
				assertIteratorError(t, tc.mutate(v), tc.frozenError)

				frozenIter := starlark.Iterate(v)
				if got := drainIterator(frozenIter); !equalStrings(got, []string{"1", "2", "3"}) {
					t.Fatalf("frozen iteration = %v, want [1 2 3]", got)
				}
				frozenIter.Done()
				frozenIter.Done()
			})

			t.Run("error path releases deferred lease", func(t *testing.T) {
				v := tc.makeFull()
				if err := mutateWithDeferredDone(v, tc.mutate); err == nil || err.Error() != tc.mutatingError {
					t.Fatalf("error = %v, want %q", err, tc.mutatingError)
				}
				if err := tc.mutate(v); err != nil {
					t.Fatalf("mutation after deferred Done: %v", err)
				}
			})

			t.Run("push iterator lease", func(t *testing.T) {
				v := tc.makeFull()
				if err := mutateDuringSeq(v, tc.iterateSeq, tc.mutate); err == nil || err.Error() != tc.mutatingError {
					t.Fatalf("error = %v, want %q", err, tc.mutatingError)
				}
				if err := tc.mutate(v); err != nil {
					t.Fatalf("mutation after push iterator: %v", err)
				}
			})

			if tc.deleteKey != nil {
				t.Run("delete and reinsert", func(t *testing.T) {
					v := tc.makeFull()
					iter := starlark.Iterate(v)
					assertIteratorError(t, tc.deleteKey(v), tc.deletingError)
					assertIteratorError(t, tc.reinsertKey(v), tc.mutatingError)
					iter.Done()

					if err := tc.deleteKey(v); err != nil {
						t.Fatalf("delete after Done: %v", err)
					}
					if err := tc.reinsertKey(v); err != nil {
						t.Fatalf("reinsert after delete: %v", err)
					}
					if got := drainIterator(starlark.Iterate(v)); !equalStrings(got, tc.reinsertedOrder) {
						t.Fatalf("order after reinsert = %v, want %v", got, tc.reinsertedOrder)
					}
				})
			}
		})
	}
}

func drainIterator(iter starlark.Iterator) []string {
	var got []string
	var x starlark.Value
	for iter.Next(&x) {
		got = append(got, x.String())
	}
	return got
}

func mutateWithDeferredDone(v starlark.Value, mutate func(starlark.Value) error) (err error) {
	iter := starlark.Iterate(v)
	defer iter.Done()
	var x starlark.Value
	iter.Next(&x)
	return mutate(v)
}

func mutateDuringSeq(v starlark.Value, seq func(starlark.Value, func(starlark.Value) bool), mutate func(starlark.Value) error) (err error) {
	seq(v, func(starlark.Value) bool {
		err = mutate(v)
		return false
	})
	return err
}

func assertIteratorError(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}

func equalStrings(x, y []string) bool {
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}
