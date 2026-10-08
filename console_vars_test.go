package pix_test

import (
	"strings"
	"testing"

	"github.com/bluescreen10/pix"
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/console"
	"github.com/bluescreen10/pix/input"
	"github.com/bluescreen10/pix/scenes"
)

// nullInput satisfies console.Input without a window: these tests drive the console
// through Exec rather than through typing.
type nullInput struct{}

func (nullInput) Chars() []rune          { return nil }
func (nullInput) Keys() []input.KeyEvent { return nil }

func consoleFor(t *testing.T) (*pix.Renderer, *console.Console) {
	t.Helper()
	r, err := pix.NewOffscreenRenderer(32, 32)
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

	r.ShowFPS(false)
	c.Exec("set stats on")
	if !r.StatsVisible() {
		t.Error("set stats on did not show the HUD")
	}

	c.Exec("set vsync off")
	if r.VSyncEnabled() {
		t.Error("set vsync off left vsync on")
	}
	c.Exec("set vsync on")
	if !r.VSyncEnabled() {
		t.Error("set vsync on left vsync off")
	}
}

// TestShadowFilterVarRoundTrips: the filter is an enum the console owns, addressed by
// name, and it has the usual failure mode — a typo must report what is valid rather than silently
// leaving the kernel alone.
func TestShadowFilterVarRoundTrips(t *testing.T) {
	r, c := consoleFor(t)

	c.Exec("set shadow.filter soft")
	if got := r.ShadowFilter(); got != pix.ShadowFilterSoft {
		t.Fatalf("shadow.filter = %v, want soft", got)
	}
	c.Exec("shadow.filter")
	if got := lastConsoleLine(c); !strings.Contains(got, "soft") {
		t.Errorf("reading it back said %q, want it to report soft", got)
	}
	c.Exec("set shadow.filter nonsense")
	if got := r.ShadowFilter(); got != pix.ShadowFilterSoft {
		t.Errorf("an unknown filter name changed the setting to %v", got)
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
	for _, want := range []string{"shadows", "shadow.filter", "depthprepass", "stats", "clear.color"} {
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

// typedInput plays back keyboard input, one frame's worth per Update: the console
// drains Chars and Keys once a frame.
type typedInput struct {
	frames []typedFrame
}

// typedFrame is what typedInput delivers in one frame.
type typedFrame struct {
	chars []rune
	keys  []input.KeyEvent
}

func (in *typedInput) Keys() []input.KeyEvent {
	if len(in.frames) == 0 {
		return nil
	}
	return in.frames[0].keys
}

// Chars delivers the frame's text and moves on to the next frame: the console reads
// Keys first, then Chars.
func (in *typedInput) Chars() []rune {
	if len(in.frames) == 0 {
		return nil
	}
	chars := in.frames[0].chars
	in.frames = in.frames[1:]
	return chars
}

// TestConsoleCommandReconfiguresBetweenFrames: a command typed into the console runs
// while the renderer draws a frame, and switching HDR off rebuilds the renderer's
// resources — uploading the console's own font among them, which needs a command
// buffer of its own. It must take effect between frames, not in the middle of one,
// where the frame's command buffer is already open.
func TestConsoleCommandReconfiguresBetweenFrames(t *testing.T) {
	r, err := pix.NewOffscreenRenderer(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	r.EnableHDR(true)
	scene := scenes.New()
	defer scene.Destroy()
	scene.NewPerspectiveCamera(45, 1, 0.1, 100)

	enter := input.KeyEvent{Key: input.KeyEnter, Action: input.KeyPress}
	r.EnableConsole(&typedInput{frames: []typedFrame{
		{keys: []input.KeyEvent{{Key: console.DefaultToggleKey, Action: input.KeyPress}}},
		{chars: []rune("set hdr off"), keys: []input.KeyEvent{enter}},
	}})

	for range 3 {
		r.Render(scene)
	}

	if r.HDREnabled() {
		t.Error("HDREnabled() = true, want the typed command to have switched it off")
	}
}
