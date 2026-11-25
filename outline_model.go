// Copyright (c) the appkit authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package appkit

import (
	"strconv"
	"strings"
)

// The part of the collection kinds that is plain data: how a tree is flattened,
// how a path names a node, how a cell is looked up. It has no AppKit in it, so it
// is tested on every platform.

// onode is one outline node, flattened: an NSOutlineView item is an NSNumber
// holding the node's index in outlineModel.nodes.
type onode struct {
	title    string
	expanded bool
	children []int
	path     string
}

// outlineModel is a tree as a flat slice plus the indexes of its roots.
type outlineModel struct {
	nodes []onode
	roots []int
}

// flattenModel builds the model of a tree; a node's path is its dotted child
// indexes, "2.0.1".
func flattenModel(nodes []TreeItem) outlineModel {
	var m outlineModel
	var add func(ns []TreeItem, prefix string) []int
	add = func(ns []TreeItem, prefix string) []int {
		idx := make([]int, 0, len(ns))
		for i, n := range ns {
			p := strconv.Itoa(i)
			if prefix != "" {
				p = prefix + "." + p
			}
			at := len(m.nodes)
			m.nodes = append(m.nodes, onode{title: n.Title, expanded: n.Expanded, path: p})
			m.nodes[at].children = add(n.Children, p)
			idx = append(idx, at)
		}
		return idx
	}
	m.roots = add(nodes, "")
	return m
}

// kids is the child list of node i; a negative i is the root.
func (m *outlineModel) kids(i int) []int {
	if i < 0 {
		return m.roots
	}
	if i >= len(m.nodes) {
		return nil
	}
	return m.nodes[i].children
}

// child is the index-th child of node i (negative i: the root list).
func (m *outlineModel) child(i, index int) (int, bool) {
	k := m.kids(i)
	if index < 0 || index >= len(k) {
		return 0, false
	}
	return k[index], true
}

// title is the text of node i, empty for an index that names none.
func (m *outlineModel) title(i int) string {
	if i < 0 || i >= len(m.nodes) {
		return ""
	}
	return m.nodes[i].title
}

// path is the dotted path of node i, empty for an index that names none.
func (m *outlineModel) path(i int) string {
	if i < 0 || i >= len(m.nodes) {
		return ""
	}
	return m.nodes[i].path
}

// resolve turns a dotted path into the node indexes from a root down to the
// node it names; ok is false for "" and for a path that names no node.
func (m *outlineModel) resolve(p string) (chain []int, ok bool) {
	if p == "" {
		return nil, false
	}
	level := m.roots
	for _, part := range strings.Split(p, ".") {
		k, err := strconv.Atoi(part)
		if err != nil || k < 0 || k >= len(level) {
			return nil, false
		}
		chain = append(chain, level[k])
		level = m.nodes[level[k]].children
	}
	return chain, true
}

// cellAt is the text of one cell; an out-of-range ask answers empty.
func cellAt(rows [][]string, col, row int) string {
	if col < 0 || row < 0 || row >= len(rows) || col >= len(rows[row]) {
		return ""
	}
	return rows[row][col]
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
