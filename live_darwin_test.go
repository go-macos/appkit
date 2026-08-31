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
