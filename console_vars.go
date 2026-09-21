package pix

import (
	"fmt"
	"strconv"
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

	c.Register("shadow.filter",
		"kernel directional shadow lookups use: "+strings.Join(ShadowFilterNames(), "/"),
		func() string { return r.ShadowFilter().String() },
		func(v string) error {
			filter, ok := ParseShadowFilter(v)
			if !ok {
				return fmt.Errorf("unknown filter %q; want one of %s", v, strings.Join(ShadowFilterNames(), ", "))
			}
			r.SetShadowFilter(filter)
			return nil
		})

	console.BindFunc(c, "shadow.near", r.ShadowNear,
		func(v float32) error { r.SetShadowNear(v); return nil },
		"cascaded: distance the split starts from, world units (0 = auto)")

	console.BindFunc(c, "shadow.cascades", func() uint32 { return uint32(cascadeSettings(r).Levels) },
		func(v uint32) error {
			if v == 0 || v > MaxShadowCascades {
				return fmt.Errorf("cascades must be 1..%d", MaxShadowCascades)
			}
			cs := cascadeSettings(r)
			cs.Levels = int(v)
			r.SetShadows(cs)
			return nil
		},
		"cascaded: how many slices the view is split into")

	// The boundaries as a comma list, which is what tuning a scene actually comes down
	// to: "shadow.steps 8,25,80" is the whole of it. Empty derives them.
	c.Register("shadow.steps",
		"cascaded: boundary distances, comma separated, innermost first, or \"auto\"",
		func() string {
			cs := cascadeSettings(r)
			if cs.AutoSteps || len(cs.Steps) == 0 {
				return "auto"
			}
			parts := make([]string, len(cs.Steps))
			for i, v := range cs.Steps {
				parts[i] = strconv.FormatFloat(float64(v), 'g', -1, 32)
			}
			return strings.Join(parts, ",")
		},
		func(v string) error {
			cs := cascadeSettings(r)
			v = strings.TrimSpace(v)
			if v == "" || v == "auto" {
				cs.Steps, cs.AutoSteps = nil, true
				r.SetShadows(cs)
				return nil
			}
			fields := strings.Split(v, ",")
			if len(fields) > MaxShadowCascades {
				return fmt.Errorf("at most %d steps", MaxShadowCascades)
			}
			steps := make([]float32, 0, len(fields))
			prev := float32(0)
			for _, f := range fields {
				d, err := strconv.ParseFloat(strings.TrimSpace(f), 32)
				if err != nil {
					return fmt.Errorf("step %q is not a distance", f)
				}
				if float32(d) <= prev {
					return fmt.Errorf("steps must increase; %g does not follow %g", d, prev)
				}
				prev = float32(d)
				steps = append(steps, float32(d))
			}
			cs.Steps, cs.AutoSteps = steps, false
			r.SetShadows(cs)
			return nil
		})

	// The fit is a choice between two named shapes rather than a number, so it goes
	// through Register — "set shadow.algorithm cascaded" reads better than a magic value,
	// and the error lists what is valid. Switching keeps whatever cascade settings were
	// already there, so flipping back and forth does not discard a tuned split.
	c.Register("shadow.algorithm",
		"how directional shadow cameras are fitted: uniform/cascaded",
		func() string {
			if _, ok := r.Shadows().(ShadowCascaded); ok {
				return "cascaded"
			}
			return "uniform"
		},
		func(v string) error {
			switch v {
			case "uniform":
				r.SetShadows(ShadowUniform{})
			case "cascaded":
				r.SetShadows(cascadeSettings(r))
			default:
				return fmt.Errorf("unknown algorithm %q; want one of uniform, cascaded", v)
			}
			return nil
		})

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

	// The debug view is an enum, so it goes through Register with its own names
	// rather than Bind — "set debug normal" reads better than a magic number, and
	// the error lists what is valid. Most of these (albedo..position) are G-buffer
	// targets and need `deferred on`; objectid/triangleid are a separate, always-
	// available pass (see DebugView's doc comment) and are exempt from that check.
	c.Register("debug", "show one debug view instead of the shaded frame: "+
		strings.Join(DebugViewNames(), "/")+" (albedo..position need `deferred on`; objectid/triangleid always work)",
		func() string { return r.DebugView().String() },
		func(v string) error {
			view, ok := ParseDebugView(v)
			if !ok {
				return fmt.Errorf("unknown view %q; want one of %s", v, strings.Join(DebugViewNames(), ", "))
			}
			if view != DebugOff && view < DebugObjectID && !r.DeferredEnabled() {
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

// cascadeSettings is the renderer's cascade configuration, or the defaults when it is
// not currently fitting cascades. The console edits one field at a time, so every
// setter needs the rest of the settings to carry forward.
func cascadeSettings(r *Renderer) ShadowCascaded {
	if c, ok := r.Shadows().(ShadowCascaded); ok {
		if c.Levels <= 0 {
			c.Levels = c.levels()
		}
		return c
	}
	return ShadowCascaded{Levels: DefaultShadowCascades}
}
