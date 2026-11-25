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
	return nil
}
