// Copyright (c) the appkit authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package appkit

import (
	"fmt"
	"os"
	"runtime"
	"testing"

	"github.com/ebitengine/purego"
	"github.com/go-macos/objc"
)

// The LIVE suite. It builds REAL AppKit controls, reads their values back
// through the real Objective-C selectors, and frames one into a real NSView —
// the check that the bindings message the right selectors, which a fake impl
// cannot make.
//
// All the AppKit work happens in TestMain, on the one OS thread it pins, because
// AppKit permits control creation and value access only on the main thread and a
// Go test function runs on an arbitrary goroutine. The work needs no run loop:
// creating a control and reading its value are synchronous, unlike an action,
// which is why this suite asserts VALUES and leaves action delivery to the
// portable dispatch tests. With no window server there is no GUI session to make
// controls in, so the suite skips rather than failing for the wrong reason.

var (
	liveSkipped bool
	liveErr     error
)

// cgMainDisplayID is CoreGraphics' main-display id: 0 when the process has no
// window server (an ssh session, or a CI runner with no GUI login).
var cgMainDisplayID func() uint32

func hasWindowServer() bool {
	lib, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics",
		purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return false
	}
	purego.RegisterLibFunc(&cgMainDisplayID, lib, "CGMainDisplayID")
	return cgMainDisplayID() != 0
}

func TestMain(m *testing.M) {
	runtime.LockOSThread()
	if !hasWindowServer() {
		liveSkipped = true
		os.Exit(m.Run())
	}
	if err := objc.Load(objc.AppKit, objc.Foundation); err != nil {
		os.Stderr.WriteString("cannot load AppKit: " + err.Error() + "\n")
		os.Exit(1)
	}
	if objc.App() == 0 {
		os.Stderr.WriteString("NSApplication could not be created\n")
		os.Exit(1)
	}
	liveErr = runLiveSmoke()
	os.Exit(m.Run())
}

// TestLiveBindings reports the outcome of the real-AppKit smoke that TestMain
// ran on the main thread.
func TestLiveBindings(t *testing.T) {
	if liveSkipped {
		t.Skip("no window server in this session: there are no AppKit controls to build")
	}
	if liveErr != nil {
		t.Fatalf("live bindings: %v", liveErr)
	}
}

