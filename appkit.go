// Copyright (c) the appkit authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package appkit

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/go-macos/objc"
)

// ErrUnsupported is returned by every constructor away from macOS: the controls
// this package embeds are AppKit's, and there is no AppKit to embed them in.
var ErrUnsupported = errors.New("appkit: native controls are only available on macOS")

// ErrClosed is returned when a method is called on a control that has already
// been closed. A closed control's native object is gone; touching it would
// message freed memory.
var ErrClosed = errors.New("appkit: control is closed")

// Kind is which native control a [Control] wraps. It is fixed at construction:
// AppKit has no single control that becomes a button or a slider after the fact,
// so neither does this package.
type Kind int

const (
	// Button is an NSButton with a push-button style — a momentary control that
	// fires its action when clicked.
	Button Kind = iota
	// Label is a non-editable, non-selectable NSTextField: text the host places
	// but the person cannot change. It has no action.
	Label
	// TextField is an editable NSTextField. Its action fires when editing ends
	// (Return, or focus leaving the field); OnChange fires on every keystroke.
	TextField
	// SecureTextField is an NSSecureTextField: an editable field whose glyphs
	// are bullets and whose contents the window server fills without this
	// process seeing the keystrokes. This is the control that cannot be drawn.
	SecureTextField
	// Checkbox is an NSButton with a switch (checkbox) style: an on/off control
	// with a label. Its state is [Control.Bool].
	Checkbox
	// RadioButton is an NSButton with a radio style. AppKit groups radio buttons
	// that share a superview and action, so that selecting one clears its
	// siblings; give a group of RadioButtons the same parent to get that.
	RadioButton
	// Switch is an NSSwitch, the sliding on/off control introduced in macOS
	// 10.15. Its state is [Control.Bool].
	Switch
	// Slider is an NSSlider over a [Spec.Min],[Spec.Max] range. Its value is
	// [Control.Double]; OnChange fires as it is dragged.
	Slider
	// PopUpButton is an NSPopUpButton: a pull-down list of [Spec.Items]. The
	// selected title is [Control.StringValue]; its action fires on selection.
	PopUpButton

	// ProgressIndicator is a determinate NSProgressIndicator (a bar). Its value
	// is [Control.Double], clamped to the [Spec.Min],[Spec.Max] range fixed at
	// construction. It is read-only: it has no action, because a progress bar
	// reports, it is not operated.
	ProgressIndicator
	// Spinner is an indeterminate, spinning NSProgressIndicator. It carries no
	// value; [Control.SetBool] starts (true) and stops (false) its animation.
	Spinner
	// Stepper is an NSStepper over [Spec.Min],[Spec.Max] starting at
	// [Spec.Value]. Its value is [Control.Double]; its action fires on each step.
	Stepper
	// SearchField is an NSSearchField: an editable field styled for search. Its
	// text is [Control.StringValue]; OnChange fires on every keystroke and its
	// action fires on Return.
	SearchField
	// ComboBox is an editable NSComboBox: a text field with a drop-down list of
	// [Spec.Items]. The typed-or-picked text is [Control.StringValue]; OnChange
	// fires on a keystroke, its action on Return or a pick.
	ComboBox
	// SegmentedControl is an NSSegmentedControl over [Spec.Items]. The selected
	// segment's label is [Control.StringValue] (the binding maps label to index);
	// its action fires when the selection changes.
	SegmentedControl
	// TextView is a multi-line, editable NSTextView (inside an NSScrollView). Its
	// text is [Control.StringValue]; OnChange fires as it is edited. It has no
	// action.
	TextView
	// LinkButton is an NSButton styled as a hyperlink. Its title is
	// [Control.StringValue]; its action fires when it is clicked, which is where
	// a host opens the link.
	LinkButton
	// DatePicker is an NSDatePicker. Its value is [Control.StringValue] as an
	// ISO-8601 YYYY-MM-DD date string (the binding parses and formats at the
	// boundary); its action fires when the date changes.
	DatePicker
	// ColorWell is an NSColorWell. Its value is [Control.StringValue] as a
	// #RRGGBB hex string (the binding converts to and from NSColor); its action
	// fires when the colour changes.
	ColorWell

	// kindCount bounds validation. Keep it last.
	kindCount
)

