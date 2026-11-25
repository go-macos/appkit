// Copyright (c) the appkit authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package appkit

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/go-macos/objc"
)

// runLiveCollections builds the grid, the outline and the tab view for real and
// checks each round-trips its selection through AppKit. Like runLiveSmoke it
// runs on the main thread, from TestMain.
func runLiveCollections() error {
	const n = 10000
	rows := make([][]string, n)
	for i := range rows {
		rows[i] = []string{"r" + strconv.Itoa(i), "b", "c"}
	}
	g, err := NewGridView([]string{"A", "B", "C"}, rows)
	if err != nil {
		return fmt.Errorf("NewGridView: %w", err)
	}
	defer g.Close()
	changes := 0
	g.OnChange(func() { changes++ })
	if got := g.Double(); got != -1 {
		return fmt.Errorf("a fresh GridView reports row %v, want -1", got)
	}
	if err := g.SetDouble(5000); err != nil {
		return err
	}
	if got := g.Double(); got != 5000 {
		return fmt.Errorf("GridView row = %v, want 5000", got)
	}
	rows2 := make([][]string, n)
	copy(rows2, rows)
	rows2[0] = []string{"x", "y", "z"}
	start := time.Now()
	const rounds = 20
	for i := 0; i < rounds; i++ {
		if err := g.SetGrid([]string{"A", "B", "C"}, rows2); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "appkit: GridView SetGrid of %d rows: %v per update\n", n, time.Since(start)/rounds)
	if changes != 0 {
		return fmt.Errorf("programmatic grid changes reported %d OnChange calls, want 0", changes)
	}

	tree := []TreeItem{
		{Title: "a", Expanded: true, Children: []TreeItem{{Title: "a0"}, {Title: "a1"}}},
		{Title: "b", Children: []TreeItem{{Title: "b0"}}},
	}
	o, err := NewOutlineView(tree)
	if err != nil {
		return fmt.Errorf("NewOutlineView: %w", err)
	}
	defer o.Close()
	oc := 0
	o.OnChange(func() { oc++ })
	if got := o.StringValue(); got != "" {
		return fmt.Errorf("a fresh OutlineView reports %q, want none", got)
	}
	// "b.0" sits under a collapsed parent: selecting it must open b.
	for _, p := range []string{"0.1", "1.0", "1"} {
		if err := o.SetStringValue(p); err != nil {
			return err
		}
		if got := o.StringValue(); got != p {
			return fmt.Errorf("OutlineView path = %q, want %q", got, p)
		}
	}
	if err := o.SetTree(tree[:1]); err != nil {
		return err
	}
	if got := o.StringValue(); got != "" {
		return fmt.Errorf("after SetTree the selection is %q, want none", got)
	}
	if oc != 0 {
		return fmt.Errorf("programmatic outline changes reported %d OnChange calls, want 0", oc)
	}

	t, err := NewTabView([]string{"un", "deux", "trois"})
	if err != nil {
		return fmt.Errorf("NewTabView: %w", err)
	}
	defer t.Close()
	tc := 0
	t.OnChange(func() { tc++ })
	if got := t.Double(); got != 0 {
		return fmt.Errorf("a fresh TabView is on tab %v, want 0", got)
	}
	if err := t.SetDouble(2); err != nil {
		return err
	}
	if got := t.Double(); got != 2 {
		return fmt.Errorf("TabView tab = %v, want 2", got)
	}
	if err := t.SetItems([]string{"seul", "deux"}); err != nil {
		return err
	}
	if got := t.Double(); got < 0 || got > 1 {
		return fmt.Errorf("after replacing the titles the tab is %v", got)
	}
	if tc != 0 {
		return fmt.Errorf("programmatic tab changes reported %d OnChange calls, want 0", tc)
	}
	// A tab index outside the strip changes nothing.
	for _, bad := range []float64{-1, 7} {
		if err := t.SetDouble(bad); err != nil {
			return err
		}
		if got := t.Double(); got != 1 && got != 0 {
			return fmt.Errorf("SetDouble(%v) moved the tab to %v", bad, got)
		}
	}
	if err := t.SetDouble(1); err != nil {
		return err
	}
	if err := t.SetDouble(9); err != nil {
		return err
	}
	if got := t.Double(); got != 1 {
		return fmt.Errorf("SetDouble(9) moved the tab to %v, want to stay on 1", got)
	}
	empty, err := NewTabView(nil)
	if err != nil {
		return err
	}
	defer empty.Close()
	if got := empty.Double(); got != -1 {
		return fmt.Errorf("a TabView with no tab reports %v, want -1", got)
	}

	if err := liveCollectionEdges(g, o, t); err != nil {
		return err
	}
	return liveCollectionFailures()
}

