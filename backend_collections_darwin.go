// Copyright (c) the appkit authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package appkit

import (
	"strconv"
	"strings"
	"sync"

	"github.com/go-macos/objc"
)

// The darwin half of the collection kinds: an NSTableView of several columns, an
// NSOutlineView and an NSTabView. AppKit asks, the data sits beside the control.

type gridState struct {
	cols   []string
	rows   [][]string
	colIdx map[objc.ID]int // NSTableColumn -> column index
}

// onode is one outline node, flattened: an NSOutlineView item is an NSNumber
// holding the node's index in nodes.
type onode struct {
	title    string
	expanded bool
	children []int
	path     string
}

type outlineState struct {
	nodes []onode
	roots []int
	items []objc.ID // retained NSNumber per node; NSOutlineView does not retain items
}

var (
	collMu   sync.Mutex
	grids    = map[uintptr]*gridState{}
	outlines = map[uintptr]*outlineState{}

	// collQuiet is set while this package itself changes a collection: AppKit
	// reports a selection change for a reload or a programmatic select, and
	// forwarding that to the application would overwrite the value it just set
	// (a reload clears the selection, so a tree would report "none").
	// Everything here runs on the main thread, so a plain flag is enough.
	collQuiet bool
)

func gridOf(tv objc.ID) *gridState {
	collMu.Lock()
	defer collMu.Unlock()
	return grids[uintptr(tv)]
}

func outlineOf(ov objc.ID) *outlineState {
	collMu.Lock()
	defer collMu.Unlock()
	return outlines[uintptr(ov)]
}

// gridCell is the text of one cell; an out-of-range ask answers empty.
func gridCell(g *gridState, col objc.ID, row int) string {
	c, ok := g.colIdx[col]
	if !ok || row < 0 || row >= len(g.rows) || c >= len(g.rows[row]) {
		return ""
	}
	return g.rows[row][c]
}

func quietly(fn func()) {
	was := collQuiet
	collQuiet = true
	defer func() { collQuiet = was }()
	fn()
}

func makeGridView(cols []string, rows [][]string) (sv, tv objc.ID) {
	tv = newObject("NSTableView")
	if tv == 0 {
		return 0, 0
	}
	g := &gridState{cols: cols, rows: rows, colIdx: map[objc.ID]int{}}
	for i, title := range cols {
		col := newObject("NSTableColumn")
		if col == 0 {
			tv.Send(objc.Sel("release"))
			return 0, 0
		}
		col.Send(objc.Sel("setIdentifier:"), objc.NSString(strconv.Itoa(i)))
		col.Send(objc.Sel("setEditable:"), false)
		col.Send(objc.Sel("headerCell")).Send(objc.Sel("setStringValue:"), objc.NSString(title))
		col.Send(objc.Sel("setWidth:"), 120.0)
		tv.Send(objc.Sel("addTableColumn:"), col)
		g.colIdx[col] = i
		col.Send(objc.Sel("release")) // the table owns it now
	}
	tv.Send(objc.Sel("setUsesAlternatingRowBackgroundColors:"), true)
	tv.Send(objc.Sel("setAllowsMultipleSelection:"), false)
	tv.Send(objc.Sel("setAllowsColumnSelection:"), false)
	tv.Send(objc.Sel("setColumnAutoresizingStyle:"), 1) // NSTableViewUniformColumnAutoresizingStyle
	collMu.Lock()
	grids[uintptr(tv)] = g
	collMu.Unlock()
	return scrollAround(tv)
}

func scrollAround(doc objc.ID) (sv, inner objc.ID) {
	sv = newObject("NSScrollView")
	if sv == 0 {
		collMu.Lock()
		delete(grids, uintptr(doc))
		delete(outlines, uintptr(doc))
		collMu.Unlock()
		doc.Send(objc.Sel("release"))
		return 0, 0
	}
	sv.Send(objc.Sel("setHasVerticalScroller:"), true)
	sv.Send(objc.Sel("setAutohidesScrollers:"), true)
	sv.Send(objc.Sel("setDocumentView:"), doc) // retains doc
	doc.Send(objc.Sel("release"))
	return sv, doc
}

