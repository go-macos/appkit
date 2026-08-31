// Copyright (c) the appkit authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package appkit

import (
	"errors"
	"fmt"
	"sync"

	"github.com/go-macos/objc"
)

// errNoTargetClass is the internal failure when the one process-wide action
// target class cannot be registered or instantiated. It never escapes to a
// caller unwrapped; platformCreate wraps it with the kind being built.
var errNoTargetClass = errors.New("appkit: could not create the action target")

// The selectors of the runtime class registered by ensureTarget.
//
//   - selAction is namespaced, because a bare -fire: would collide with whatever
//     else is linked into the host application.
//   - selChange is NOT namespaced: it is the delegate method AppKit itself calls
//     on an NSControl's delegate, so it must be spelt exactly. It is safe on our
//     own target class, which is the delegate of nothing but our own fields.
var (
	selAction = objc.Sel("goAppKitAction:")
	selChange = objc.Sel("controlTextDidChange:")
	selTag    = objc.Sel("tag")
)

// The FFI leaves are package variables so tests drive every branch — the
// framework-load failure, the class-registration failure, the nil-object
// failure — without needing to actually break AppKit, exactly as
// github.com/go-macos/objc's own dispatch_darwin.go does for libdispatch.
var (
	loadAppKit = func() error { return objc.Load(objc.AppKit, objc.Foundation) }

	registerTargetClass = func() (objc.Class, error) {
		return objc.RegisterClass(
			"GoMacOSAppKitTarget",
			objc.GetClass("NSObject"),
			[]objc.MethodDef{
				// MethodDef.Fn is the raw Go func; RegisterClass wraps it into an
				// IMP itself, so it must not be pre-wrapped here.
				{
					Cmd: selAction,
					Fn: func(_ objc.ID, _ objc.SEL, sender objc.ID) {
						dispatchAction(uint64(sender.Send(selTag)))
					},
				},
				{
					Cmd: selChange,
					Fn: func(_ objc.ID, _ objc.SEL, note objc.ID) {
						obj := note.Send(objc.Sel("object"))
						dispatchChange(uint64(obj.Send(selTag)))
					},
				},
			},
		)
	}

	// newObject allocs and inits an instance of the named class. It is the one
	// place a nil object can come from, so a test swaps it to return 0 and reach
	// the "creation failed" branch without a broken AppKit.
	newObject = func(class string) objc.ID {
		return objc.ClassID(class).Send(objc.Sel("alloc")).Send(objc.Sel("init"))
	}
)

var (
	classOnce sync.Once
	classErr  error
	target    objc.ID // the shared target/delegate; retained for the process life
)

// ensureTarget loads AppKit and registers-and-instantiates the one action
// target this package needs, once per process.
//
// It must be once: objc_allocateClassPair refuses a duplicate class name and
// returns nil, so a second registration would hand back a nil class whose
// -alloc quietly yields nil, and every control would then have a nil target — a
// control that draws perfectly and does nothing when operated.
func ensureTarget() error {
	classOnce.Do(func() {
		if err := loadAppKit(); err != nil {
			classErr = fmt.Errorf("appkit: loading AppKit: %w", err)
			return
		}
		cls, err := registerTargetClass()
		if err != nil {
			classErr = fmt.Errorf("appkit: registering the action target class: %w", err)
			return
		}
		if cls == 0 {
			classErr = errNoTargetClass
			return
		}
		t := objc.ID(cls).Send(objc.Sel("alloc")).Send(objc.Sel("init"))
		if t == 0 {
			classErr = errNoTargetClass
			return
		}
		// The target outlives every reference AppKit holds to it (each control
		// points its target/delegate at it), so it is retained here and never
		// released.
		t.Send(objc.Sel("retain"))
		target = t
	})
	return classErr
}

// nativeControl is the darwin [impl]: one live NSControl (or NSView) addressed
// by Objective-C message send.
type nativeControl struct {
	view objc.ID
	kind Kind
}

// platformCreate builds the native control, tags it, and wires its target and
// (for text fields) its delegate to the shared action target.
func platformCreate(spec Spec, tag uint64) (impl, error) {
	if err := ensureTarget(); err != nil {
		return nil, err
	}
	var view objc.ID
	switch spec.Kind {
	case Button:
		view = makeButton(spec.Title, 0)
	case Label:
		view = makeLabel(spec.Title)
	case TextField:
		view = makeTextField("NSTextField", spec.Title)
	case SecureTextField:
		view = makeTextField("NSSecureTextField", spec.Title)
	case Checkbox:
		view = makeButton(spec.Title, buttonTypeSwitch)
	case RadioButton:
		view = makeButton(spec.Title, buttonTypeRadio)
	case Switch:
		view = newObject("NSSwitch")
	case Slider:
		view = makeSlider(spec.Min, spec.Max, spec.Value)
	case PopUpButton:
		view = makePopUp(spec.Items)
	}
	if view == 0 {
		return nil, fmt.Errorf("appkit: creating %s failed", spec.Kind)
	}
	view.Send(objc.Sel("setTag:"), int(tag))
	wire(view, spec.Kind)
	return &nativeControl{view: view, kind: spec.Kind}, nil
}

