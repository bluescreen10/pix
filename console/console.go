// Package console is a drop-down developer console in the idTech tradition: press a
// key mid-frame, type `set shadow.distance 40`, watch it take effect. It is the
// alternative to rebuilding to change a number, and to growing a bespoke UI for every
// knob — register a value once and it is reachable by name forever.
//
// A Console is inert on its own: it holds the registry, the scrollback and the edit
// line, reads from an Input, and paints through a Painter. The renderer wires those
// up (pix.Renderer.EnableConsole); nothing here touches the GPU.
package console

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/input"
)

// Defaults for a new Console. All are adjustable after New.
const (
	DefaultMaxLines   = 512 // scrollback kept
	DefaultMaxHistory = 64  // recalled command lines kept
	DefaultToggleKey  = input.KeyGraveAccent
	DefaultFontSize   = 12 // glyph height in logical points

	// lineSpacing is the baseline step as a multiple of the glyph height. 1.4 leaves
	// the gap that makes a wall of scrollback readable rather than a solid block.
	lineSpacing = 1.4
)

// Input is what a console needs to read: typed characters plus key transitions.
// Declared here, by the consumer, so this package depends on the input interfaces
// rather than any particular backend — *glfwinput.Input satisfies it for free.
type Input interface {
	input.TextInput
	input.KeyEvents
}

// Console is the registry, scrollback and edit line. Create it with New, feed it with
// Update once per frame, and paint it with Draw.
//
// It is not safe for concurrent use: Update and Draw run on the frame loop, and
// registration is expected during setup.
type Console struct {
	in Input

	vars  map[string]*variable
	order []string // registration order, so `list` is stable

	lines    []string // scrollback, oldest first, capped at MaxLines
	MaxLines int

	history    []string // submitted lines, oldest first, capped at MaxHistory
	MaxHistory int
	histPos    int // index into history while recalling; len(history) == editing fresh

	edit   []rune // the line being typed
	cursor int    // rune index within edit
	scroll int    // scrollback lines scrolled back from the bottom

	visible bool

	// ToggleKey shows and hides the console. The character it produces is swallowed
	// on the frame it toggles, so it never lands in the edit line.
	ToggleKey input.Key

	// Appearance. Height is the fraction of the viewport the console covers and
	// Padding is the inset from its edges, both in the same units Draw is given.
	// Colours are per-console rather than package-level so two consoles can differ.
	Height  float32
	Padding float32

	Background colors.RGBA32F
	Border     colors.RGBA32F
	Foreground colors.RGBA32F
	PromptFG   colors.RGBA32F
	CursorFG   colors.RGBA32F

	// fontSize is the glyph height in logical points. Line spacing is derived from
	// the size the painter actually rounds to, at draw time, so the two cannot drift.
	fontSize float32
}

// New creates a console reading from in. Register variables on it, then hand it to
// the renderer (or drive Update/Draw yourself).
func New(in Input) *Console {
	c := &Console{
		in:         in,
		vars:       map[string]*variable{},
		MaxLines:   DefaultMaxLines,
		MaxHistory: DefaultMaxHistory,
		ToggleKey:  DefaultToggleKey,
		Height:     0.5,
		Padding:    8,
		Background: colors.RGBA32F{0.05, 0.06, 0.09, 0.88},
		Border:     colors.RGBA32F{0.35, 0.62, 0.95, 0.9},
		Foreground: colors.RGBA32F{0.85, 0.88, 0.93, 1},
		PromptFG:   colors.RGBA32F{0.98, 0.82, 0.35, 1},
		CursorFG:   colors.RGBA32F{0.98, 0.82, 0.35, 0.55},
	}
	c.SetFontSize(DefaultFontSize)
	c.Printf("pix console — `list` for variables, `help` for commands")
	return c
}

// FontSize is the console's glyph height in logical points.
func (c *Console) FontSize() float32 { return c.fontSize }

// SetFontSize sets the glyph height in LOGICAL POINTS — not device pixels, so the
// console is the same physical size on a HiDPI display as on a 1x one.
//
// The painter rounds it to what its font can draw crisply (pix's is pixel art, so
// whole multiples of its 16px cell), which means sizes are quantized: on a 2x display
// the usable steps are 8, 16 and 24 points. 16 is the default because it is exactly
// the size the font was drawn at.
func (c *Console) SetFontSize(px float32) {
	if px <= 0 {
		return
	}
	c.fontSize = px
}

