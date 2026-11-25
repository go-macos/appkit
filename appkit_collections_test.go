// Copyright (c) the appkit authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package appkit

import "testing"

// The collection halves of the fake: they record what the control handed over.
type collFake struct {
	fakeImpl
	cols []string
	rows [][]string
	tree []TreeItem
}

func (f *collFake) setGrid(c []string, r [][]string) { f.cols, f.rows = c, r }
func (f *collFake) setTree(n []TreeItem)             { f.tree = n }

func TestCollectionConstructorsAndSetters(t *testing.T) {
	old := create
	create = func(Spec, uint64) (impl, error) { return &collFake{}, nil }
	t.Cleanup(func() { create = old })

	g, err := NewGridView([]string{"a", "b"}, [][]string{{"1", "2"}})
	if err != nil || g.Kind() != GridView {
		t.Fatalf("NewGridView: %v kind %v", err, g.Kind())
	}
	if err := g.SetGrid([]string{"x"}, [][]string{{"y"}}); err != nil {
		t.Fatal(err)
	}
	if f := g.im.(*collFake); len(f.cols) != 1 || f.cols[0] != "x" || f.rows[0][0] != "y" {
		t.Errorf("grid not handed over: %v %v", f.cols, f.rows)
	}

	o, err := NewOutlineView([]TreeItem{{Title: "r"}})
	if err != nil || o.Kind() != OutlineView {
		t.Fatalf("NewOutlineView: %v kind %v", err, o.Kind())
	}
	if err := o.SetTree([]TreeItem{{Title: "z"}}); err != nil {
		t.Fatal(err)
	}
	if f := o.im.(*collFake); len(f.tree) != 1 || f.tree[0].Title != "z" {
		t.Errorf("tree not handed over: %v", f.tree)
	}

	tb, err := NewTabView([]string{"one", "two"})
	if err != nil || tb.Kind() != TabView {
		t.Fatalf("NewTabView: %v kind %v", err, tb.Kind())
	}
}

// A control that is not a collection ignores SetGrid and SetTree.
func TestCollectionSettersIgnoreOtherKinds(t *testing.T) {
	fakeCreate(t)
	c, err := NewLabel("x")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SetGrid(nil, nil); err != nil {
		t.Errorf("SetGrid on a label: %v", err)
	}
	if err := c.SetTree(nil); err != nil {
		t.Errorf("SetTree on a label: %v", err)
	}
}
