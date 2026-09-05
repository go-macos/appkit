// Copyright (c) the appkit authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package appkit

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"unsafe"

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
//
// selActionChange is a second action selector that dispatches BOTH the action
// and the change handler from one Objective-C action. NSColorWell has no
// text-editing delegate to carry a continuous change, only an action, so its
// action stands in for both OnAction and OnChange.
//
// selChangeText is the NSText/NSTextView delegate notification. NSTextView is
// not an NSControl, so it does not send controlTextDidChange:; its editing
// notification is textDidChange:, and its object cannot carry a -tag, so those
// are routed through textTags rather than the tag on the sender.
var (
	selAction       = objc.Sel("goAppKitAction:")
	selActionChange = objc.Sel("goAppKitActionChange:")
	selChange       = objc.Sel("controlTextDidChange:")
	selChangeText   = objc.Sel("textDidChange:")
	selTag          = objc.Sel("tag")
)

// textTags maps an NSTextView's object pointer to its control tag, because an
// NSTextView cannot carry a -tag the way an NSControl can. It is written when a
// TextView is created and read in the textDidChange: delegate.
var (
	textTagsMu sync.Mutex
	textTags   = map[uintptr]uint64{}
	// tableRows is what each live NSTableView shows. AppKit asks a data source
	// how many rows there are and what is in each; the one shared target
	// answers both, so it must know WHICH table is asking.
	tableRowsMu sync.Mutex
	tableRows   = map[uintptr][]string{}
)