// runLiveSmoke builds each control kind for real and checks its value round-trips
// through AppKit. It returns the first mismatch, or nil.
func runLiveSmoke() error {
	// A list, which is the one kind here that answers a DATA SOURCE rather
	// than holding a value: AppKit asks how many rows there are and what is in
	// each, every time it draws.
	tbl, err := NewTableView([]string{"un", "deux", "trois"})
	if err != nil {
		return fmt.Errorf("NewTableView: %w", err)
	}
	defer tbl.Close()
	// Nothing chosen yet reads as -1, not as row zero. A list that claims a
	// selection it does not have makes every caller act on the wrong row.
	if got := tbl.Double(); got != -1 {
		return fmt.Errorf("a fresh TableView reports row %v, want -1", got)
	}
	if err := tbl.SetDouble(2); err != nil {
		return err
	}
	if got := tbl.Double(); got != 2 {
		return fmt.Errorf("TableView selected row = %v, want 2", got)
	}
	// Replacing the rows shortens the list under the selection; AppKit drops
	// it rather than keeping an index past the end.
	if err := tbl.SetItems([]string{"seul"}); err != nil {
		return err
	}
	if got := tbl.Double(); got >= 1 {
		return fmt.Errorf("after shrinking to one row the selection is %v", got)
	}
	if err := tbl.SetDouble(0); err != nil {
		return err
	}
	if got := tbl.Double(); got != 0 {
		return fmt.Errorf("selecting the only row gave %v, want 0", got)
	}
	// A list is READ, not typed into. A cell-based NSTableView hands each row
	// an editable NSTextFieldCell unless its column says otherwise, and
	// clicking a row then opened a text field over it.
	if cols := tbl.columnsAreEditable(); cols {
		return fmt.Errorf("the table's column is editable, so clicking a row opens a field")
	}
	// A real NSMenu, hung off the real table. What this checks is that the
	// selectors exist and the objects take them: a menu built against a
	// misspelt selector is silently empty, which looks exactly like a menu
	// nobody opened.
	if err := tbl.SetMenu([]MenuItem{
		{Title: "Retry", OnPick: func() {}},
		{},
		{Title: "Remove"},
	}); err != nil {
		return fmt.Errorf("SetMenu: %w", err)
	}
	if n := tbl.menuItemCount(); n != 3 {
		return fmt.Errorf("the table's menu holds %d items, want 3", n)
	}
	if err := tbl.SetMenu(nil); err != nil {
		return fmt.Errorf("SetMenu(nil): %w", err)
	}
	if n := tbl.menuItemCount(); n != 0 {
		return fmt.Errorf("after clearing, the menu holds %d items", n)
	}
	// A real picture on a real button, from bytes. A one-pixel PNG is enough:
	// what this checks is that NSData and NSImage take the selectors, not that
	// the icon is pretty. An NSImage built from a misspelt selector is nil,
	// and a nil image is a button that looks exactly like one nobody gave a
	// picture to.
	icon, ierr := NewButton("Pause")
	if ierr != nil {
		return fmt.Errorf("NewButton: %w", ierr)
	}
	defer icon.Close()
	if err := icon.SetImage(onePixelPNG); err != nil {
		return err
	}
	if !icon.hasImage() {
		return fmt.Errorf("the button took the bytes and has no image")
	}
	if err := icon.SetImageOnly(true); err != nil {
		return err
	}
	if err := icon.SetImage(nil); err != nil {
		return err
	}
	if icon.hasImage() {
		return fmt.Errorf("clearing the image left one behind")
	}
	// A row that does not exist clears the selection instead of pretending.
	if err := tbl.SetDouble(9); err != nil {
		return err
	}
	if got := tbl.Double(); got != -1 {
		return fmt.Errorf("selecting row 9 of a one-row list gave %v, want -1", got)
	}

	tf, err := NewTextField("hello")
	if err != nil {
		return fmt.Errorf("NewTextField: %w", err)
	}
	defer tf.Close()
	if err := tf.SetStringValue("world"); err != nil {
		return err
	}
	if got := tf.StringValue(); got != "world" {
		return fmt.Errorf("TextField value = %q, want world", got)
	}

	sf, err := NewSecureTextField("s3cr3t")
	if err != nil {
		return fmt.Errorf("NewSecureTextField: %w", err)
	}
	defer sf.Close()
	if got := sf.StringValue(); got != "s3cr3t" {
		return fmt.Errorf("SecureTextField value = %q", got)
	}

	lb, err := NewLabel("A label")
	if err != nil {
		return fmt.Errorf("NewLabel: %w", err)
	}
	defer lb.Close()
	if got := lb.StringValue(); got != "A label" {
		return fmt.Errorf("Label value = %q", got)
	}

	cb, err := NewCheckbox("Enable")
	if err != nil {
		return fmt.Errorf("NewCheckbox: %w", err)
	}
	defer cb.Close()
	if err := cb.SetBool(true); err != nil {
		return err
	}
	if !cb.Bool() {
		return fmt.Errorf("Checkbox state = false, want true")
	}

	sw, err := NewSwitch()
	if err != nil {
		return fmt.Errorf("NewSwitch: %w", err)
	}
	defer sw.Close()
	if err := sw.SetBool(true); err != nil {
		return err
	}
	if !sw.Bool() {
		return fmt.Errorf("Switch state = false, want true")
	}

	sl, err := NewSlider(0, 10, 3)
	if err != nil {
		return fmt.Errorf("NewSlider: %w", err)
	}
	defer sl.Close()
	if got := sl.Double(); got != 3 {
		return fmt.Errorf("Slider initial = %v, want 3", got)
	}
	if err := sl.SetDouble(7); err != nil {
		return err
	}
	if got := sl.Double(); got != 7 {
		return fmt.Errorf("Slider value = %v, want 7", got)
	}

	pu, err := NewPopUpButton([]string{"One", "Two", "Three"})
	if err != nil {
		return fmt.Errorf("NewPopUpButton: %w", err)
	}
	defer pu.Close()
	if err := pu.SetStringValue("Two"); err != nil {
		return err
	}
	if got := pu.StringValue(); got != "Two" {
		return fmt.Errorf("PopUp selection = %q, want Two", got)
	}

	if err := runLiveSmokeExtra(); err != nil {
		return err
	}

	// Frame + add to a real parent view + remove: setFrame: (NSRect by value),
	// addSubview:, removeFromSuperview.
	btn, err := NewButton("Go")
	if err != nil {
		return fmt.Errorf("NewButton: %w", err)
	}
	defer btn.Close()
	parent := objc.ClassID("NSView").Send(objc.Sel("alloc")).Send(objc.Sel("init"))
	if parent == 0 {
		return fmt.Errorf("could not create a parent NSView")
	}
	if err := btn.SetFrame(10, 10, 80, 24); err != nil {
		return err
	}
	if err := btn.AddTo(parent); err != nil {
		return err
	}
	if n := int(parent.Send(objc.Sel("subviews")).Send(objc.Sel("count"))); n != 1 {
		return fmt.Errorf("after AddTo, subview count = %d, want 1", n)
	}
	if err := btn.Remove(); err != nil {
		return err
	}
	if n := int(parent.Send(objc.Sel("subviews")).Send(objc.Sel("count"))); n != 0 {
		return fmt.Errorf("after Remove, subview count = %d, want 0", n)
	}
	return nil
}