// asked sends one data-source question straight to the shared target, the way
// AppKit does, so an out-of-range ask can be made.
func asked(sel string, args ...any) objc.ID {
	return target.Send(objc.Sel(sel), args...)
}

func liveCollectionEdges(g, o, t *Control) error {
	gn := g.im.(*nativeControl)
	cols := gn.value.Send(objc.Sel("tableColumns"))
	col0 := cols.Send(objc.Sel("objectAtIndex:"), 0)
	var err error
	if got := objc.Send[int](target, objc.Sel("numberOfRowsInTableView:"), gn.value); got != 10000 {
		return fmt.Errorf("numberOfRows = %d, want 10000", got)
	}
	for _, c := range []struct {
		col  objc.ID
		row  int
		want string
	}{{col0, 5000, "r5000"}, {col0, 0, "x"}, {col0, -1, ""}, {col0, 10000, ""}, {0, 0, ""}} {
		if got := objc.GoString(asked("tableView:objectValueForTableColumn:row:", gn.value, c.col, c.row)); got != c.want {
			return fmt.Errorf("grid cell(%v,%d) = %q, want %q", c.col, c.row, got, c.want)
		}
	}
	// Row -1 and a row past the end clear the selection.
	for _, row := range []float64{-1, 10000} {
		if err := g.SetDouble(3); err != nil {
			return err
		}
		if err := g.SetDouble(row); err != nil {
			return err
		}
		if got := g.Double(); got != -1 {
			return fmt.Errorf("SetDouble(%v) left row %v selected", row, got)
		}
	}
	// A different set of columns rebuilds them.
	if err := g.SetGrid([]string{"P", "Q"}, [][]string{{"1", "2"}, {"3"}}); err != nil {
		return err
	}
	cols = gn.value.Send(objc.Sel("tableColumns"))
	if got := objc.Send[int](cols, objc.Sel("count")); got != 2 {
		return fmt.Errorf("after SetGrid with 2 columns the table has %d", got)
	}
	q := cols.Send(objc.Sel("objectAtIndex:"), 1)
	if got := objc.GoString(asked("tableView:objectValueForTableColumn:row:", gn.value, q, 0)); got != "2" {
		return fmt.Errorf("rebuilt cell = %q, want 2", got)
	}
	if got := objc.GoString(asked("tableView:objectValueForTableColumn:row:", gn.value, q, 1)); got != "" {
		return fmt.Errorf("a short row's missing cell = %q, want empty", got)
	}
	withoutClass("NSTableColumn", func() { err = g.SetGrid([]string{"U", "V", "W"}, nil) })
	if err != nil {
		return err
	}
	if got := objc.Send[int](gn.value.Send(objc.Sel("tableColumns")), objc.Sel("count")); got != 0 {
		return fmt.Errorf("with column allocation failing the table has %d columns", got)
	}

	// A plain TableView still answers through the same target.
	pt, err := NewTableView([]string{"one", "two"})
	if err != nil {
		return err
	}
	defer pt.Close()
	pn := pt.im.(*nativeControl)
	if got := objc.Send[int](target, objc.Sel("numberOfRowsInTableView:"), pn.value); got != 2 {
		return fmt.Errorf("plain table rows = %d, want 2", got)
	}
	if got := objc.GoString(asked("tableView:objectValueForTableColumn:row:", pn.value, objc.ID(0), 1)); got != "two" {
		return fmt.Errorf("plain table cell = %q, want two", got)
	}

	on := o.im.(*nativeControl)
	if err := o.SetTree([]TreeItem{{Title: "a", Children: []TreeItem{{Title: "a0"}}}, {Title: "b"}}); err != nil {
		return err
	}
	st := outlineOf(on.value)
	root := objc.ID(0)
	if got := objc.Send[int](target, objc.Sel("outlineView:numberOfChildrenOfItem:"), on.value, root); got != 2 {
		return fmt.Errorf("root children = %d, want 2", got)
	}
	if got := objc.Send[int](target, objc.Sel("outlineView:numberOfChildrenOfItem:"), on.value, st.items[0]); got != 1 {
		return fmt.Errorf("children of a = %d, want 1", got)
	}
	if got := asked("outlineView:child:ofItem:", on.value, 1, root); got != st.items[2] {
		return fmt.Errorf("root child 1 = %v, want %v", got, st.items[2])
	}
	for _, idx := range []int{-1, 2} {
		if got := asked("outlineView:child:ofItem:", on.value, idx, root); got != 0 {
			return fmt.Errorf("root child %d = %v, want nil", idx, got)
		}
	}
	if !objc.Send[bool](target, objc.Sel("outlineView:isItemExpandable:"), on.value, st.items[0]) {
		return fmt.Errorf("a node with children is not expandable")
	}
	if objc.Send[bool](target, objc.Sel("outlineView:isItemExpandable:"), on.value, st.items[2]) {
		return fmt.Errorf("a leaf is expandable")
	}
	if !objc.Send[bool](target, objc.Sel("outlineView:isItemExpandable:"), on.value, root) {
		return fmt.Errorf("the root with nodes is not expandable")
	}
	if got := objc.GoString(asked("outlineView:objectValueForTableColumn:byItem:", on.value, objc.ID(0), st.items[1])); got != "a0" {
		return fmt.Errorf("title of a0 = %q", got)
	}
	if got := objc.GoString(asked("outlineView:objectValueForTableColumn:byItem:", on.value, objc.ID(0), root)); got != "" {
		return fmt.Errorf("title of the root = %q, want empty", got)
	}
	// Paths that name no node clear the selection.
	for _, p := range []string{"1", "9", "0.x", ""} {
		if err := o.SetStringValue("0"); err != nil {
			return err
		}
		if err := o.SetStringValue(p); err != nil {
			return err
		}
		want := p
		if p == "9" || p == "0.x" || p == "" {
			want = ""
		}
		if got := o.StringValue(); got != want {
			return fmt.Errorf("SetStringValue(%q) gives %q, want %q", p, got, want)
		}
	}
	return nil
}

