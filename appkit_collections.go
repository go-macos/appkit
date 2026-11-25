// Copyright (c) the appkit authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package appkit

// The collection kinds: a multi-column grid, an outline and a tab row. They
// share what [TableView] taught this package: AppKit does not HOLD a collection,
// it ASKS for it, so the data lives beside the control and is handed over by
// reference. A grid of 10,000 rows is therefore one slice header, not 10,000
// cells built up front, and replacing it is one reload.

// TreeItem is one node of an [OutlineView]: its title, whether it is open when
// the outline is first built, and its children.
type TreeItem struct {
	Title    string
	Expanded bool
	Children []TreeItem
}

// gridImpl and treeImpl are the optional halves of impl that only the
// collection kinds carry, so every other control (and every fake in the tests)
// stays as small as it was.
type gridImpl interface {
	setGrid(columns []string, rows [][]string)
}
type treeImpl interface {
	setTree(nodes []TreeItem)
}

// NewGridView makes an NSTableView of several columns, with a header, inside
// an NSScrollView. It is virtual: AppKit asks the data source for the cells it
// is about to draw, so the rows are never copied or built per frame. The chosen
// row is [Control.Double] (-1 for none); OnChange fires when it moves. The rows
// are kept by reference and must not be changed in place; replace them with
// [Control.SetGrid].
func NewGridView(columns []string, rows [][]string) (*Control, error) {
	return New(Spec{Kind: GridView, Columns: columns, Rows: rows})
}

// NewOutlineView makes an NSOutlineView of one column inside an NSScrollView.
// The chosen node is [Control.StringValue] as a path of child indices joined by
// dots ("2.0.1"), empty for none; OnChange fires when it moves. Replace the
// nodes with [Control.SetTree].
func NewOutlineView(nodes []TreeItem) (*Control, error) {
	return New(Spec{Kind: OutlineView, Tree: nodes})
}

// NewTabView makes an NSTabView over titles. Its pages are empty: the host lays
// the content out itself. The chosen tab is [Control.Double]; OnChange fires
// when it moves. Replace the titles with [Control.SetItems].
func NewTabView(titles []string) (*Control, error) {
	return New(Spec{Kind: TabView, Items: titles})
}

// SetGrid replaces the columns and rows of a GridView. It does nothing for any
// other kind.
func (c *Control) SetGrid(columns []string, rows [][]string) error {
	return c.withImpl(func(im impl) {
		if g, ok := im.(gridImpl); ok {
			g.setGrid(columns, rows)
		}
	})
}

// SetTree replaces the nodes of an OutlineView. It does nothing for any other
// kind.
func (c *Control) SetTree(nodes []TreeItem) error {
	return c.withImpl(func(im impl) {
		if t, ok := im.(treeImpl); ok {
			t.setTree(nodes)
		}
	})
}