func makeOutlineView(nodes []TreeItem) (sv, ov objc.ID) {
	ov = newObject("NSOutlineView")
	if ov == 0 {
		return 0, 0
	}
	col := newObject("NSTableColumn")
	if col == 0 {
		ov.Send(objc.Sel("release"))
		return 0, 0
	}
	col.Send(objc.Sel("setIdentifier:"), objc.NSString("outline"))
	col.Send(objc.Sel("setEditable:"), false)
	ov.Send(objc.Sel("addTableColumn:"), col)
	ov.Send(objc.Sel("setOutlineTableColumn:"), col)
	col.Send(objc.Sel("release"))
	ov.Send(objc.Sel("setHeaderView:"), objc.ID(0))
	ov.Send(objc.Sel("setColumnAutoresizingStyle:"), 1)
	ov.Send(objc.Sel("setAllowsMultipleSelection:"), false)
	st := &outlineState{}
	flattenTree(st, nodes)
	collMu.Lock()
	outlines[uintptr(ov)] = st
	collMu.Unlock()
	return scrollAround(ov)
}

// flattenTree fills st from nodes and retains one NSNumber item per node.
func flattenTree(st *outlineState, nodes []TreeItem) {
	var add func(ns []TreeItem, prefix string) []int
	add = func(ns []TreeItem, prefix string) []int {
		idx := make([]int, 0, len(ns))
		for i, n := range ns {
			p := strconv.Itoa(i)
			if prefix != "" {
				p = prefix + "." + p
			}
			at := len(st.nodes)
			st.nodes = append(st.nodes, onode{title: n.Title, expanded: n.Expanded, path: p})
			kids := add(n.Children, p)
			st.nodes[at].children = kids
			idx = append(idx, at)
		}
		return idx
	}
	st.roots = add(nodes, "")
	st.items = make([]objc.ID, len(st.nodes))
	for i := range st.nodes {
		it := objc.ClassID("NSNumber").Send(objc.Sel("numberWithInteger:"), i)
		it.Send(objc.Sel("retain"))
		st.items[i] = it
	}
}

func (st *outlineState) releaseItems() {
	for _, it := range st.items {
		it.Send(objc.Sel("release"))
	}
	st.items = nil
}

// kids is the child list an outline item names; item 0 is the root.
func (st *outlineState) kids(item objc.ID) []int {
	if item == 0 {
		return st.roots
	}
	i := objc.Send[int](item, objc.Sel("integerValue"))
	if i < 0 || i >= len(st.nodes) {
		return nil
	}
	return st.nodes[i].children
}

// outlineExpand opens the nodes that asked to be open, parents first.
func (n *nativeControl) outlineExpand(st *outlineState) {
	for i, nd := range st.nodes {
		if nd.expanded && len(nd.children) > 0 {
			n.value.Send(objc.Sel("expandItem:"), st.items[i])
		}
	}
}

func makeTabView(titles []string) objc.ID {
	tv := newObject("NSTabView")
	if tv == 0 {
		return 0
	}
	fillTabs(tv, titles)
	return tv
}

func fillTabs(tv objc.ID, titles []string) {
	for _, t := range titles {
		it := newObject("NSTabViewItem")
		if it == 0 {
			continue
		}
		it.Send(objc.Sel("setLabel:"), objc.NSString(t))
		tv.Send(objc.Sel("addTabViewItem:"), it)
		it.Send(objc.Sel("release")) // the tab view owns it now
	}
}

func (n *nativeControl) setGrid(cols []string, rows [][]string) {
	g := gridOf(n.value)
	if g == nil {
		return
	}
	if len(cols) != len(g.cols) || !equalStrings(cols, g.cols) {
		// A different set of columns is a different control: rebuild them.
		existing := n.value.Send(objc.Sel("tableColumns"))
		for objc.Send[int](existing, objc.Sel("count")) > 0 {
			n.value.Send(objc.Sel("removeTableColumn:"), existing.Send(objc.Sel("objectAtIndex:"), 0))
			existing = n.value.Send(objc.Sel("tableColumns"))
		}
		collMu.Lock()
		g.colIdx = map[objc.ID]int{}
		collMu.Unlock()
		for i, title := range cols {
			col := newObject("NSTableColumn")
			if col == 0 {
				continue
			}
			col.Send(objc.Sel("setIdentifier:"), objc.NSString(strconv.Itoa(i)))
			col.Send(objc.Sel("setEditable:"), false)
			col.Send(objc.Sel("headerCell")).Send(objc.Sel("setStringValue:"), objc.NSString(title))
			col.Send(objc.Sel("setWidth:"), 120.0)
			n.value.Send(objc.Sel("addTableColumn:"), col)
			collMu.Lock()
			g.colIdx[col] = i
			collMu.Unlock()
			col.Send(objc.Sel("release"))
		}
	}
	collMu.Lock()
	g.cols, g.rows = cols, rows
	collMu.Unlock()
	quietly(func() { n.value.Send(objc.Sel("reloadData")) })
}

