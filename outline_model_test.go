// Copyright (c) the appkit authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package appkit

import (
	"reflect"
	"testing"
)

func sampleTree() []TreeItem {
	return []TreeItem{
		{Title: "a", Expanded: true, Children: []TreeItem{{Title: "a0"}, {Title: "a1", Children: []TreeItem{{Title: "a1x"}}}}},
		{Title: "b"},
	}
}

func TestFlattenModel(t *testing.T) {
	m := flattenModel(sampleTree())
	if !reflect.DeepEqual(m.roots, []int{0, 4}) {
		t.Fatalf("roots = %v", m.roots)
	}
	wantPaths := []string{"0", "0.0", "0.1", "0.1.0", "1"}
	for i, want := range wantPaths {
		if got := m.path(i); got != want {
			t.Errorf("path(%d) = %q, want %q", i, got, want)
		}
	}
	if !m.nodes[0].expanded || m.nodes[3].expanded {
		t.Errorf("expanded flags lost: %+v", m.nodes)
	}
}

func TestOutlineModelLookups(t *testing.T) {
	m := flattenModel(sampleTree())
	if got := m.kids(-1); !reflect.DeepEqual(got, []int{0, 4}) {
		t.Errorf("root kids = %v", got)
	}
	if got := m.kids(0); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Errorf("kids(0) = %v", got)
	}
	if got := m.kids(len(m.nodes)); got != nil {
		t.Errorf("kids past the end = %v", got)
	}
	if got := m.kids(len(m.nodes) - 1); len(got) != 0 {
		t.Errorf("a leaf has kids %v", got)
	}
	if c, ok := m.child(0, 1); !ok || c != 2 {
		t.Errorf("child(0,1) = %d,%v", c, ok)
	}
	if _, ok := m.child(0, 2); ok {
		t.Error("child past the end reported ok")
	}
	if _, ok := m.child(0, -1); ok {
		t.Error("negative child reported ok")
	}
	if c, ok := m.child(-1, 1); !ok || c != 4 {
		t.Errorf("root child(1) = %d,%v", c, ok)
	}
	if m.title(2) != "a1" || m.title(-1) != "" || m.title(len(m.nodes)) != "" {
		t.Error("title lookup wrong")
	}
	if m.path(-1) != "" || m.path(len(m.nodes)) != "" {
		t.Error("path of nothing is not empty")
	}
}

func TestOutlineModelResolve(t *testing.T) {
	m := flattenModel(sampleTree())
	chain, ok := m.resolve("0.1.0")
	if !ok || !reflect.DeepEqual(chain, []int{0, 2, 3}) {
		t.Errorf("resolve(0.1.0) = %v,%v", chain, ok)
	}
	for _, bad := range []string{"", "x", "2", "-1", "0.2", "0.", "0.0.0"} {
		if c, ok := m.resolve(bad); ok || c != nil {
			t.Errorf("resolve(%q) = %v,%v, want none", bad, c, ok)
		}
	}
	if c, ok := m.resolve("1"); !ok || len(c) != 1 || c[0] != 4 {
		t.Errorf("resolve(1) = %v,%v", c, ok)
	}
}

func TestCellAt(t *testing.T) {
	rows := [][]string{{"a", "b"}, {"c"}}
	cases := []struct {
		col, row int
		want     string
	}{{0, 0, "a"}, {1, 0, "b"}, {0, 1, "c"}, {1, 1, ""}, {2, 0, ""}, {-1, 0, ""}, {0, -1, ""}, {0, 2, ""}}
	for _, c := range cases {
		if got := cellAt(rows, c.col, c.row); got != c.want {
			t.Errorf("cellAt(%d,%d) = %q, want %q", c.col, c.row, got, c.want)
		}
	}
}

func TestEqualStrings(t *testing.T) {
	if !equalStrings(nil, []string{}) || !equalStrings([]string{"a"}, []string{"a"}) {
		t.Error("equal slices differ")
	}
	if equalStrings([]string{"a"}, []string{"a", "b"}) || equalStrings([]string{"a", "b"}, []string{"a", "c"}) {
		t.Error("different slices are equal")
	}
}