// The FFI leaves are package variables so tests drive every branch — the
// framework-load failure, the class-registration failure, the nil-object
// failure — without needing to actually break AppKit, exactly as
// github.com/go-macos/objc's own dispatch_darwin.go does for libdispatch.
var (
	loadAppKit = func() error { return objc.Load(objc.AppKit, objc.Foundation) }

	registerTargetClass = func() (objc.Class, error) {
		// Declared conformance, not just the methods: -setDataSource: and
		// -setDelegate: on NSTableView check conformsToProtocol:, and a class
		// that merely implements the selectors is refused.
		var protos []*objc.Protocol
		for _, name := range []string{"NSTableViewDataSource", "NSTableViewDelegate"} {
			if p := objc.GetProtocol(name); p != nil {
				protos = append(protos, p)
			}
		}
		return objc.RegisterClassWithProtocols(
			"GoMacOSAppKitTarget",
			objc.GetClass("NSObject"),
			protos,
			[]objc.MethodDef{
				{
					// How many rows this table has.
					Cmd: objc.Sel("numberOfRowsInTableView:"),
					Fn: func(_ objc.ID, _ objc.SEL, tv objc.ID) int {
						return len(rowsOf(tv))
					},
				},
				{
					// What is in one of them. Out-of-range asks happen while a
					// table is being reloaded and must answer empty rather than
					// panic across the FFI boundary, where a Go panic has no
					// frame to unwind into.
					Cmd: objc.Sel("tableView:objectValueForTableColumn:row:"),
					Fn: func(_ objc.ID, _ objc.SEL, tv, _ objc.ID, row int) objc.ID {
						rows := rowsOf(tv)
						if row < 0 || row >= len(rows) {
							return objc.NSString("")
						}
						return objc.NSString(rows[row])
					},
				},
				{
					// The selection moved.
					Cmd: objc.Sel("tableViewSelectionDidChange:"),
					Fn: func(_ objc.ID, _ objc.SEL, note objc.ID) {
						obj := note.Send(objc.Sel("object"))
						dispatchChange(uint64(obj.Send(selTag)))
					},
				},
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
				{
					// One action that fires both handlers, for a control (the
					// colour well) whose only feedback is its action.
					Cmd: selActionChange,
					Fn: func(_ objc.ID, _ objc.SEL, sender objc.ID) {
						tag := uint64(sender.Send(selTag))
						dispatchAction(tag)
						dispatchChange(tag)
					},
				},
				{
					// A menu item was chosen. Menu items carry their own tags,
					// in their own table: a menu is not a control, and giving
					// it one would mean an item and a button could collide on
					// a number.
					Cmd: objc.Sel("goMenuPick:"),
					Fn: func(_ objc.ID, _ objc.SEL, sender objc.ID) {
						dispatchMenu(uint64(sender.Send(selTag)))
					},
				},
				{
					// NSTextView editing: the object is the text view, which
					// carries no tag, so map it through textTags.
					Cmd: selChangeText,
					Fn: func(_ objc.ID, _ objc.SEL, note objc.ID) {
						obj := note.Send(objc.Sel("object"))
						textTagsMu.Lock()
						tag := textTags[uintptr(obj)]
						textTagsMu.Unlock()
						dispatchChange(tag)
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
//
//   - view is the object added to a parent and framed. For a TextView it is the
//     enclosing NSScrollView, not the text view itself.
//   - value is the object that carries the control's value; it is view for every
//     control but TextView, where it is the NSTextView inside the scroll view.
//   - fmtr is the retained NSDateFormatter a DatePicker uses to turn its NSDate
//     into a YYYY-MM-DD string and back; it is 0 for every other kind.
type nativeControl struct {
	view  objc.ID
	value objc.ID
	kind  Kind
	fmtr  objc.ID
	// menuTags are this control's menu items, so their handlers can be
	// forgotten when the menu is replaced or the control closed.
	menuTags  []uint64
	animating bool
}

// platformCreate builds the native control, tags it, and wires its target and
// (for editable text) its delegate to the shared action target.
func platformCreate(spec Spec, tag uint64) (impl, error) {
	if err := ensureTarget(); err != nil {
		return nil, err
	}
	n := &nativeControl{kind: spec.Kind}
	switch spec.Kind {
	case Button:
		n.view = makeButton(spec.Title, 0)
	case Label:
		n.view = makeLabel(spec.Title)
	case TextField:
		n.view = makeTextField("NSTextField", spec.Title)
	case SecureTextField:
		n.view = makeTextField("NSSecureTextField", spec.Title)
	case Checkbox:
		n.view = makeButton(spec.Title, buttonTypeSwitch)
	case RadioButton:
		n.view = makeButton(spec.Title, buttonTypeRadio)
	case Switch:
		n.view = newObject("NSSwitch")
	case Slider:
		n.view = makeSlider(spec.Min, spec.Max, spec.Value)
	case PopUpButton:
		n.view = makePopUp(spec.Items)
	case ProgressIndicator:
		n.view = makeProgress(spec.Min, spec.Max)
	case Spinner:
		n.view = makeSpinner()
	case Stepper:
		n.view = makeStepper(spec.Min, spec.Max, spec.Value)
	case SearchField:
		n.view = makeTextField("NSSearchField", spec.Title)
	case ComboBox:
		n.view = makeComboBox(spec.Items)
	case SegmentedControl:
		n.view = makeSegmented(spec.Items)
	case TextView:
		n.view, n.value = makeTextView(spec.Title)
	case LinkButton:
		n.view = makeLinkButton(spec.Title)
	case DatePicker:
		n.view, n.fmtr = makeDatePicker()
	case ColorWell:
		n.view = newObject("NSColorWell")
	case TableView:
		n.view, n.value = makeTableView(spec.Items)
	}
	// A TextView needs both its scroll view and its text view; a DatePicker needs
	// its formatter. Any zero here is a failed creation.
	if n.view == 0 ||
		((spec.Kind == TextView || spec.Kind == TableView) && n.value == 0) ||
		(spec.Kind == DatePicker && n.fmtr == 0) {
		return nil, fmt.Errorf("appkit: creating %s failed", spec.Kind)
	}
	if n.value == 0 {
		n.value = n.view
	}
	// A -tag is only settable on an NSControl. NSProgressIndicator and the
	// NSScrollView wrapping a TextView are plain NSViews, so tag them only where
	// it is meaningful; the TextView is reached through textTags instead.
	switch spec.Kind {
	case ProgressIndicator, Spinner, TextView:
	case TableView:
		// The scroll view around it is a plain NSView; the table inside is an
		// NSControl, and it is what the selection notification names.
		n.value.Send(objc.Sel("setTag:"), int(tag))
	default:
		n.view.Send(objc.Sel("setTag:"), int(tag))
	}
	n.wire(tag)
	return n, nil
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

// NSProgressIndicatorStyle. Bar is determinate; Spinning is indeterminate.
const (
	progressStyleBar      = 0 // NSProgressIndicatorStyleBar
	progressStyleSpinning = 1 // NSProgressIndicatorStyleSpinning
)

func makeProgress(min, max float64) objc.ID {
	p := newObject("NSProgressIndicator")
	if p == 0 {
		return 0
	}
	p.Send(objc.Sel("setStyle:"), progressStyleBar)
	p.Send(objc.Sel("setIndeterminate:"), false)
	p.Send(objc.Sel("setMinValue:"), min)
	p.Send(objc.Sel("setMaxValue:"), max)
	p.Send(objc.Sel("setDoubleValue:"), min)
	return p
}

func makeSpinner() objc.ID {
	p := newObject("NSProgressIndicator")
	if p == 0 {
		return 0
	}
	p.Send(objc.Sel("setStyle:"), progressStyleSpinning)
	p.Send(objc.Sel("setIndeterminate:"), true)
	p.Send(objc.Sel("setDisplayedWhenStopped:"), true)
	return p
}

func makeStepper(min, max, value float64) objc.ID {
	s := newObject("NSStepper")
	if s == 0 {
		return 0
	}
	s.Send(objc.Sel("setMinValue:"), min)
	s.Send(objc.Sel("setMaxValue:"), max)
	s.Send(objc.Sel("setIncrement:"), 1.0)
	s.Send(objc.Sel("setValueWraps:"), false)
	s.Send(objc.Sel("setDoubleValue:"), value)
	return s
}

func makeComboBox(items []string) objc.ID {
	c := newObject("NSComboBox")
	if c == 0 {
		return 0
	}
	c.Send(objc.Sel("setEditable:"), true)
	for _, it := range items {
		c.Send(objc.Sel("addItemWithObjectValue:"), objc.NSString(it))
	}
	return c
}

// NSSegmentSwitchTrackingSelectOne: exactly one segment is selected at a time,
// which is what makes "the selected segment's label" a single well-defined value.
const segmentTrackingSelectOne = 0

func makeSegmented(items []string) objc.ID {
	s := newObject("NSSegmentedControl")
	if s == 0 {
		return 0
	}
	s.Send(objc.Sel("setSegmentCount:"), len(items))
	s.Send(objc.Sel("setTrackingMode:"), segmentTrackingSelectOne)
	for i, it := range items {
		s.Send(objc.Sel("setLabel:forSegment:"), objc.NSString(it), i)
	}
	if len(items) > 0 {
		s.Send(objc.Sel("setSelectedSegment:"), 0)
	}
	return s
}

// makeTextView builds an editable, plain-text NSTextView inside an NSScrollView
// and returns (scrollView, textView). The scroll view is what the host frames
// and adds; the text view carries the string. Plain (non-rich) text is required
// so that -string round-trips exactly what -setString: was given.
func makeTextView(text string) (objc.ID, objc.ID) {
	tv := newObject("NSTextView")
	if tv == 0 {
		return 0, 0
	}
	tv.Send(objc.Sel("setEditable:"), true)
	tv.Send(objc.Sel("setRichText:"), false)
	tv.Send(objc.Sel("setString:"), objc.NSString(text))
	sv := newObject("NSScrollView")
	if sv == 0 {
		tv.Send(objc.Sel("release"))
		return 0, 0
	}
	sv.Send(objc.Sel("setHasVerticalScroller:"), true)
	sv.Send(objc.Sel("setDocumentView:"), tv) // retains tv
	tv.Send(objc.Sel("release"))              // the scroll view owns it now
	return sv, tv
}

func makeLinkButton(text string) objc.ID {
	b := newObject("NSButton")
	if b == 0 {
		return 0
	}
	b.Send(objc.Sel("setTitle:"), objc.NSString(text))
	b.Send(objc.Sel("setButtonType:"), 0) // momentary
	b.Send(objc.Sel("setBordered:"), false)
	return b
}

// NSDatePickerElementFlagYearMonthDay shows only Y-M-D (no time). Fixing the
// picker and its formatter to GMT keeps a date-only value from drifting across
// midnight when it is turned into a string and back.
const datePickerElementYMD = 0x00e0

func makeDatePicker() (objc.ID, objc.ID) {
	p := newObject("NSDatePicker")
	if p == 0 {
		return 0, 0
	}
	gmt := objc.ClassID("NSTimeZone").Send(objc.Sel("timeZoneForSecondsFromGMT:"), 0)
	p.Send(objc.Sel("setDatePickerElements:"), datePickerElementYMD)
	p.Send(objc.Sel("setTimeZone:"), gmt)
	p.Send(objc.Sel("setDateValue:"), objc.ClassID("NSDate").Send(objc.Sel("date")))

	f := newObject("NSDateFormatter")
	if f == 0 {
		p.Send(objc.Sel("release"))
		return 0, 0
	}
	f.Send(objc.Sel("setDateFormat:"), objc.NSString("yyyy-MM-dd"))
	f.Send(objc.Sel("setTimeZone:"), gmt)
	loc := objc.ClassID("NSLocale").Send(objc.Sel("localeWithLocaleIdentifier:"), objc.NSString("en_US_POSIX"))
	f.Send(objc.Sel("setLocale:"), loc)
	f.Send(objc.Sel("retain")) // held for the life of the control
	return p, f
}

// wire points a control's target/action — and for editable text, its delegate —
// at the shared target, so operating it reaches dispatchAction/dispatchChange.
// The valueless read-outs (Label, ProgressIndicator, Spinner) are left unwired:
// they have no action and report nothing back.
func (n *nativeControl) wire(tag uint64) {
	switch n.kind {
	case Label, ProgressIndicator, Spinner:
		return
	case TextField, SecureTextField, SearchField, ComboBox:
		// Editable text: action on Return, delegate for per-keystroke change.
		n.view.Send(objc.Sel("setTarget:"), target)
		n.view.Send(objc.Sel("setAction:"), selAction)
		n.view.Send(objc.Sel("setDelegate:"), target)
	case ColorWell:
		// No editing delegate; its action carries both action and change.
		n.view.Send(objc.Sel("setTarget:"), target)
		n.view.Send(objc.Sel("setAction:"), selActionChange)
	case TableView:
		// AppKit pulls the rows from a data source and reports the selection
		// to a delegate. Both are the shared target, which answers for every
		// table by looking the asking one up.
		n.value.Send(objc.Sel("setDataSource:"), target)
		n.value.Send(objc.Sel("setDelegate:"), target)
	case TextView:
		// Not an NSControl: only an NSText change notification, keyed by object.
		textTagsMu.Lock()
		textTags[uintptr(n.value)] = tag
		textTagsMu.Unlock()
		n.value.Send(objc.Sel("setDelegate:"), target)
	default:
		n.view.Send(objc.Sel("setTarget:"), target)
		n.view.Send(objc.Sel("setAction:"), selAction)
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
	// A table has no value of its own; what a caller wants from it is which
	// row is chosen, and -1 when none is. That rides Double rather than a
	// method of its own so a list binds through the same seam as a slider.
	if n.kind == TableView {
		return float64(objc.Send[int](n.value, objc.Sel("selectedRow")))
	}
	return objc.Send[float64](n.view, objc.Sel("doubleValue"))
}

func (n *nativeControl) setDouble(v float64) {
	if n.kind == TableView {
		n.selectRow(int(v))
		return
	}
	n.view.Send(objc.Sel("setDoubleValue:"), v)
}

// release frees the native object. A TextView is dropped from textTags first; a
// DatePicker's retained formatter is released too.
func (n *nativeControl) release() {
	n.dropMenu()
	if n.kind == TableView {
		tableRowsMu.Lock()
		delete(tableRows, uintptr(n.value))
		tableRowsMu.Unlock()
	}
	if n.kind == TextView {
		textTagsMu.Lock()
		delete(textTags, uintptr(n.value))
		textTagsMu.Unlock()
	}
	if n.fmtr != 0 {
		n.fmtr.Send(objc.Sel("release"))
	}
	n.view.Send(objc.Sel("release"))
}

// boolValue/setBool go through -state / -setState:, which is how NSButton
// (checkbox, radio) and NSSwitch both carry on/off. NSControlStateValueOn is 1,
// Off is 0. For a Spinner there is no state: setBool starts or stops the
// animation, and boolValue reports whether it is currently animating.
func (n *nativeControl) boolValue() bool {
	if n.kind == Spinner {
		return n.animating
	}
	return int(n.view.Send(objc.Sel("state"))) != 0
}

// setItems replaces what a list shows and asks it to redraw.
//
// The rows live beside the control rather than in it, because AppKit pulls
// them from a data source instead of holding them: the table is asked, every
// time it draws, what row 12 contains.
func (n *nativeControl) setItems(items []string) {
	switch n.kind {
	case TableView:
		tableRowsMu.Lock()
		tableRows[uintptr(n.value)] = append([]string(nil), items...)
		tableRowsMu.Unlock()
		n.value.Send(objc.Sel("reloadData"))
	case PopUpButton:
		n.view.Send(objc.Sel("removeAllItems"))
		for _, it := range items {
			n.view.Send(objc.Sel("addItemWithTitle:"), objc.NSString(it))
		}
	case ComboBox:
		n.view.Send(objc.Sel("removeAllItems"))
		for _, it := range items {
			n.view.Send(objc.Sel("addItemWithObjectValue:"), objc.NSString(it))
		}
	}
}

// selectRow moves the selection of a table, or clears it for a row outside it.
func (n *nativeControl) selectRow(row int) {
	if row < 0 || row >= len(rowsOf(n.value)) {
		n.value.Send(objc.Sel("deselectAll:"), objc.ID(0))
		return
	}
	set := objc.ClassID("NSIndexSet").Send(objc.Sel("indexSetWithIndex:"), row)
	n.value.Send(objc.Sel("selectRowIndexes:byExtendingSelection:"), set, false)
	n.value.Send(objc.Sel("scrollRowToVisible:"), row)
}

// columnsEditable asks the live table whether any of its columns is editable.
func (n *nativeControl) columnsEditable() bool {
	if n.kind != TableView {
		return false
	}
	cols := n.value.Send(objc.Sel("tableColumns"))
	if cols == 0 {
		return false
	}
	for i, count := 0, objc.Send[int](cols, objc.Sel("count")); i < count; i++ {
		c := cols.Send(objc.Sel("objectAtIndex:"), i)
		if c != 0 && objc.Send[bool](c, objc.Sel("isEditable")) {
			return true
		}
	}
	return false
}

// setMenu builds an NSMenu and hangs it off the control's view.
//
// The items are retained by the menu, and the menu by the view, so nothing
// here is released early. The Go funcs live in menuPicks until the control is
// closed -- a menu item that outlived its handler would be a line that looks
// like a verb and does nothing.
func (n *nativeControl) setMenu(items []MenuItem) {
	n.dropMenu()
	if len(items) == 0 {
		n.view.Send(objc.Sel("setMenu:"), objc.ID(0))
		return
	}
	menu := newObject("NSMenu")
	if menu == 0 {
		return
	}
	// The table's own rows must stay selectable while the menu is up.
	menu.Send(objc.Sel("setAutoenablesItems:"), false)
	for _, it := range items {
		var item objc.ID
		if it.Title == "" {
			item = objc.ClassID("NSMenuItem").Send(objc.Sel("separatorItem"))
			if item == 0 {
				continue
			}
			menu.Send(objc.Sel("addItem:"), item)
			continue
		}
		item = newObject("NSMenuItem")
		if item == 0 {
			continue
		}
		item.Send(objc.Sel("setTitle:"), objc.NSString(it.Title))
		item.Send(objc.Sel("setEnabled:"), it.OnPick != nil)
		if it.OnPick != nil {
			tag := tagSeq.Add(1)
			menuMu.Lock()
			menuPicks[tag] = it.OnPick
			menuMu.Unlock()
			n.menuTags = append(n.menuTags, tag)
			item.Send(objc.Sel("setTag:"), int(tag))
			item.Send(objc.Sel("setTarget:"), target)
			item.Send(objc.Sel("setAction:"), objc.Sel("goMenuPick:"))
		}
		menu.Send(objc.Sel("addItem:"), item)
		item.Send(objc.Sel("release")) // the menu owns it now
	}
	n.view.Send(objc.Sel("setMenu:"), menu)
	menu.Send(objc.Sel("release")) // the view owns it now
}

// dropMenu forgets the handlers of a menu being replaced.
func (n *nativeControl) dropMenu() {
	if len(n.menuTags) == 0 {
		return
	}
	menuMu.Lock()
	for _, t := range n.menuTags {
		delete(menuPicks, t)
	}
	menuMu.Unlock()
	n.menuTags = nil
}

// menuCount is how many items the live menu holds.
func (n *nativeControl) menuCount() int {
	m := n.view.Send(objc.Sel("menu"))
	if m == 0 {
		return 0
	}
	return objc.Send[int](m, objc.Sel("numberOfItems"))
}

// setImage puts a picture on the control, from encoded bytes.
//
// NSImage is created from NSData rather than a file: the caller has bytes --
// an icon compiled into the program -- and writing them to a temporary file so
// AppKit can read them back is a round trip through the disk for nothing.
func (n *nativeControl) setImage(png []byte) {
	if len(png) == 0 {
		n.view.Send(objc.Sel("setImage:"), objc.ID(0))
		return
	}
	data := objc.ClassID("NSData").Send(objc.Sel("dataWithBytes:length:"),
		unsafe.Pointer(&png[0]), len(png))
	if data == 0 {
		return
	}
	img := objc.ClassID("NSImage").Send(objc.Sel("alloc")).
		Send(objc.Sel("initWithData:"), data)
	if img == 0 {
		return
	}
	// A template image takes the system's own tint -- so it is dark on a light
	// toolbar and light on a dark one, without the caller shipping two icons.
	img.Send(objc.Sel("setTemplate:"), true)
	n.view.Send(objc.Sel("setImage:"), img)
	img.Send(objc.Sel("release")) // the control retains it
}

// setImageOnly hides the title without unsetting it: the title is what a
// screen reader announces, so an icon-only button that dropped it would be a
// button nobody using assistive technology could name.
func (n *nativeControl) setImageOnly(only bool) {
	// NSImageOnly is 1; NSImageLeft is 2, which is what a button with both
	// shows.
	pos := 2
	if only {
		pos = 1
	}
	n.view.Send(objc.Sel("setImagePosition:"), pos)
}

// imageSet reports whether the live control carries an image.
func (n *nativeControl) imageSet() bool {
	return n.view.Send(objc.Sel("image")) != 0
}

func (n *nativeControl) setBool(on bool) {
	if n.kind == Spinner {
		n.animating = on
		if on {
			n.view.Send(objc.Sel("startAnimation:"), objc.ID(0))
		} else {
			n.view.Send(objc.Sel("stopAnimation:"), objc.ID(0))
		}
		return
	}
	state := 0
	if on {
		state = 1
	}
	n.view.Send(objc.Sel("setState:"), state)
}

// stringValue reads a control's value as a string, converting at the boundary
// for the controls whose native value is not itself a string: a pop-up's or
// segment's selection, a date, a colour, the text of a text view or the title of
// a link button.
func (n *nativeControl) stringValue() string {
	switch n.kind {
	case PopUpButton:
		return objc.GoString(n.view.Send(objc.Sel("titleOfSelectedItem")))
	case SegmentedControl:
		idx := int(n.view.Send(objc.Sel("selectedSegment")))
		if idx < 0 {
			return ""
		}
		return objc.GoString(n.view.Send(objc.Sel("labelForSegment:"), idx))
	case TextView:
		return objc.GoString(n.value.Send(objc.Sel("string")))
	case LinkButton:
		return objc.GoString(n.view.Send(objc.Sel("title")))
	case DatePicker:
		date := n.view.Send(objc.Sel("dateValue"))
		if date == 0 {
			return ""
		}
		return objc.GoString(n.fmtr.Send(objc.Sel("stringFromDate:"), date))
	case ColorWell:
		return colorToHex(n.view.Send(objc.Sel("color")))
	default:
		return objc.GoString(n.view.Send(objc.Sel("stringValue")))
	}
}

func (n *nativeControl) setStringValue(s string) {
	switch n.kind {
	case PopUpButton:
		n.view.Send(objc.Sel("selectItemWithTitle:"), objc.NSString(s))
	case SegmentedControl:
		count := int(n.view.Send(objc.Sel("segmentCount")))
		for i := 0; i < count; i++ {
			if objc.GoString(n.view.Send(objc.Sel("labelForSegment:"), i)) == s {
				n.view.Send(objc.Sel("setSelectedSegment:"), i)
				return
			}
		}
	case TextView:
		n.value.Send(objc.Sel("setString:"), objc.NSString(s))
	case LinkButton:
		n.view.Send(objc.Sel("setTitle:"), objc.NSString(s))
	case DatePicker:
		if date := n.fmtr.Send(objc.Sel("dateFromString:"), objc.NSString(s)); date != 0 {
			n.view.Send(objc.Sel("setDateValue:"), date)
		}
	case ColorWell:
		if c := hexToColor(s); c != 0 {
			n.view.Send(objc.Sel("setColor:"), c)
		}
	default:
		n.view.Send(objc.Sel("setStringValue:"), objc.NSString(s))
	}
}

// colorToHex renders an NSColor as #RRGGBB. It first converts into the sRGB
// space, because -redComponent and its kin are only valid on an RGB colour and
// raise on a catalog or pattern colour; a colour that will not convert yields
// black.
func colorToHex(c objc.ID) string {
	if c == 0 {
		return "#000000"
	}
	space := objc.ClassID("NSColorSpace").Send(objc.Sel("sRGBColorSpace"))
	rgb := c.Send(objc.Sel("colorUsingColorSpace:"), space)
	if rgb == 0 {
		return "#000000"
	}
	r := objc.Send[float64](rgb, objc.Sel("redComponent"))
	g := objc.Send[float64](rgb, objc.Sel("greenComponent"))
	b := objc.Send[float64](rgb, objc.Sel("blueComponent"))
	return fmt.Sprintf("#%02X%02X%02X", clamp8(r), clamp8(g), clamp8(b))
}

// clamp8 turns a 0..1 component into a 0..255 byte, rounding and clamping.
func clamp8(v float64) int {
	n := int(math.Round(v * 255))
	if n < 0 {
		return 0
	}
	if n > 255 {
		return 255
	}
	return n
}

// hexToColor parses #RRGGBB (with or without the leading #) into an sRGB
// NSColor, returning 0 if it is not a valid six-digit hex colour so the caller
// leaves the well unchanged.
func hexToColor(s string) objc.ID {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return 0
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0
	}
	r := float64((v>>16)&0xff) / 255
	g := float64((v>>8)&0xff) / 255
	b := float64(v&0xff) / 255
	return objc.ClassID("NSColor").Send(objc.Sel("colorWithSRGBRed:green:blue:alpha:"), r, g, b, 1.0)
}

// rowsOf is what the table view tv is showing.
func rowsOf(tv objc.ID) []string {
	tableRowsMu.Lock()
	defer tableRowsMu.Unlock()
	return tableRows[uintptr(tv)]
}

// makeTableView builds an NSTableView of one text column inside an
// NSScrollView, and returns the pair: the scroll view is what a host adds and
// frames, the table view is what carries the value and the tag.
//
// One column, no header: this is a list, and a person reading a queue wants
// the rows, not a column title to sort by. The scroll view is what makes it a
// list rather than a clipped rectangle -- AppKit's own scrolling, its elastic
// bounce, its scroller that hides itself.
func makeTableView(items []string) (objc.ID, objc.ID) {
	tv := newObject("NSTableView")
	if tv == 0 {
		return 0, 0
	}
	col := newObject("NSTableColumn")
	if col == 0 {
		tv.Send(objc.Sel("release"))
		return 0, 0
	}
	col.Send(objc.Sel("setIdentifier:"), objc.NSString("row"))
	// NOT editable. A cell-based NSTableView hands each row an NSTextFieldCell,
	// and that cell is editable unless its column says otherwise: clicking a
	// row opened a text field over it, as though a list of downloads were
	// something a person types into. Reported as "the line looks editable",
	// which is exactly what it was.
	col.Send(objc.Sel("setEditable:"), false)
	tv.Send(objc.Sel("addTableColumn:"), col)
	col.Send(objc.Sel("release")) // the table owns it now
	tv.Send(objc.Sel("setHeaderView:"), objc.ID(0))
	tv.Send(objc.Sel("setUsesAlternatingRowBackgroundColors:"), true)
	tv.Send(objc.Sel("setAllowsMultipleSelection:"), false)
	// A column nobody can select either: this is a list of one column, and
	// selecting the column is a gesture with nothing behind it.
	tv.Send(objc.Sel("setAllowsColumnSelection:"), false)

	tableRowsMu.Lock()
	tableRows[uintptr(tv)] = append([]string(nil), items...)
	tableRowsMu.Unlock()

	sv := newObject("NSScrollView")
	if sv == 0 {
		tableRowsMu.Lock()
		delete(tableRows, uintptr(tv))
		tableRowsMu.Unlock()
		tv.Send(objc.Sel("release"))
		return 0, 0
	}
	sv.Send(objc.Sel("setHasVerticalScroller:"), true)
	sv.Send(objc.Sel("setAutohidesScrollers:"), true)
	sv.Send(objc.Sel("setDocumentView:"), tv) // retains tv
	tv.Send(objc.Sel("release"))              // the scroll view owns it now
	return sv, tv
}
