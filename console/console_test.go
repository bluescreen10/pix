package console_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/console"
	"github.com/bluescreen10/pix/input"
)

// fakeInput drives a Console without a window: queued characters and key events are
// drained exactly the way a real backend's buffers are.
type fakeInput struct {
	chars []rune
	keys  []input.KeyEvent
}

func (f *fakeInput) Chars() []rune {
	out := f.chars
	f.chars = nil
	return out
}

func (f *fakeInput) Keys() []input.KeyEvent {
	out := f.keys
	f.keys = nil
	return out
}

func (f *fakeInput) typeStr(s string) {
	f.chars = append(f.chars, []rune(s)...)
}

func (f *fakeInput) press(k input.Key) {
	f.keys = append(f.keys, input.KeyEvent{Key: k, Action: input.KeyPress})
}

// newTest returns an open console (skipping the toggle dance) and its input.
func newTest(t *testing.T) (*console.Console, *fakeInput) {
	t.Helper()
	in := &fakeInput{}
	c := console.New(in)
	c.Show(true)
	c.Exec("clear") // drop the banner so tests assert on their own output
	return c, in
}

// lastLine is the most recent scrollback line.
func lastLine(c *console.Console) string {
	lines := c.Lines()
	if len(lines) == 0 {
		return ""
	}
	return lines[len(lines)-1]
}

// lastEcho is the most recent "] <line>" submit echo — what a line recalled from
// history was, regardless of whatever Exec printed afterward (an unregistered name
// like "b 2" also prints an "unknown variable" line right after its echo).
func lastEcho(c *console.Console) string {
	lines := c.Lines()
	for _, line := range slices.Backward(lines) {
		if strings.HasPrefix(line, "] ") {
			return line
		}
	}
	return ""
}

// editLine returns what the console is currently showing as the prompt + typed text.
// Draw is the only public window onto the edit line's content, so tests that need to
// see it (rather than what it produces once submitted) go through a fakePainter.
func editLine(t *testing.T, c *console.Console) string {
	t.Helper()
	p := &fakePainter{scale: 1}
	c.Draw(p, 800, 600)
	if len(p.texts) == 0 {
		t.Fatal("Draw produced nothing while the console was visible")
	}
	return p.texts[0].s
}

// TestSetRoutesParsedValueToBinding is the core promise: text typed at the prompt
// reaches the bound variable converted to its own Go type, not as a string.
func TestSetRoutesParsedValueToBinding(t *testing.T) {
	c, in := newTest(t)
	var speed float32
	console.Bind(c, "xyz", &speed, "")

	in.typeStr("set xyz 23")
	in.press(input.KeyEnter)
	c.Update()

	if speed != 23 {
		t.Fatalf("speed = %v, want 23", speed)
	}
	if got := lastLine(c); got != "xyz = 23" {
		t.Fatalf("echo = %q, want %q", got, "xyz = 23")
	}
}

// TestBareNameGetsAndAssigns covers the Quake-style shorthand: a bare name prints,
// name+value assigns, without the `set` keyword.
func TestBareNameGetsAndAssigns(t *testing.T) {
	c, _ := newTest(t)
	v := 7
	console.Bind(c, "count", &v, "")

	c.Exec("count")
	if got := lastLine(c); got != "count = 7" {
		t.Fatalf("bare get = %q", got)
	}
	c.Exec("count 9")
	if v != 9 {
		t.Fatalf("bare assign left count = %d, want 9", v)
	}
}

// TestBadValueReportsAndLeavesValueAlone: a typo at the prompt must not corrupt the
// variable, and must say so rather than failing silently.
func TestBadValueReportsAndLeavesValueAlone(t *testing.T) {
	c, _ := newTest(t)
	v := float32(1.5)
	console.Bind(c, "x", &v, "")

	c.Exec("set x banana")
	if v != 1.5 {
		t.Fatalf("bad value changed x to %v", v)
	}
	if got := lastLine(c); !strings.Contains(got, "banana") {
		t.Fatalf("error line = %q, want it to mention the bad token", got)
	}
}

// TestBoolAcceptsWordForms: a console is typed by hand, so `on`/`off` must work
// alongside true/false.
func TestBoolAcceptsWordForms(t *testing.T) {
	c, _ := newTest(t)
	var on bool
	console.Bind(c, "deferred", &on, "")

	for _, word := range []string{"on", "yes", "true", "1"} {
		on = false
		c.Exec("set deferred " + word)
		if !on {
			t.Fatalf("%q did not set the flag", word)
		}
	}
	for _, word := range []string{"off", "no", "false", "0"} {
		on = true
		c.Exec("set deferred " + word)
		if on {
			t.Fatalf("%q did not clear the flag", word)
		}
	}
}