// NSButton button types (AppKit's NSButtonType). 0 is left as the default
// momentary push for a plain Button.
const (
	buttonTypeSwitch = 3 // NSButtonTypeSwitch
	buttonTypeRadio  = 4 // NSButtonTypeRadio
)

func makeButton(title string, buttonType int) objc.ID {
	b := newObject("NSButton")
	if b == 0 {
		return 0
	}
	b.Send(objc.Sel("setTitle:"), objc.NSString(title))
	if buttonType != 0 {
		b.Send(objc.Sel("setButtonType:"), buttonType)
	}
	return b
}

func makeLabel(text string) objc.ID {
	t := newObject("NSTextField")
	if t == 0 {
		return 0
	}
	t.Send(objc.Sel("setStringValue:"), objc.NSString(text))
	t.Send(objc.Sel("setEditable:"), false)
	t.Send(objc.Sel("setSelectable:"), false)
	t.Send(objc.Sel("setBezeled:"), false)
	t.Send(objc.Sel("setDrawsBackground:"), false)
	return t
}

func makeTextField(class, text string) objc.ID {
	t := newObject(class)
	if t == 0 {
		return 0
	}
	t.Send(objc.Sel("setStringValue:"), objc.NSString(text))
	return t
}

func makeSlider(min, max, value float64) objc.ID {
	s := newObject("NSSlider")
	if s == 0 {
		return 0
	}
	s.Send(objc.Sel("setMinValue:"), min)
	s.Send(objc.Sel("setMaxValue:"), max)
	s.Send(objc.Sel("setDoubleValue:"), value)
	return s
}

func makePopUp(items []string) objc.ID {
	p := newObject("NSPopUpButton")
	if p == 0 {
		return 0
	}
	for _, it := range items {
		p.Send(objc.Sel("addItemWithTitle:"), objc.NSString(it))
	}
	return p
}

// wire points a control's target/action — and for editable text, its delegate —
// at the shared target, so operating it reaches dispatchAction/dispatchChange.
// A Label is left unwired: it has no action and reads back nothing.
func wire(view objc.ID, kind Kind) {
	switch kind {
	case Label:
		return
	case TextField, SecureTextField:
		view.Send(objc.Sel("setTarget:"), target)
		view.Send(objc.Sel("setAction:"), selAction)
		view.Send(objc.Sel("setDelegate:"), target)
	default:
		view.Send(objc.Sel("setTarget:"), target)
		view.Send(objc.Sel("setAction:"), selAction)
	}
}

func (n *nativeControl) setFrame(x, y, w, h float64) {
	n.view.Send(objc.Sel("setFrame:"), objc.NSRect{
		Origin: objc.NSPoint{X: x, Y: y},
		Size:   objc.NSSize{Width: w, Height: h},
	})
}

func (n *nativeControl) addTo(parent objc.ID)  { parent.Send(objc.Sel("addSubview:"), n.view) }
func (n *nativeControl) removeFromParent()     { n.view.Send(objc.Sel("removeFromSuperview")) }
func (n *nativeControl) setHidden(hidden bool) { n.view.Send(objc.Sel("setHidden:"), hidden) }
func (n *nativeControl) doubleValue() float64 {
	return objc.Send[float64](n.view, objc.Sel("doubleValue"))
}
func (n *nativeControl) setDouble(v float64) { n.view.Send(objc.Sel("setDoubleValue:"), v) }
func (n *nativeControl) release()            { n.view.Send(objc.Sel("release")) }

// boolValue/setBool go through -state / -setState:, which is how NSButton
// (checkbox, radio) and NSSwitch both carry on/off. NSControlStateValueOn is 1,
// Off is 0.
func (n *nativeControl) boolValue() bool { return int(n.view.Send(objc.Sel("state"))) != 0 }

func (n *nativeControl) setBool(on bool) {
	state := 0
	if on {
		state = 1
	}
	n.view.Send(objc.Sel("setState:"), state)
}

// stringValue reads a text control's contents; for a pop-up it is the selected
// item's title, which is what a caller means by "the value" of a pop-up.
func (n *nativeControl) stringValue() string {
	if n.kind == PopUpButton {
		return objc.GoString(n.view.Send(objc.Sel("titleOfSelectedItem")))
	}
	return objc.GoString(n.view.Send(objc.Sel("stringValue")))
}

func (n *nativeControl) setStringValue(s string) {
	if n.kind == PopUpButton {
		n.view.Send(objc.Sel("selectItemWithTitle:"), objc.NSString(s))
		return
	}
	n.view.Send(objc.Sel("setStringValue:"), objc.NSString(s))
}
