# appkit

Embed real AppKit controls — `NSButton`, `NSTextField`, `NSSecureTextField`,
`NSSlider` and their kin — inside a host's own `NSView`, from **pure Go with
`CGO_ENABLED=0`**.

```go
import "github.com/go-macos/appkit"

pw, _ := appkit.NewSecureTextField("")
pw.OnAction(func() { unlock(pw.StringValue()) }) // fires on Return
pw.SetFrame(12, 12, 220, 24)
pw.AddTo(contentView) // an objc.ID for the view it should live in
```

## Why this exists

It is the **native counterpart** to a pixel-drawn widget toolkit. A toolkit that
paints its own buttons and fields into a framebuffer is portable and fast, but a
small set of controls the operating system must own:

- a **secure text field** the window server fills without the process ever
  seeing the keystrokes — a drawn password box cannot be that;
- controls that carry the platform's exact **focus ring, drag, and
  accessibility** behaviour, which an imitation only approximates.

For those, this package hands the host a live AppKit object it can place where
the toolkit laid out a region. The two compose: **the toolkit does the layout,
AppKit does the control.**

## The controls

| Constructor | AppKit class | Value | Action |
|---|---|---|---|
| `NewButton` | `NSButton` (push) | — | `OnAction` on click |
| `NewLabel` | `NSTextField` (static) | `StringValue` | — |
| `NewTextField` | `NSTextField` | `StringValue` | `OnAction` on Return; `OnChange` per keystroke |
| `NewSecureTextField` | `NSSecureTextField` | `StringValue` | as `TextField` |
| `NewCheckbox` | `NSButton` (switch) | `Bool` | `OnAction` on toggle |
| `NewRadioButton` | `NSButton` (radio) | `Bool` | `OnAction`; siblings in one superview group |
| `NewSwitch` | `NSSwitch` | `Bool` | `OnAction` on toggle |
| `NewSlider` | `NSSlider` | `Double` | `OnChange` while dragging |
| `NewPopUpButton` | `NSPopUpButton` | `StringValue` (selected title) | `OnAction` on select |

## Contract

- **Main thread only.** Every method must be called on the thread that runs the
  AppKit event loop — where AppKit permits control creation and mutation, and
  where a windowing host already runs its layout and event loop. The action and
  change handlers are called back on that same thread.
- **It needs a host.** A control is useful only once it is a subview of a view
  that is on screen in a running application. This package does not create the
  application, window, or event loop — those belong to the windowing library
  that owns the process (for example
  [`go-widgets/window`](https://github.com/go-widgets/window)).
- **Off macOS every constructor returns [`ErrUnsupported`]** rather than being
  absent, so a consumer cross-compiles to every platform and finds out at run
  time, with one clean error, that this platform has no AppKit to embed.

## How it binds

Every native call is an Objective-C message send over
[`go-macos/objc`](https://github.com/go-macos/objc) (itself over
[`ebitengine/purego`](https://github.com/ebitengine/purego)), so the package
links with **no cgo** and cross-compiles like any other Go code. One
process-wide Objective-C target routes each control's action back to its Go
handler by the control's tag — the same pattern
[`go-macos/statusitem`](https://github.com/go-macos/statusitem) uses for menu
rows.

## Testing

The portable control model — kind and spec validation, the action registry, the
closed-state bookkeeping — is **100% covered on Linux and on all six of Go's
64-bit architectures under qemu**, through a fake control seam that needs no
AppKit. On macOS a live suite builds the real controls and reads their values
back through the actual selectors, skipping itself when the runner has no window
server.

## License

BSD-3-Clause. See [LICENSE](LICENSE).
