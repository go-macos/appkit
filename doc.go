// Copyright (c) the appkit authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// Package appkit embeds real AppKit controls — NSButton, NSTextField,
// NSSecureTextField, NSSlider and their kin — inside a host's own NSView, from
// pure Go with CGO_ENABLED=0.
//
// It exists to be the NATIVE counterpart to a pixel-drawn widget toolkit. A
// toolkit that paints its own buttons and text fields into a framebuffer is
// portable and fast, but there is a small set of controls the operating system
// must own: a secure text field the window server fills without the process
// ever seeing the keystrokes, the system colour and font panels, a control that
// carries the platform's exact focus-ring, drag and accessibility behaviour.
// For those, a drawn imitation is not merely less faithful — it cannot be
// correct. This package hands the host a live AppKit object it can place where
// the toolkit laid out a region, so the two compose: the toolkit does the
// layout, AppKit does the control.
//
// # The shape of the API
//
// A [Control] wraps one native control. The host creates it with a constructor
// ([NewButton], [NewSecureTextField], …), positions it with [Control.SetFrame]
// in the coordinate system of the superview it is added to with [Control.AddTo],
// reads and writes its value ([Control.StringValue], [Control.SetBool], …), and
// is told when the person changes it ([Control.OnAction], [Control.OnChange]).
// Every method must be called on the process main thread — where AppKit permits
// control creation and mutation, and where a windowing host already runs its
// layout and event loop. The action and change handlers are called back on that
// same thread.
//
// # It binds through go-macos/objc
//
// Every native call is an Objective-C message send over
// github.com/go-macos/objc (itself over github.com/ebitengine/purego), so the
// package links with no cgo and cross-compiles like any other Go code. Off
// macOS every constructor reports [ErrUnsupported] rather than being absent, so
// a consumer builds and runs on every platform and finds out at run time — with
// one clean error — that this platform has no AppKit to embed.
//
// # It needs a running application and a host view
//
// A control is only useful once it is a subview of a view that is on screen and
// whose application is running an event loop — otherwise it draws nowhere and no
// action ever fires. This package does not create the application, the window or
// the event loop; those belong to whoever owns the process (a windowing library
// such as github.com/go-widgets/window). A control created before there is an
// application is still a valid object; it simply shows and reacts to nothing
// until it is placed and the loop runs.
package appkit
