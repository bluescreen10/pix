package pix

import (
	"fmt"
	"strings"

	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/console"
)

// registerBuiltins exposes the renderer's own switches on a freshly created console,
// so a console is useful the moment it is enabled rather than only after the
// application has bound things by hand. Every one of these was previously reachable
// only by editing code and rebuilding.
//
// Names are dotted and grouped by subject (shadow.*, stats.*) so `list shadow.` filters
// usefully and tab completion narrows in the same way.
func (r *Renderer) registerBuiltins(c *console.Console) {
	console.BindFunc(c, "shadows", r.ShadowsEnabled,
		func(v bool) error { r.EnableShadows(v); return nil },
		"render shadow maps")

	console.BindFunc(c, "shadow.distance", r.ShadowDistance,
		func(v float32) error { r.SetShadowDistance(v); return nil },
		"directional shadow fit distance, world units (0 = auto)")

	console.BindFunc(c, "deferred", r.DeferredEnabled,
		func(v bool) error { r.EnableDeferredRendering(v); return nil },
		"shade through the G-buffer instead of forward")

	console.BindFunc(c, "stats", r.StatsVisible,
		func(v bool) error { r.ShowFPS(v); return nil },
		"show the FPS / CPU / GPU HUD")

	bindColor(c, "stats.color", r.FontColor, r.SetFontColor,
		"HUD text colour, \"r g b a\" in [0,1]")

	bindColor(c, "clear.color", r.ClearColor, r.SetClearColor,
		"background colour, \"r g b a\" in [0,1]")

	// The G-buffer view is an enum, so it goes through Register with its own names
	// rather than Bind — "set gbuffer normal" reads better than a magic number, and
	// the error lists what is valid.
	c.Register("gbuffer", "show one G-buffer target instead of the shaded frame: "+
		strings.Join(DebugViewNames(), "/")+" (needs `deferred on`)",
		func() string { return r.DebugView().String() },
		func(v string) error {
			view, ok := ParseDebugView(v)
			if !ok {
				return fmt.Errorf("unknown view %q; want one of %s", v, strings.Join(DebugViewNames(), ", "))
			}
			if view != DebugOff && !r.DeferredEnabled() {
				return fmt.Errorf("deferred rendering is off, so there is no G-buffer to show — `set deferred on` first")
			}
			r.SetDebugView(view)
			return nil
		})

	// Read-only: no setter, so the console reports them rather than pretending they
	// can be assigned. Resizing is driven by the window, not by a variable.
	c.Register("size", "framebuffer size in pixels",
		func() string { w, h := r.Size(); return fmt.Sprintf("%dx%d", w, h) }, nil)
	c.Register("aspect", "framebuffer aspect ratio",
		func() string { return fmt.Sprintf("%.4f", r.Aspect()) }, nil)
}

// registerCommands adds the console commands that act on the renderer. Unlike a
// variable, these do something once rather than holding a value.
func (r *Renderer) registerCommands(c *console.Console) {
	c.Command("screenshot", "save a PNG of this frame; defaults to a timestamped name",
		func(args []string) error {
			path := ""
			if len(args) > 0 {
				path = strings.Join(args, " ")
			}
			// The capture happens at the end of this very frame, so the result is
			// reported from the callback rather than returned.
			r.Screenshot(path, func(p string, err error) {
				if err != nil {
					c.Printf("screenshot failed: %v", err)
					return
				}
				c.Printf("wrote %s", p)
			})
			return nil
		})
}

// bindColor binds a colour through the console's untyped Register, since a colour is
// four numbers rather than one of the scalar types Bind parses. It is the same shape
// an application would use for any type of its own.
func bindColor(c *console.Console, name string, get func() colors.RGBA32F, set func(colors.RGBA32F), desc string) {
	c.Register(name, desc,
		func() string { return get().String() },
		func(s string) error {
			v, err := colors.ParseRGBA32F(s)
			if err != nil {
				return err
			}
			set(v)
			return nil
		})
}