// String names the kind for error messages and logs.
func (k Kind) String() string {
	switch k {
	case Button:
		return "Button"
	case Label:
		return "Label"
	case TextField:
		return "TextField"
	case SecureTextField:
		return "SecureTextField"
	case Checkbox:
		return "Checkbox"
	case RadioButton:
		return "RadioButton"
	case Switch:
		return "Switch"
	case Slider:
		return "Slider"
	case PopUpButton:
		return "PopUpButton"
	case ProgressIndicator:
		return "ProgressIndicator"
	case Spinner:
		return "Spinner"
	case Stepper:
		return "Stepper"
	case SearchField:
		return "SearchField"
	case ComboBox:
		return "ComboBox"
	case SegmentedControl:
		return "SegmentedControl"
	case TextView:
		return "TextView"
	case LinkButton:
		return "LinkButton"
	case DatePicker:
		return "DatePicker"
	case ColorWell:
		return "ColorWell"
	default:
		return fmt.Sprintf("Kind(%d)", int(k))
	}
}

// Spec describes a control to build. Most callers reach for a constructor
// ([NewButton], [NewSlider], …) rather than filling this in by hand; it is
// exported so a host that maps a laid-out region to a control kind at run time
// can build one from data.
type Spec struct {
	Kind Kind

	// Title is the label of a Button, Checkbox or RadioButton, the initial text
	// of a Label, TextField, SecureTextField, SearchField or TextView, and the
	// title of a LinkButton. It is ignored for the valueless controls (Switch,
	// Slider, PopUpButton, and the rest).
	Title string

	// Items are the entries of a PopUpButton or ComboBox drop-down, or the
	// segments of a SegmentedControl, in order. A PopUpButton or SegmentedControl
	// with no items is refused: neither can be operated empty. A ComboBox may be
	// empty — it is still a typeable text field.
	Items []string

	// Min, Max and Value are the bounds and initial position of a Slider or
	// Stepper, and the bounds of a ProgressIndicator (whose initial value is
	// Min). Min must be < Max. Value is clamped into the range. Ignored
	// otherwise.
	Min, Max, Value float64
}

func (s Spec) validate() error {
	if s.Kind < 0 || s.Kind >= kindCount {
		return fmt.Errorf("appkit: unknown control kind %s", s.Kind)
	}
	if s.Kind == PopUpButton && len(s.Items) == 0 {
		return errors.New("appkit: a PopUpButton needs at least one item")
	}
	if s.Kind == SegmentedControl && len(s.Items) == 0 {
		return errors.New("appkit: a SegmentedControl needs at least one item")
	}
	if (s.Kind == Slider || s.Kind == Stepper || s.Kind == ProgressIndicator) && !(s.Min < s.Max) {
		return fmt.Errorf("appkit: %s needs Min < Max, got Min=%g Max=%g", s.Kind, s.Min, s.Max)
	}
	return nil
}

// impl is the seam between the portable half of this package and the AppKit
// half: one live native control, addressed only through these methods. The
// darwin build supplies the real implementation; the non-darwin build supplies
// none (its constructors never get this far). Tests replace [create] with a
// factory returning a fake impl, which is how every line below runs on a Linux
// runner with no window server.
//
// Every method here is called on the process main thread, inside an autorelease
// pool, by the wrapper in [Control].
type impl interface {
	setFrame(x, y, w, h float64)
	addTo(parent objc.ID)
	removeFromParent()
	setHidden(hidden bool)
	stringValue() string
	setStringValue(s string)
	doubleValue() float64
	setDouble(v float64)
	boolValue() bool
	setBool(on bool)
	release()
}

// create builds a native control of the given kind, tagging it with tag so that
// an action arriving from AppKit can be routed back to the owning [Control]. It
// is a package variable, not a plain call, so tests substitute a fake that
// needs no AppKit; [platformCreate] is the real one, and is [ErrUnsupported]
// off darwin.
var create = platformCreate

// The action registry maps a control's tag to the control, so that the one
// process-wide Objective-C target — which cannot close over Go state — can find
// the Go handler for whichever control was clicked. A captured closure will not
// do: an Objective-C method is a C function pointer with no room for a Go
// closure environment, so the only thing the target has to go on is the tag it
// reads off the sender.
var (
	regMu  sync.Mutex
	reg    = map[uint64]*Control{}
	tagSeq atomic.Uint64
)

// dispatchAction is called by the AppKit target when a control fires its action
// (a button clicked, editing ended, a pop-up selection made). It runs on the
// main thread, where AppKit delivered the action, so the handler does too.
func dispatchAction(tag uint64) {
	regMu.Lock()
	c := reg[tag]
	regMu.Unlock()
	if c == nil {
		return
	}
	if fn := c.action(); fn != nil {
		fn()
	}
}

// dispatchChange is called by the AppKit delegate when a control's value changes
// continuously (a keystroke in a text field, a slider dragged). It also runs on
// the main thread.
func dispatchChange(tag uint64) {
	regMu.Lock()
	c := reg[tag]
	regMu.Unlock()
	if c == nil {
		return
	}
	if fn := c.change(); fn != nil {
		fn()
	}
}