// withoutClass runs fn with newObject failing for one class.
func withoutClass(class string, fn func()) {
	old := newObject
	newObject = func(c string) objc.ID {
		if c == class {
			return 0
		}
		return old(c)
	}
	defer func() { newObject = old }()
	fn()
}

// liveCollectionFailures makes each allocation fail in turn: creation answers
// an error, leaks nothing into the registries, and never panics.
func liveCollectionFailures() error {
	collMu.Lock()
	g0, o0 := len(grids), len(outlines)
	collMu.Unlock()
	for _, class := range []string{"NSTableView", "NSTableColumn", "NSScrollView"} {
		var err error
		withoutClass(class, func() {
			var c *Control
			c, err = NewGridView([]string{"A"}, nil)
			if c != nil {
				c.Close()
			}
		})
		if err == nil {
			return fmt.Errorf("NewGridView with %s failing reported no error", class)
		}
	}
	for _, class := range []string{"NSOutlineView", "NSTableColumn", "NSScrollView"} {
		var err error
		withoutClass(class, func() {
			var c *Control
			c, err = NewOutlineView([]TreeItem{{Title: "a"}})
			if c != nil {
				c.Close()
			}
		})
		if err == nil {
			return fmt.Errorf("NewOutlineView with %s failing reported no error", class)
		}
	}
	var err error
	withoutClass("NSTabView", func() {
		var c *Control
		c, err = NewTabView([]string{"a"})
		if c != nil {
			c.Close()
		}
	})
	if err == nil {
		return fmt.Errorf("NewTabView with NSTabView failing reported no error")
	}
	// A tab item that cannot be made is skipped, not fatal.
	err = nil
	withoutClass("NSTabViewItem", func() {
		c, e := NewTabView([]string{"a", "b"})
		if e != nil {
			err = e
			return
		}
		defer c.Close()
		if got := c.Double(); got != -1 {
			err = fmt.Errorf("a TabView whose items failed reports tab %v, want -1", got)
		}
	})
	if err != nil {
		return err
	}
	collMu.Lock()
	g1, o1 := len(grids), len(outlines)
	collMu.Unlock()
	if g1 != g0 || o1 != o0 {
		return fmt.Errorf("failed creations leaked registry entries: grids %d->%d, outlines %d->%d", g0, g1, o0, o1)
	}
	return nil
}