func (n *nativeControl) setTree(nodes []TreeItem) {
	st := outlineOf(n.value)
	if st == nil {
		return
	}
	old := *st
	fresh := &outlineState{}
	flattenTree(fresh, nodes)
	collMu.Lock()
	*st = *fresh
	collMu.Unlock()
	quietly(func() {
		n.value.Send(objc.Sel("reloadData"))
		n.outlineExpand(st)
	})
	old.releaseItems()
}

// outlineSelectedPath is the path of the chosen node, "" for none.
func (n *nativeControl) outlineSelectedPath() string {
	st := outlineOf(n.value)
	row := objc.Send[int](n.value, objc.Sel("selectedRow"))
	if st == nil || row < 0 {
		return ""
	}
	item := n.value.Send(objc.Sel("itemAtRow:"), row)
	if item == 0 {
		return ""
	}
	i := objc.Send[int](item, objc.Sel("integerValue"))
	if i < 0 || i >= len(st.nodes) {
		return ""
	}
	return st.nodes[i].path
}

// outlineSelectPath chooses the node a path names, opening its ancestors, or
// clears the selection for a path that names none.
func (n *nativeControl) outlineSelectPath(p string) {
	st := outlineOf(n.value)
	quietly(func() {
		if st == nil || p == "" {
			n.value.Send(objc.Sel("deselectAll:"), objc.ID(0))
			return
		}
		level := st.roots
		at := -1
		for _, part := range strings.Split(p, ".") {
			k, err := strconv.Atoi(part)
			if err != nil || k < 0 || k >= len(level) {
				n.value.Send(objc.Sel("deselectAll:"), objc.ID(0))
				return
			}
			if at >= 0 {
				n.value.Send(objc.Sel("expandItem:"), st.items[at])
			}
			at = level[k]
			level = st.nodes[at].children
		}
		row := objc.Send[int](n.value, objc.Sel("rowForItem:"), st.items[at])
		if row < 0 {
			n.value.Send(objc.Sel("deselectAll:"), objc.ID(0))
			return
		}
		set := objc.ClassID("NSIndexSet").Send(objc.Sel("indexSetWithIndex:"), row)
		n.value.Send(objc.Sel("selectRowIndexes:byExtendingSelection:"), set, false)
		n.value.Send(objc.Sel("scrollRowToVisible:"), row)
	})
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// selectGridRow chooses a row of a grid, or clears the selection for a row
// outside it.
func (n *nativeControl) selectGridRow(row int) {
	g := gridOf(n.value)
	quietly(func() {
		if g == nil || row < 0 || row >= len(g.rows) {
			n.value.Send(objc.Sel("deselectAll:"), objc.ID(0))
			return
		}
		set := objc.ClassID("NSIndexSet").Send(objc.Sel("indexSetWithIndex:"), row)
		n.value.Send(objc.Sel("selectRowIndexes:byExtendingSelection:"), set, false)
		n.value.Send(objc.Sel("scrollRowToVisible:"), row)
	})
}

func (n *nativeControl) setTabItems(titles []string) {
	quietly(func() {
		for {
			items := n.view.Send(objc.Sel("tabViewItems"))
			if objc.Send[int](items, objc.Sel("count")) == 0 {
				break
			}
			n.view.Send(objc.Sel("removeTabViewItem:"), items.Send(objc.Sel("objectAtIndex:"), 0))
		}
		fillTabs(n.view, titles)
	})
}

func (n *nativeControl) tabSelected() float64 {
	it := n.view.Send(objc.Sel("selectedTabViewItem"))
	if it == 0 {
		return -1
	}
	return float64(objc.Send[int](n.view, objc.Sel("indexOfTabViewItem:"), it))
}

func (n *nativeControl) selectTab(i int) {
	count := objc.Send[int](n.view.Send(objc.Sel("tabViewItems")), objc.Sel("count"))
	if i < 0 || i >= count {
		return
	}
	quietly(func() { n.view.Send(objc.Sel("selectTabViewItemAtIndex:"), i) })
}

// releaseCollections drops what a closed collection control held.
func (n *nativeControl) releaseCollections() {
	collMu.Lock()
	defer collMu.Unlock()
	switch n.kind {
	case GridView:
		delete(grids, uintptr(n.value))
	case OutlineView:
		if st := outlines[uintptr(n.value)]; st != nil {
			st.releaseItems()
		}
		delete(outlines, uintptr(n.value))
	}
}