// Control is one live native AppKit control. Its zero value is not usable; get
// one from a constructor.
//
// Every method must be called on the process main thread — the thread that runs
// the AppKit event loop — because AppKit permits control creation and mutation
// only there. A host such as github.com/go-widgets/window drives its layout and
// event handling on that thread already, which is where it places and updates
// these controls. Calling from another goroutine is a bug this package does not
// guard against; the action and change handlers, in turn, are invoked on the
// main thread because that is where AppKit delivers them.
type Control struct {
	kind Kind
	tag  uint64

	mu       sync.Mutex
	closed   bool
	im       impl
	onAction func()
	onChange func()
}

// New builds a control from a full [Spec]. The constructors below are thin
// wrappers over it and are what most callers want.
func New(spec Spec) (*Control, error) {
	if err := spec.validate(); err != nil {
		return nil, err
	}
	tag := tagSeq.Add(1)
	im, err := create(spec, tag)
	if err != nil {
		return nil, err
	}
	c := &Control{kind: spec.Kind, tag: tag, im: im}
	regMu.Lock()
	reg[tag] = c
	regMu.Unlock()
	return c, nil
}

// NewButton makes a push button with the given title.
func NewButton(title string) (*Control, error) {
	return New(Spec{Kind: Button, Title: title})
}

// NewLabel makes a non-editable text label.
func NewLabel(text string) (*Control, error) {
	return New(Spec{Kind: Label, Title: text})
}

// NewTextField makes an editable text field with the given initial text.
func NewTextField(text string) (*Control, error) {
	return New(Spec{Kind: TextField, Title: text})
}

// NewSecureTextField makes a secure (bulleted) text field. This is the control
// a drawn toolkit cannot substitute for a password box: the window server fills
// it without the process seeing the keystrokes.
func NewSecureTextField(text string) (*Control, error) {
	return New(Spec{Kind: SecureTextField, Title: text})
}

// NewCheckbox makes a labelled checkbox.
func NewCheckbox(title string) (*Control, error) {
	return New(Spec{Kind: Checkbox, Title: title})
}

// NewRadioButton makes a radio button. Give a set of them the same superview
// and they behave as one group.
func NewRadioButton(title string) (*Control, error) {
	return New(Spec{Kind: RadioButton, Title: title})
}

// NewSwitch makes an on/off switch (NSSwitch, macOS 10.15+).
func NewSwitch() (*Control, error) {
	return New(Spec{Kind: Switch})
}

// NewSlider makes a slider over [min,max] positioned at value.
func NewSlider(min, max, value float64) (*Control, error) {
	return New(Spec{Kind: Slider, Min: min, Max: max, Value: value})
}

// NewPopUpButton makes a pop-up list of the given items.
func NewPopUpButton(items []string) (*Control, error) {
	return New(Spec{Kind: PopUpButton, Items: items})
}

// NewProgressIndicator makes a determinate progress bar over [min,max],
// starting at min. Its value is read and written with [Control.Double]; it has
// no action.
func NewProgressIndicator(min, max float64) (*Control, error) {
	return New(Spec{Kind: ProgressIndicator, Min: min, Max: max, Value: min})
}

// NewSpinner makes an indeterminate spinning progress indicator.
// [Control.SetBool](true) starts its animation and (false) stops it.
func NewSpinner() (*Control, error) {
	return New(Spec{Kind: Spinner})
}

// NewStepper makes a stepper over [min,max] starting at value. Its value is
// [Control.Double]; [Control.OnAction] fires on each step.
func NewStepper(min, max, value float64) (*Control, error) {
	return New(Spec{Kind: Stepper, Min: min, Max: max, Value: value})
}

// NewSearchField makes a search field with the given initial text. Its text is
// [Control.StringValue]; [Control.OnChange] fires on every keystroke and
// [Control.OnAction] on Return.
func NewSearchField(text string) (*Control, error) {
	return New(Spec{Kind: SearchField, Title: text})
}

// NewComboBox makes an editable combo box with the given drop-down items. The
// typed-or-picked text is [Control.StringValue]; [Control.OnChange] fires on a
// keystroke, [Control.OnAction] on Return or a pick.
func NewComboBox(items []string) (*Control, error) {
	return New(Spec{Kind: ComboBox, Items: items})
}

// NewSegmentedControl makes a segmented control with the given segment labels.
// The selected segment's label is [Control.StringValue]; [Control.OnAction]
// fires when the selection changes.
func NewSegmentedControl(items []string) (*Control, error) {
	return New(Spec{Kind: SegmentedControl, Items: items})
}

// NewTextView makes a multi-line, editable text view with the given initial
// text. Its text is [Control.StringValue]; [Control.OnChange] fires as it is
// edited.
func NewTextView(text string) (*Control, error) {
	return New(Spec{Kind: TextView, Title: text})
}