// TestReadOnlyVariableIsReported: BindFunc with a nil setter must refuse writes out
// loud, not accept them into the void.
func TestReadOnlyVariableIsReported(t *testing.T) {
	c, _ := newTest(t)
	console.BindFunc(c, "fps", func() float64 { return 60 }, nil, "")

	c.Exec("set fps 12")
	if got := lastLine(c); !strings.Contains(got, "read-only") {
		t.Fatalf("line = %q, want a read-only complaint", got)
	}
}

// TestUnknownNameIsReported: a mistyped variable must not look like it worked.
func TestUnknownNameIsReported(t *testing.T) {
	c, _ := newTest(t)
	c.Exec("set nope 1")
	if got := lastLine(c); !strings.Contains(got, "unknown") {
		t.Fatalf("line = %q, want an unknown-variable complaint", got)
	}
	c.Exec("wat")
	if got := lastLine(c); !strings.Contains(got, "unknown") {
		t.Fatalf("line = %q, want an unknown-command complaint", got)
	}
}

// TestQuotedStringKeepsSpaces: tokenizing must not shred a string value.
func TestQuotedStringKeepsSpaces(t *testing.T) {
	c, _ := newTest(t)
	var s string
	console.Bind(c, "title", &s, "")

	c.Exec(`set title "hello there"`)
	if s != "hello there" {
		t.Fatalf("title = %q, want %q", s, "hello there")
	}
}

// TestScrollbackIsCapped: an unbounded console is a memory leak in a long session.
func TestScrollbackIsCapped(t *testing.T) {
	c, _ := newTest(t)
	c.MaxLines = 8
	for i := range 100 {
		c.Printf("line %d", i)
	}
	if len(c.Lines()) != 8 {
		t.Fatalf("scrollback held %d lines, want the cap of 8", len(c.Lines()))
	}
	if got := lastLine(c); got != "line 99" {
		t.Fatalf("cap dropped the newest line: last = %q", got)
	}
}

// TestHistoryIsCappedAndRecalls: recalling past the cap must not reach an entry that
// was evicted. Recall content is only observable by submitting what was recalled and
// reading the echo, so that is how this drives and checks it.
func TestHistoryIsCappedAndRecalls(t *testing.T) {
	c, in := newTest(t)
	c.MaxHistory = 3
	for _, line := range []string{"a 1", "b 2", "c 3", "d 4"} {
		in.typeStr(line)
		in.press(input.KeyEnter)
		c.Update()
	}

	// Four ups from the fresh (post-submit) position would reach "a 1" if history
	// still held it. The cap should have evicted it, so the fourth Up must clamp at
	// the oldest surviving entry, "b 2", instead.
	for range 4 {
		in.press(input.KeyUp)
		c.Update()
	}
	in.press(input.KeyEnter)
	c.Update()
	if got := lastEcho(c); got != "] b 2" {
		t.Fatalf("recall after eviction submitted %q, want the oldest surviving entry b 2 (a 1 should have been capped out)", got)
	}
}

// TestRecallWalksNewestToOldestThenBack covers up/down recall order and returning to
// the empty line once every entry has been stepped past.
func TestRecallWalksNewestToOldestThenBack(t *testing.T) {
	c, in := newTest(t)
	for _, line := range []string{"a 1", "b 2"} {
		in.typeStr(line)
		in.press(input.KeyEnter)
		c.Update()
	}

	in.press(input.KeyUp)
	c.Update()
	in.press(input.KeyEnter)
	c.Update()
	if got := lastEcho(c); got != "] b 2" {
		t.Fatalf("first recall submitted %q, want the newest entry b 2", got)
	}

	in.press(input.KeyUp)
	c.Update()
	in.press(input.KeyUp)
	c.Update()
	in.press(input.KeyEnter)
	c.Update()
	if got := lastEcho(c); got != "] a 1" {
		t.Fatalf("recalling two steps back submitted %q, want a 1", got)
	}

	// Stepping past the newest entry (more Downs than there is history) must land on
	// an empty line: submitting it is a no-op rather than echoing something stale.
	before := len(c.Lines())
	in.press(input.KeyDown)
	c.Update()
	in.press(input.KeyDown)
	c.Update()
	in.press(input.KeyEnter)
	c.Update()
	if len(c.Lines()) != before {
		t.Fatal("submitting after walking past the newest entry printed something; want a no-op empty-line submit")
	}
}