// Visible reports whether the console is open. An application should skip its own
// input handling while it is: the console captures typed text but does not and cannot
// stop a camera controller from polling the same keys. Through the renderer,
// pix.Renderer.IsConsoleOpen is the same check without needing to hold the Console.
func (c *Console) Visible() bool { return c.visible }

// Show opens or closes the console. Closing does not clear the edit line.
func (c *Console) Show(on bool) {
	c.visible = on
	c.scroll = 0
}

// Print appends a line to the scrollback (oldest lines drop past MaxLines).
func (c *Console) Print(s string) {
	for line := range strings.SplitSeq(s, "\n") {
		c.lines = append(c.lines, line)
	}
	if n := len(c.lines) - c.MaxLines; n > 0 && c.MaxLines > 0 {
		c.lines = append(c.lines[:0], c.lines[n:]...)
	}
}

// Printf appends a formatted line to the scrollback.
func (c *Console) Printf(format string, args ...any) {
	c.Print(fmt.Sprintf(format, args...))
}

// Lines returns the scrollback, oldest first. The slice aliases the console's own
// storage — read it, do not keep it.
func (c *Console) Lines() []string { return c.lines }

// Update drains a frame's input: the toggle key always, and everything else only
// while the console is open. Call once per frame, before Draw.
//
// Characters are applied before command keys, which matters when a frame catches more
// than one keystroke. The two arrive on separate queues, so their relative order is
// lost, and the two possible readings are not equally likely: a human types the line
// and *then* presses Enter, so treating text as earlier is right except for the case
// of pressing Enter and typing again inside the same frame (16ms). Doing it the other
// way round would instead submit an empty line and strand what was typed — the far
// more common sequence and a much more annoying failure. Restoring true ordering would
// mean sequencing every event through one queue in the input layer, which is not worth
// it for that window.
func (c *Console) Update() {
	keys := c.in.Keys()
	chars := c.in.Chars()

	// The toggle is read first so the character it also produces ("`") can be dropped
	// rather than typed into the line the console just opened.
	toggled := false
	for _, e := range keys {
		if e.Key == c.ToggleKey && e.Action == input.KeyPress {
			c.Show(!c.visible)
			toggled = true
		}
	}
	if !c.visible {
		return
	}
	if !toggled {
		for _, ch := range chars {
			if ch >= 0x20 && ch != 0x7F { // printable only; control keys are key events
				c.insert(ch)
			}
		}
	}
	for _, e := range keys {
		if e.Action == input.KeyRelease || e.Key == c.ToggleKey {
			continue
		}
		c.key(e)
	}
}

// key applies one non-toggle key transition. Press and Repeat are treated alike, so a
// held Backspace or arrow repeats at whatever rate the platform reports.
func (c *Console) key(e input.KeyEvent) {
	ctrl := e.Mods&input.ModControl != 0
	switch e.Key {
	case input.KeyEnter, input.KeyKPEnter:
		c.submit()
	case input.KeyBackspace:
		if c.cursor > 0 {
			c.edit = append(c.edit[:c.cursor-1], c.edit[c.cursor:]...)
			c.cursor--
		}
	case input.KeyDelete:
		if c.cursor < len(c.edit) {
			c.edit = append(c.edit[:c.cursor], c.edit[c.cursor+1:]...)
		}
	case input.KeyLeft:
		if c.cursor > 0 {
			c.cursor--
		}
	case input.KeyRight:
		if c.cursor < len(c.edit) {
			c.cursor++
		}
	case input.KeyHome:
		c.cursor = 0
	case input.KeyEnd:
		c.cursor = len(c.edit)
	case input.KeyUp:
		c.recall(-1)
	case input.KeyDown:
		c.recall(+1)
	case input.KeyPageUp:
		c.scroll++
	case input.KeyPageDown:
		if c.scroll > 0 {
			c.scroll--
		}
	case input.KeyEscape:
		c.Show(false)
	case input.KeyTab:
		c.complete()
	case input.KeyU:
		if ctrl { // readline-ish: clear the line
			c.edit, c.cursor = c.edit[:0], 0
		}
	case input.KeyL:
		if ctrl {
			c.lines = c.lines[:0]
		}
	}
}

