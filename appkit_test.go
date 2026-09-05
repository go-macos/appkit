// Copyright (c) the appkit authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package appkit

import (
	"errors"
	"strings"
	"testing"

	"github.com/go-macos/objc"
)

// fakeImpl is a control with no AppKit behind it: it records what the portable
// layer asked of it and returns whatever a test set. Swapping [create] for a
// factory that returns one is what lets every line of the portable file run on
// a Linux runner — and on every architecture under qemu — with no window
// server anywhere.
type fakeImpl struct {
	items      []string
	x, y, w, h float64
	parent     objc.ID
	removed    bool
	released   bool
	hidden     bool
	str        string
	dbl        float64
	bl         bool
}

func (f *fakeImpl) setFrame(x, y, w, h float64) { f.x, f.y, f.w, f.h = x, y, w, h }
func (f *fakeImpl) addTo(p objc.ID)             { f.parent = p }
func (f *fakeImpl) removeFromParent()           { f.removed = true }
func (f *fakeImpl) setHidden(h bool)            { f.hidden = h }
func (f *fakeImpl) stringValue() string         { return f.str }
func (f *fakeImpl) setStringValue(s string)     { f.str = s }
func (f *fakeImpl) doubleValue() float64        { return f.dbl }
func (f *fakeImpl) setDouble(v float64)         { f.dbl = v }
func (f *fakeImpl) boolValue() bool             { return f.bl }
func (f *fakeImpl) setBool(on bool)             { f.bl = on }
func (f *fakeImpl) setItems(items []string)     { f.items = append([]string(nil), items...) }
func (f *fakeImpl) release()                    { f.released = true }

// fakeCreate installs a create seam that hands out a fresh *fakeImpl for every
// control. A test reads the impl of a built control with getFake. The seam is
// restored when the test ends.
func fakeCreate(t *testing.T) {
	t.Helper()
	old := create
	create = func(Spec, uint64) (impl, error) { return &fakeImpl{}, nil }
	t.Cleanup(func() { create = old })
}

// failCreate installs a create seam that always fails with err.
func failCreate(t *testing.T, err error) {
	t.Helper()
	old := create
	create = func(Spec, uint64) (impl, error) { return nil, err }
	t.Cleanup(func() { create = old })
}

// impl of the last control the fake built, read off the control itself.
func getFake(c *Control) *fakeImpl { return c.im.(*fakeImpl) }

func TestKindString(t *testing.T) {
	want := map[Kind]string{
		Button:            "Button",
		Label:             "Label",
		TextField:         "TextField",
		SecureTextField:   "SecureTextField",
		Checkbox:          "Checkbox",
		RadioButton:       "RadioButton",
		Switch:            "Switch",
		Slider:            "Slider",
		PopUpButton:       "PopUpButton",
		ProgressIndicator: "ProgressIndicator",
		Spinner:           "Spinner",
		Stepper:           "Stepper",
		SearchField:       "SearchField",
		ComboBox:          "ComboBox",
		SegmentedControl:  "SegmentedControl",
		TextView:          "TextView",
		LinkButton:        "LinkButton",
		DatePicker:        "DatePicker",
		ColorWell:         "ColorWell",
	}
	for k, s := range want {
		if got := k.String(); got != s {
			t.Errorf("Kind(%d).String() = %q, want %q", int(k), got, s)
		}
	}
	if got := Kind(99).String(); got != "Kind(99)" {
		t.Errorf("unknown kind String() = %q, want Kind(99)", got)
	}
}