// TestRepeatedSubmitDoesNotFloodHistory: holding Enter should not bury the history.
// Since duplicate entries are indistinguishable by content, this bookends the run of
// duplicates with a distinct marker and checks how many recalls it takes to reach it.
func TestRepeatedSubmitDoesNotFloodHistory(t *testing.T) {
	c, in := newTest(t)
	in.typeStr("start")
	in.press(input.KeyEnter)
	c.Update()
	for range 5 {
		in.typeStr("same")
		in.press(input.KeyEnter)
		c.Update()
	}

	// If the duplicate submits were not deduped, "start" would sit five slots further
	// back; two Ups from the fresh position would still land on a "same" entry.
	in.press(input.KeyUp)
	c.Update()
	in.press(input.KeyUp)
	c.Update()
	in.press(input.KeyEnter)
	c.Update()
	if got := lastEcho(c); got != "] start" {
		t.Fatalf("two ups after five duplicate submits reached %q, want the earlier distinct entry \"start\" (duplicates were not deduped)", got)
	}
}

// TestEditingMovesAndDeletes exercises the line editor at a non-tail cursor, which is
// where off-by-ones live.
func TestEditingMovesAndDeletes(t *testing.T) {
	c, in := newTest(t)
	in.typeStr("abcd")
	c.Update()

	in.press(input.KeyLeft)
	in.press(input.KeyLeft)
	c.Update() // move the cursor left twice; no characters in this update
	in.typeStr("X")
	c.Update() // insert at the now-repositioned cursor
	if got := editLine(t, c); got != "> abXcd" {
		t.Fatalf("insert at cursor = %q, want > abXcd", got)
	}

	in.press(input.KeyBackspace)
	c.Update()
	if got := editLine(t, c); got != "> abcd" {
		t.Fatalf("backspace at cursor = %q, want > abcd", got)
	}
	in.press(input.KeyDelete)
	c.Update()
	if got := editLine(t, c); got != "> abd" {
		t.Fatalf("delete at cursor = %q, want > abd", got)
	}
	in.press(input.KeyHome)
	in.press(input.KeyBackspace)
	c.Update()
	if got := editLine(t, c); got != "> abd" {
		t.Fatalf("backspace at the start deleted something: %q", got)
	}
}

// TestToggleKeyDoesNotTypeItself: the key that opens the console also produces a
// character, which must not land in the fresh edit line.
func TestToggleKeyDoesNotTypeItself(t *testing.T) {
	in := &fakeInput{}
	c := console.New(in)

	in.press(c.ToggleKey)
	in.typeStr("`") // the character GLFW reports for that same keystroke
	c.Update()

	if !c.Visible() {
		t.Fatal("toggle key did not open the console")
	}
	if got := editLine(t, c); got != "> " {
		t.Fatalf("edit line = %q, want \"> \" — the toggle char leaked in", got)
	}

	// And typing after it works normally.
	in.typeStr("hi")
	c.Update()
	if got := editLine(t, c); got != "> hi" {
		t.Fatalf("edit line = %q, want > hi", got)
	}
}

// TestHiddenConsoleIgnoresTyping: a closed console must not accumulate the WASD the
// player is pressing.
func TestHiddenConsoleIgnoresTyping(t *testing.T) {
	in := &fakeInput{}
	c := console.New(in)

	in.typeStr("wasd")
	in.press(input.KeyEnter)
	c.Update()

	c.Show(true) // Draw is a no-op while hidden, so open it to inspect what landed
	if got := editLine(t, c); got != "> " {
		t.Fatalf("hidden console captured %q", got)
	}
}

// TestCompleteFillsCommonPrefix pins tab completion to the longest unambiguous
// prefix rather than to the first match.
func TestCompleteFillsCommonPrefix(t *testing.T) {
	c, in := newTest(t)
	var a, b float32
	console.Bind(c, "shadow.distance", &a, "")
	console.Bind(c, "shadow.bias", &b, "")

	in.typeStr("sha")
	c.Update()
	in.press(input.KeyTab)
	c.Update()
	if got := editLine(t, c); got != "> shadow." {
		t.Fatalf("completion = %q, want > shadow.", got)
	}

	in.typeStr("d")
	c.Update()
	in.press(input.KeyTab)
	c.Update()
	if got := editLine(t, c); got != "> shadow.distance" {
		t.Fatalf("unambiguous completion = %q", got)
	}
}