// NewLinkButton makes a hyperlink-styled button with the given title. Its title
// is [Control.StringValue]; [Control.OnAction] fires when it is clicked.
func NewLinkButton(text string) (*Control, error) {
	return New(Spec{Kind: LinkButton, Title: text})
}

// NewDatePicker makes a date picker. Its value is [Control.StringValue] as an
// ISO-8601 YYYY-MM-DD string; [Control.OnAction] fires when the date changes.
func NewDatePicker() (*Control, error) {
	return New(Spec{Kind: DatePicker})
}

// NewColorWell makes a colour well. Its value is [Control.StringValue] as a
// #RRGGBB hex string; [Control.OnAction] (and [Control.OnChange]) fire when the
// colour changes.
func NewColorWell() (*Control, error) {
	return New(Spec{Kind: ColorWell})
}

// Kind reports which control this is.
func (c *Control) Kind() Kind { return c.kind }

// withImpl runs fn against the native control unless the control is closed. It
// centralises the closed-check every method needs. Like every AppKit call, it
// must run on the main thread; see the note on [Control].
func (c *Control) withImpl(fn func(impl)) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrClosed
	}
	im := c.im
	c.mu.Unlock()
	fn(im)
	return nil
}

// SetFrame positions and sizes the control in the coordinate system of the view
// it is (or will be) added to. The host converts from its own layout space; a
// control's frame means nothing until it has a superview.
func (c *Control) SetFrame(x, y, w, h float64) error {
	return c.withImpl(func(im impl) { im.setFrame(x, y, w, h) })
}

// AddTo makes the control a subview of parent. Adding it to a view that is on
// screen is what makes it appear; a control is never visible on its own.
func (c *Control) AddTo(parent objc.ID) error {
	return c.withImpl(func(im impl) { im.addTo(parent) })
}

// Remove takes the control out of its superview without closing it, so it can be
// added elsewhere. To dispose of it for good, use [Control.Close].
func (c *Control) Remove() error {
	return c.withImpl(func(im impl) { im.removeFromParent() })
}

// SetHidden shows or hides the control in place, keeping its frame and its place
// in the view tree.
func (c *Control) SetHidden(hidden bool) error {
	return c.withImpl(func(im impl) { im.setHidden(hidden) })
}

// SetStringValue replaces a text control's contents (Label, TextField,
// SecureTextField), or a pop-up's selected title.
func (c *Control) SetStringValue(s string) error {
	return c.withImpl(func(im impl) { im.setStringValue(s) })
}

// StringValue reads a text control's contents, or a pop-up's selected title.
// On a closed control it returns "".
func (c *Control) StringValue() string {
	var s string
	_ = c.withImpl(func(im impl) { s = im.stringValue() })
	return s
}

// SetDouble sets a slider's position (clamped to its range).
func (c *Control) SetDouble(v float64) error {
	return c.withImpl(func(im impl) { im.setDouble(v) })
}

// Double reads a slider's position. On a closed control it returns 0.
func (c *Control) Double() float64 {
	var v float64
	_ = c.withImpl(func(im impl) { v = im.doubleValue() })
	return v
}

// SetBool sets the on/off state of a Checkbox or Switch.
func (c *Control) SetBool(on bool) error {
	return c.withImpl(func(im impl) { im.setBool(on) })
}

// Bool reads the on/off state of a Checkbox or Switch. On a closed control it
// returns false.
func (c *Control) Bool() bool {
	var b bool
	_ = c.withImpl(func(im impl) { b = im.boolValue() })
	return b
}

// OnAction registers the handler called when the control fires its primary
// action: a button clicked, a checkbox or switch toggled, a pop-up selection
// made, or editing ended in a text field. The handler runs on the main thread.
// Passing nil clears it.
func (c *Control) OnAction(fn func()) {
	c.mu.Lock()
	c.onAction = fn
	c.mu.Unlock()
}

// OnChange registers the handler called as a control's value changes
// continuously: every keystroke in a text field, every step of a dragged
// slider. The handler runs on the main thread. Passing nil clears it.
func (c *Control) OnChange(fn func()) {
	c.mu.Lock()
	c.onChange = fn
	c.mu.Unlock()
}

func (c *Control) action() func() {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.onAction
}

func (c *Control) change() func() {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.onChange
}

// Close removes the control from its superview and releases the native object.
// After Close the control is inert: mutating methods return [ErrClosed] and
// readers return their zero value. Closing twice is safe.
func (c *Control) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	im := c.im
	c.im = nil
	tag := c.tag
	c.mu.Unlock()

	regMu.Lock()
	delete(reg, tag)
	regMu.Unlock()

	im.removeFromParent()
	im.release()
}