func TestSpecValidate(t *testing.T) {
	cases := []struct {
		name string
		spec Spec
		ok   bool
	}{
		{"button ok", Spec{Kind: Button}, true},
		{"kind too small", Spec{Kind: -1}, false},
		{"kind too large", Spec{Kind: kindCount}, false},
		{"popup empty", Spec{Kind: PopUpButton}, false},
		{"popup ok", Spec{Kind: PopUpButton, Items: []string{"a"}}, true},
		{"slider min>=max", Spec{Kind: Slider, Min: 1, Max: 1}, false},
		{"slider ok", Spec{Kind: Slider, Min: 0, Max: 1}, true},
		{"segmented empty", Spec{Kind: SegmentedControl}, false},
		{"segmented ok", Spec{Kind: SegmentedControl, Items: []string{"a"}}, true},
		{"stepper min>=max", Spec{Kind: Stepper, Min: 2, Max: 1}, false},
		{"stepper ok", Spec{Kind: Stepper, Min: 0, Max: 10}, true},
		{"progress min>=max", Spec{Kind: ProgressIndicator, Min: 0, Max: 0}, false},
		{"progress ok", Spec{Kind: ProgressIndicator, Min: 0, Max: 1}, true},
		{"combobox empty ok", Spec{Kind: ComboBox}, true},
	}
	for _, c := range cases {
		err := c.spec.validate()
		if c.ok && err != nil {
			t.Errorf("%s: validate() = %v, want nil", c.name, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s: validate() = nil, want error", c.name)
		}
	}
}

func TestNewValidationError(t *testing.T) {
	if _, err := New(Spec{Kind: PopUpButton}); err == nil {
		t.Fatal("New with empty pop-up: want validation error, got nil")
	}
}

func TestNewCreateError(t *testing.T) {
	sentinel := errors.New("boom")
	failCreate(t, sentinel)
	if _, err := New(Spec{Kind: Button}); !errors.Is(err, sentinel) {
		t.Fatalf("New create error = %v, want %v", err, sentinel)
	}
}

func TestConstructors(t *testing.T) {
	fakeCreate(t)
	cases := []struct {
		name string
		make func() (*Control, error)
		kind Kind
	}{
		{"Button", func() (*Control, error) { return NewButton("go") }, Button},
		{"Label", func() (*Control, error) { return NewLabel("hi") }, Label},
		{"TextField", func() (*Control, error) { return NewTextField("t") }, TextField},
		{"SecureTextField", func() (*Control, error) { return NewSecureTextField("s") }, SecureTextField},
		{"Checkbox", func() (*Control, error) { return NewCheckbox("c") }, Checkbox},
		{"RadioButton", func() (*Control, error) { return NewRadioButton("r") }, RadioButton},
		{"Switch", func() (*Control, error) { return NewSwitch() }, Switch},
		{"Slider", func() (*Control, error) { return NewSlider(0, 10, 5) }, Slider},
		{"PopUpButton", func() (*Control, error) { return NewPopUpButton([]string{"x"}) }, PopUpButton},
		{"ProgressIndicator", func() (*Control, error) { return NewProgressIndicator(0, 100) }, ProgressIndicator},
		{"Spinner", func() (*Control, error) { return NewSpinner() }, Spinner},
		{"Stepper", func() (*Control, error) { return NewStepper(0, 10, 5) }, Stepper},
		{"SearchField", func() (*Control, error) { return NewSearchField("q") }, SearchField},
		{"ComboBox", func() (*Control, error) { return NewComboBox([]string{"a", "b"}) }, ComboBox},
		{"SegmentedControl", func() (*Control, error) { return NewSegmentedControl([]string{"L", "R"}) }, SegmentedControl},
		{"TextView", func() (*Control, error) { return NewTextView("body") }, TextView},
		{"LinkButton", func() (*Control, error) { return NewLinkButton("home") }, LinkButton},
		{"DatePicker", func() (*Control, error) { return NewDatePicker() }, DatePicker},
		{"ColorWell", func() (*Control, error) { return NewColorWell() }, ColorWell},
	}
	for _, c := range cases {
		ctl, err := c.make()
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if ctl.Kind() != c.kind {
			t.Errorf("%s: Kind() = %v, want %v", c.name, ctl.Kind(), c.kind)
		}
		ctl.Close()
	}
}

func TestControlMutators(t *testing.T) {
	fakeCreate(t)
	c, err := NewTextField("start")
	if err != nil {
		t.Fatal(err)
	}
	f := getFake(c)

	if err := c.SetFrame(1, 2, 3, 4); err != nil {
		t.Fatal(err)
	}
	if f.x != 1 || f.y != 2 || f.w != 3 || f.h != 4 {
		t.Errorf("frame = %v,%v,%v,%v", f.x, f.y, f.w, f.h)
	}
	if err := c.AddTo(objc.ID(42)); err != nil {
		t.Fatal(err)
	}
	if f.parent != objc.ID(42) {
		t.Errorf("parent = %v, want 42", f.parent)
	}
	if err := c.SetHidden(true); err != nil || !f.hidden {
		t.Errorf("SetHidden: err=%v hidden=%v", err, f.hidden)
	}
	if err := c.Remove(); err != nil || !f.removed {
		t.Errorf("Remove: err=%v removed=%v", err, f.removed)
	}
	if err := c.SetStringValue("hello"); err != nil || c.StringValue() != "hello" {
		t.Errorf("string value = %q (err %v)", c.StringValue(), err)
	}
	if err := c.SetDouble(2.5); err != nil || c.Double() != 2.5 {
		t.Errorf("double = %v (err %v)", c.Double(), err)
	}
	if err := c.SetBool(true); err != nil || !c.Bool() {
		t.Errorf("bool = %v (err %v)", c.Bool(), err)
	}
}

func TestClosedControl(t *testing.T) {
	fakeCreate(t)
	c, err := NewButton("x")
	if err != nil {
		t.Fatal(err)
	}
	f := getFake(c)
	c.Close()
	if !f.released || !f.removed {
		t.Errorf("Close did not release/remove: released=%v removed=%v", f.released, f.removed)
	}
	// Idempotent.
	c.Close()

	// Mutators return ErrClosed; readers return zero values.
	if err := c.SetFrame(0, 0, 0, 0); !errors.Is(err, ErrClosed) {
		t.Errorf("SetFrame after close = %v, want ErrClosed", err)
	}
	if err := c.AddTo(0); !errors.Is(err, ErrClosed) {
		t.Errorf("AddTo after close = %v, want ErrClosed", err)
	}
	if err := c.SetHidden(true); !errors.Is(err, ErrClosed) {
		t.Errorf("SetHidden after close = %v, want ErrClosed", err)
	}
	if err := c.Remove(); !errors.Is(err, ErrClosed) {
		t.Errorf("Remove after close = %v, want ErrClosed", err)
	}
	if err := c.SetStringValue("x"); !errors.Is(err, ErrClosed) {
		t.Errorf("SetStringValue after close = %v, want ErrClosed", err)
	}
	if err := c.SetDouble(1); !errors.Is(err, ErrClosed) {
		t.Errorf("SetDouble after close = %v, want ErrClosed", err)
	}
	if err := c.SetBool(true); !errors.Is(err, ErrClosed) {
		t.Errorf("SetBool after close = %v, want ErrClosed", err)
	}
	if s := c.StringValue(); s != "" {
		t.Errorf("StringValue after close = %q, want empty", s)
	}
	if v := c.Double(); v != 0 {
		t.Errorf("Double after close = %v, want 0", v)
	}
	if b := c.Bool(); b {
		t.Errorf("Bool after close = %v, want false", b)
	}
}

func TestActionDispatch(t *testing.T) {
	fakeCreate(t)
	c, err := NewButton("go")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	// No handler yet: dispatch must be a no-op, not a panic.
	dispatchAction(c.tag)

	fired := 0
	c.OnAction(func() { fired++ })
	dispatchAction(c.tag)
	if fired != 1 {
		t.Fatalf("action fired %d times, want 1", fired)
	}

	// Unknown tag: no control, no-op.
	dispatchAction(c.tag + 987654)

	// Clearing the handler.
	c.OnAction(nil)
	dispatchAction(c.tag)
	if fired != 1 {
		t.Fatalf("action fired after clear, count = %d", fired)
	}
}

func TestChangeDispatch(t *testing.T) {
	fakeCreate(t)
	c, err := NewTextField("")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	dispatchChange(c.tag) // no handler: no-op

	changed := 0
	c.OnChange(func() { changed++ })
	dispatchChange(c.tag)
	dispatchChange(c.tag)
	if changed != 2 {
		t.Fatalf("change fired %d times, want 2", changed)
	}

	dispatchChange(c.tag + 987654) // unknown tag: no-op

	c.OnChange(nil)
	dispatchChange(c.tag)
	if changed != 2 {
		t.Fatalf("change fired after clear, count = %d", changed)
	}
}

func TestUnsupportedMentionsPlatform(t *testing.T) {
	// A guard that keeps the sentinel's wording useful in a cross-compiled
	// consumer's logs.
	if !strings.Contains(ErrUnsupported.Error(), "macOS") {
		t.Errorf("ErrUnsupported = %q, expected it to mention macOS", ErrUnsupported.Error())
	}
}