// TestListShowsRegisteredVariables: `list` is how anyone discovers what is tunable,
// so it must include the value and honour a prefix filter.
func TestListShowsRegisteredVariables(t *testing.T) {
	c, _ := newTest(t)
	v := float32(2.5)
	console.Bind(c, "shadow.distance", &v, "fit distance")
	other := 1
	console.Bind(c, "other", &other, "")

	c.Exec("list")
	joined := strings.Join(c.Lines(), "\n")
	if !strings.Contains(joined, "shadow.distance") || !strings.Contains(joined, "2.5") {
		t.Fatalf("list did not show the variable and value:\n%s", joined)
	}
	if !strings.Contains(joined, "fit distance") {
		t.Fatalf("list dropped the description:\n%s", joined)
	}

	c.Exec("clear")
	c.Exec("list shadow.")
	joined = strings.Join(c.Lines(), "\n")
	if strings.Contains(joined, "other") {
		t.Fatalf("prefix filter leaked an unrelated variable:\n%s", joined)
	}
}

// TestBindFuncRunsTheSetter: BindFunc exists for setters with side effects, so the
// function must actually be called rather than a field written behind its back.
func TestBindFuncRunsTheSetter(t *testing.T) {
	c, _ := newTest(t)
	got := 0
	calls := 0
	console.BindFunc(c, "n",
		func() int { return got },
		func(v int) error { calls++; got = v * 2; return nil }, "")

	c.Exec("set n 21")
	if calls != 1 || got != 42 {
		t.Fatalf("calls=%d got=%d, want 1 and 42", calls, got)
	}
}

// TestParseRejectsOutOfRange: a value that does not fit the bound type is an error,
// not a silent truncation.
func TestParseRejectsOutOfRange(t *testing.T) {
	c, _ := newTest(t)
	var small int8
	console.Bind(c, "small", &small, "")

	c.Exec("set small 9000")
	if small != 0 {
		t.Fatalf("out-of-range value was truncated into small = %d", small)
	}
	if got := lastLine(c); !strings.Contains(got, "int8") {
		t.Fatalf("error line = %q, want it to name the expected type", got)
	}
}

// fakePainter records what a Draw produced, and models a pixel-art font: sizes snap to
// whole multiples of a 16px cell, advances are a fixed 8px per character at 1x.
type fakePainter struct {
	scale float32
	texts []struct {
		s    string
		x, y float32
		size float32
	}
	rects int
}

func (p *fakePainter) Rect(x, y, w, h float32, c colors.RGBA32F) {
	p.rects++
}

func (p *fakePainter) Text(s string, x, y, size float32, c colors.RGBA32F) {
	p.texts = append(p.texts, struct {
		s    string
		x, y float32
		size float32
	}{s, x, y, size})
}

func (p *fakePainter) Measure(s string, size float32) float32 {
	return float32(len([]rune(s))) * 8 * (size / 16)
}

func (p *fakePainter) Scale() float32 {
	return p.scale
}

// SnapSize mirrors pix's real rule — floor to a whole 16px cell, minimum one. A fake
// that rounded differently would quietly make these tests disagree with what actually
// gets drawn, which is the whole thing they exist to check.
func (p *fakePainter) SnapSize(px float32) float32 {
	n := float32(int(px / 16))
	if n < 1 {
		n = 1
	}
	return n * 16
}

// TestFontSizeIsLogicalPoints: the size is in points, so the same console must come out
// twice as many device pixels on a 2x display — that is what keeps it the same physical
// size on a laptop's Retina panel as on an external 1x monitor.
func TestFontSizeIsLogicalPoints(t *testing.T) {
	drawAt := func(dpi float32) float32 {
		c, _ := newTest(t)
		c.SetFontSize(16)
		p := &fakePainter{scale: dpi}
		c.Draw(p, 800*dpi, 600*dpi)
		if len(p.texts) == 0 {
			t.Fatal("nothing was drawn")
		}
		return p.texts[0].size
	}
	one, two := drawAt(1), drawAt(2)
	if one != 16 {
		t.Fatalf("at 1x a 16pt console drew at %v device px, want 16", one)
	}
	if two != 32 {
		t.Fatalf("at 2x a 16pt console drew at %v device px, want 32", two)
	}
}