func (c *Console) insert(ch rune) {
	c.edit = append(c.edit, 0)
	copy(c.edit[c.cursor+1:], c.edit[c.cursor:])
	c.edit[c.cursor] = ch
	c.cursor++
}

// recall walks the command history: -1 is older, +1 is newer. Stepping past the
// newest entry returns to the (empty) line being composed.
func (c *Console) recall(dir int) {
	if len(c.history) == 0 {
		return
	}
	pos := c.histPos + dir
	if pos < 0 {
		pos = 0
	}
	if pos >= len(c.history) {
		c.histPos = len(c.history)
		c.edit, c.cursor = c.edit[:0], 0
		return
	}
	c.histPos = pos
	c.edit = append(c.edit[:0], []rune(c.history[pos])...)
	c.cursor = len(c.edit)
}

// complete fills in the longest unambiguous prefix of the variable/command being
// typed, and lists the candidates when there is more than one.
func (c *Console) complete() {
	line := string(c.edit)
	word := line[strings.LastIndex(line, " ")+1:]
	if word == "" {
		return
	}
	var hits []string
	for _, name := range c.order {
		if strings.HasPrefix(name, word) {
			hits = append(hits, name)
		}
	}
	for _, name := range commandNames {
		if strings.HasPrefix(name, word) {
			hits = append(hits, name)
		}
	}
	if len(hits) == 0 {
		return
	}
	common := hits[0]
	for _, h := range hits[1:] {
		for !strings.HasPrefix(h, common) {
			common = common[:len(common)-1]
		}
	}
	if len(common) > len(word) {
		c.edit = append(c.edit[:len(c.edit)-len([]rune(word))], []rune(common)...)
		c.cursor = len(c.edit)
	}
	if len(hits) > 1 {
		sort.Strings(hits)
		c.Printf("%s", strings.Join(hits, "  "))
	}
}

// submit runs the edit line and records it in the history.
func (c *Console) submit() {
	line := strings.TrimSpace(string(c.edit))
	c.edit, c.cursor = c.edit[:0], 0
	if line == "" {
		return
	}
	c.Printf("] %s", line)

	// Consecutive duplicates are not worth a history slot — holding Enter to re-run
	// something should not bury everything else.
	if n := len(c.history); n == 0 || c.history[n-1] != line {
		c.history = append(c.history, line)
		if over := len(c.history) - c.MaxHistory; over > 0 && c.MaxHistory > 0 {
			c.history = append(c.history[:0], c.history[over:]...)
		}
	}
	c.histPos = len(c.history)
	c.Exec(line)
}

// Exec runs one console line, exactly as if it had been typed, and prints whatever it
// produces to the scrollback. Useful for startup scripts and key bindings.
func (c *Console) Exec(line string) {
	args := tokenize(line)
	if len(args) == 0 {
		return
	}
	if cmd, ok := commands[args[0]]; ok {
		cmd(c, args[1:])
		return
	}
	// Quake-style bare forms: `name` prints, `name value` assigns.
	if v, ok := c.vars[args[0]]; ok {
		if len(args) == 1 {
			c.Printf("%s = %s", v.name, v.get())
		} else {
			c.assign(v, strings.Join(args[1:], " "))
		}
		return
	}
	c.Printf("unknown command or variable: %s", args[0])
}

// assign writes a variable, reporting a read-only variable or a rejected value rather
// than failing silently.
func (c *Console) assign(v *variable, raw string) {
	if v.set == nil {
		c.Printf("%s is read-only", v.name)
		return
	}
	if err := v.set(raw); err != nil {
		c.Printf("%s: %v", v.name, err)
		return
	}
	c.Printf("%s = %s", v.name, v.get())
}

// tokenize splits a line on whitespace, keeping double-quoted runs together so a
// string variable can hold spaces.
func tokenize(line string) []string {
	var out []string
	var cur strings.Builder
	inQuote, started := false, false
	flush := func() {
		if started {
			out = append(out, cur.String())
			cur.Reset()
			started = false
		}
	}
	for _, r := range line {
		switch {
		case r == '"':
			inQuote = !inQuote
			started = true // "" is a real, empty argument
		case (r == ' ' || r == '\t') && !inQuote:
			flush()
		default:
			cur.WriteRune(r)
			started = true
		}
	}
	flush()
	return out
}
