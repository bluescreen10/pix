package pix

import (
	"strings"
	"testing"

	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/console"
	"github.com/bluescreen10/pix/input"
)

// nullInput satisfies console.Input without a window: these tests drive the console
// through Exec rather than through typing.
type nullInput struct{}

func (nullInput) Chars() []rune          { return nil }
func (nullInput) Keys() []input.KeyEvent { return nil }

func consoleFor(t *testing.T) (*Renderer, *console.Console) {
	t.Helper()
	r, err := NewOffscreenRenderer(32, 32)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Destroy)
	return r, r.EnableConsole(nullInput{})
}

// TestBuiltinVarsDriveRendererState is the point of auto-registration: the variables
// the renderer registers must move the renderer, not just exist in a list.
func TestBuiltinVarsDriveRendererState(t *testing.T) {
	r, c := consoleFor(t)

	r.EnableShadows(false)
	c.Exec("set shadows on")
	if !r.ShadowsEnabled() {
		t.Error("set shadows on did not enable shadows")
	}
	c.Exec("set shadows off")
	if r.ShadowsEnabled() {
		t.Error("set shadows off did not disable shadows")
	}

	c.Exec("set shadow.distance 42.5")
	if got := r.ShadowDistance(); got != 42.5 {
		t.Errorf("shadow.distance = %v, want 42.5", got)
	}

	r.EnableDeferredRendering(false)
	c.Exec("set deferred true")
	if !r.DeferredEnabled() {
		t.Error("set deferred true did not enable deferred")
	}

	r.ShowFPS(false)
	c.Exec("set stats on")
	if !r.StatsVisible() {
		t.Error("set stats on did not show the HUD")
	}
}

// TestBuiltinColorVarRoundTrips covers the untyped Register path — a colour is four
// numbers, so it goes through parse/format rather than the generic Bind.
func TestBuiltinColorVarRoundTrips(t *testing.T) {
	r, c := consoleFor(t)

	c.Exec("set clear.color 0.25 0.5 0.75 1")
	if got := r.ClearColor(); got != (colors.RGBA32F{0.25, 0.5, 0.75, 1}) {
		t.Fatalf("clear.color = %v, want {0.25 0.5 0.75 1}", got)
	}

	// Three channels leave alpha opaque.
	c.Exec("set stats.color 1 0 0")
	if got := r.FontColor(); got != (colors.RGBA32F{1, 0, 0, 1}) {
		t.Fatalf("stats.color = %v, want {1 0 0 1}", got)
	}

	// And a bad value is refused without disturbing the current one.
	c.Exec("set clear.color nope")
	if got := r.ClearColor(); got != (colors.RGBA32F{0.25, 0.5, 0.75, 1}) {
		t.Fatalf("a bad colour changed clear.color to %v", got)
	}
}

// TestReadOnlyBuiltinsAreReported: size and aspect describe the framebuffer, which the
// window owns — they must read back but refuse assignment.
func TestReadOnlyBuiltinsAreReported(t *testing.T) {
	_, c := consoleFor(t)

	c.Exec("size")
	if got := lastConsoleLine(c); !strings.Contains(got, "32x32") {
		t.Errorf("size = %q, want it to report 32x32", got)
	}
	c.Exec("set size 64x64")
	if got := lastConsoleLine(c); !strings.Contains(got, "read-only") {
		t.Errorf("assigning size said %q, want a read-only complaint", got)
	}
}

// TestBuiltinsAreListedBeforeAnyAppBinding: a console must be useful the moment it is
// enabled, which is the whole reason the renderer registers its own switches.
func TestBuiltinsAreListedBeforeAnyAppBinding(t *testing.T) {
	_, c := consoleFor(t)
	c.Exec("list")
	listing := strings.Join(c.Lines(), "\n")
	for _, want := range []string{"shadows", "shadow.distance", "deferred", "stats", "clear.color"} {
		if !strings.Contains(listing, want) {
			t.Errorf("list did not mention %q:\n%s", want, listing)
		}
	}
}

func lastConsoleLine(c *console.Console) string {
	lines := c.Lines()
	if len(lines) == 0 {
		return ""
	}
	return lines[len(lines)-1]
}