// TestLineSpacingFollowsTheSnappedSize: the painter rounds the glyph height, so line
// spacing has to be derived from what it rounded to, not the raw requested size.
// Proven here without pinning the exact spacing ratio (a private constant): two
// requested sizes that both snap to the same 16px cell must produce the same line
// step, which could only happen if the step is computed from the snapped size.
func TestLineSpacingFollowsTheSnappedSize(t *testing.T) {
	stepFor := func(fontSize float32) float32 {
		c, _ := newTest(t)
		c.SetFontSize(fontSize)
		c.Print("one")
		c.Print("two")

		p := &fakePainter{scale: 1}
		c.Draw(p, 800, 600)
		if len(p.texts) < 3 { // prompt + two lines
			t.Fatalf("drew %d texts, want the prompt and two scrollback lines", len(p.texts))
		}
		if got := p.texts[0].size; got != 16 {
			t.Fatalf("glyph height = %v, want the snapped 16", got)
		}
		return p.texts[1].y - p.texts[2].y
	}

	a, b := stepFor(12), stepFor(15) // both snap to 16
	if a != b {
		t.Fatalf("line step depends on the requested size (12pt: %v, 15pt: %v) rather than the snapped one", a, b)
	}
	if a <= 16 {
		t.Fatalf("line step %v is not clear of the 16 glyph height — lines would touch", a)
	}
}

// TestDefaultConsoleDrawsOneCellOnHiDPI pins the DEFAULT's rendered size, not just the
// constant. A test that compares FontSize() to DefaultFontSize is tautological and
// cannot catch a bad default — which is exactly how the console once shipped at two
// cells on a Retina display (huge) while the HUD sat at one.
func TestDefaultConsoleDrawsOneCellOnHiDPI(t *testing.T) {
	c, _ := newTest(t)
	p := &fakePainter{scale: 2} // Retina
	c.Draw(p, 1600, 1200)

	if len(p.texts) == 0 {
		t.Fatal("nothing was drawn")
	}
	if got := p.texts[0].size; got != 16 {
		t.Fatalf("default console drew glyphs at %v device px on a 2x display, want 16 "+
			"(one whole cell — 32 is two cells and reads as oversized)", got)
	}
}

// TestSetFontSizeRejectsNonsense: a zero or negative size would make text vanish or
// divide by zero downstream.
func TestSetFontSizeRejectsNonsense(t *testing.T) {
	c, _ := newTest(t)
	if got := c.FontSize(); got != console.DefaultFontSize {
		t.Fatalf("default font size = %v, want %v", got, console.DefaultFontSize)
	}
	c.SetFontSize(24)
	c.SetFontSize(0)
	c.SetFontSize(-5)
	if got := c.FontSize(); got != 24 {
		t.Fatalf("a nonsense size changed the font size to %v", got)
	}
}

// TestCommandRunsWithArgs: a command is the "do something" half of the console, so it
// must receive its tokenized arguments and be reachable by name.
func TestCommandRunsWithArgs(t *testing.T) {
	c, _ := newTest(t)
	var got []string
	c.Command("shot", "take one", func(args []string) error {
		got = args
		return nil
	})

	c.Exec(`shot "my file.png" 2`)
	if len(got) != 2 || got[0] != "my file.png" || got[1] != "2" {
		t.Fatalf("command received %q, want [\"my file.png\" \"2\"]", got)
	}
}

// TestCommandErrorIsReported: a command returning an error must say so in the
// scrollback, prefixed with its name — silently failing is the thing a debug console
// can least afford.
func TestCommandErrorIsReported(t *testing.T) {
	c, _ := newTest(t)
	c.Command("boom", "", func([]string) error { return errTest })

	c.Exec("boom")
	if got := lastLine(c); !strings.Contains(got, "boom") || !strings.Contains(got, "kaboom") {
		t.Fatalf("line = %q, want the command name and its error", got)
	}
}

// TestCommandsAreCompletedAndListed: a registered command has to be discoverable the
// same way variables are, or nobody finds it.
func TestCommandsAreCompletedAndListed(t *testing.T) {
	c, in := newTest(t)
	c.Command("screenshot", "save a PNG", func([]string) error { return nil })

	in.typeStr("scre")
	c.Update()
	in.press(input.KeyTab)
	c.Update()
	if got := editLine(t, c); got != "> screenshot" {
		t.Fatalf("completion = %q, want > screenshot", got)
	}

	c.Exec("clear")
	c.Exec("help")
	if joined := strings.Join(c.Lines(), "\n"); !strings.Contains(joined, "screenshot") ||
		!strings.Contains(joined, "save a PNG") {
		t.Fatalf("help did not mention the registered command:\n%s", joined)
	}
}

// TestBuiltinCommandsCannotBeOverridden: shadowing `help` would strand a user with no
// way to discover anything.
func TestBuiltinCommandsCannotBeOverridden(t *testing.T) {
	c, _ := newTest(t)
	defer func() {
		if recover() == nil {
			t.Fatal("overriding a built-in was allowed")
		}
	}()
	c.Command("help", "", func([]string) error { return nil })
}

var errTest = errKaboom{}

type errKaboom struct{}

func (errKaboom) Error() string {
	return "kaboom"
}