// runLiveSmokeExtra builds each of the ten controls added on top of the original
// set and round-trips its value through the real AppKit selectors: the double of
// a progress bar and a stepper, the animation state of a spinner, the text of a
// search field, combo box and text view, the selected label of a segmented
// control, the title of a link button, and the two boundary conversions —
// NSDate to an ISO string and NSColor to a hex string — the binding does so a
// host sees only the string.
func runLiveSmokeExtra() error {
	pi, err := NewProgressIndicator(0, 100)
	if err != nil {
		return fmt.Errorf("NewProgressIndicator: %w", err)
	}
	defer pi.Close()
	if got := pi.Double(); got != 0 {
		return fmt.Errorf("ProgressIndicator initial = %v, want 0", got)
	}
	if err := pi.SetDouble(42); err != nil {
		return err
	}
	if got := pi.Double(); got != 42 {
		return fmt.Errorf("ProgressIndicator value = %v, want 42", got)
	}

	sp, err := NewSpinner()
	if err != nil {
		return fmt.Errorf("NewSpinner: %w", err)
	}
	defer sp.Close()
	if err := sp.SetBool(true); err != nil {
		return err
	}
	if !sp.Bool() {
		return fmt.Errorf("Spinner animating = false after start, want true")
	}
	if err := sp.SetBool(false); err != nil {
		return err
	}
	if sp.Bool() {
		return fmt.Errorf("Spinner animating = true after stop, want false")
	}

	st, err := NewStepper(0, 10, 3)
	if err != nil {
		return fmt.Errorf("NewStepper: %w", err)
	}
	defer st.Close()
	if got := st.Double(); got != 3 {
		return fmt.Errorf("Stepper initial = %v, want 3", got)
	}
	if err := st.SetDouble(7); err != nil {
		return err
	}
	if got := st.Double(); got != 7 {
		return fmt.Errorf("Stepper value = %v, want 7", got)
	}

	sf, err := NewSearchField("hi")
	if err != nil {
		return fmt.Errorf("NewSearchField: %w", err)
	}
	defer sf.Close()
	if got := sf.StringValue(); got != "hi" {
		return fmt.Errorf("SearchField initial = %q, want hi", got)
	}
	if err := sf.SetStringValue("bye"); err != nil {
		return err
	}
	if got := sf.StringValue(); got != "bye" {
		return fmt.Errorf("SearchField value = %q, want bye", got)
	}

	cb, err := NewComboBox([]string{"Apple", "Banana"})
	if err != nil {
		return fmt.Errorf("NewComboBox: %w", err)
	}
	defer cb.Close()
	if err := cb.SetStringValue("Cherry"); err != nil {
		return err
	}
	if got := cb.StringValue(); got != "Cherry" {
		return fmt.Errorf("ComboBox value = %q, want Cherry", got)
	}

	sg, err := NewSegmentedControl([]string{"One", "Two", "Three"})
	if err != nil {
		return fmt.Errorf("NewSegmentedControl: %w", err)
	}
	defer sg.Close()
	if err := sg.SetStringValue("Two"); err != nil {
		return err
	}
	if got := sg.StringValue(); got != "Two" {
		return fmt.Errorf("SegmentedControl selection = %q, want Two", got)
	}

	tv, err := NewTextView("hello")
	if err != nil {
		return fmt.Errorf("NewTextView: %w", err)
	}
	defer tv.Close()
	if got := tv.StringValue(); got != "hello" {
		return fmt.Errorf("TextView initial = %q, want hello", got)
	}
	if err := tv.SetStringValue("multi\nline"); err != nil {
		return err
	}
	if got := tv.StringValue(); got != "multi\nline" {
		return fmt.Errorf("TextView value = %q, want multi/line", got)
	}

	lb, err := NewLinkButton("Home")
	if err != nil {
		return fmt.Errorf("NewLinkButton: %w", err)
	}
	defer lb.Close()
	if got := lb.StringValue(); got != "Home" {
		return fmt.Errorf("LinkButton title = %q, want Home", got)
	}
	if err := lb.SetStringValue("Away"); err != nil {
		return err
	}
	if got := lb.StringValue(); got != "Away" {
		return fmt.Errorf("LinkButton title = %q, want Away", got)
	}

	dp, err := NewDatePicker()
	if err != nil {
		return fmt.Errorf("NewDatePicker: %w", err)
	}
	defer dp.Close()
	if err := dp.SetStringValue("2021-03-14"); err != nil {
		return err
	}
	if got := dp.StringValue(); got != "2021-03-14" {
		return fmt.Errorf("DatePicker value = %q, want 2021-03-14", got)
	}

	cw, err := NewColorWell()
	if err != nil {
		return fmt.Errorf("NewColorWell: %w", err)
	}
	defer cw.Close()
	if err := cw.SetStringValue("#1E90FF"); err != nil {
		return err
	}
	if got := cw.StringValue(); got != "#1E90FF" {
		return fmt.Errorf("ColorWell value = %q, want #1E90FF", got)
	}
	return nil
}

// onePixelPNG is the smallest valid PNG: one opaque pixel. Enough for AppKit to
// build an NSImage from, which is all the live test asks of it.
var onePixelPNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
	0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
	0x89, 0x00, 0x00, 0x00, 0x0A, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00,
	0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
	0x42, 0x60, 0x82,
}
