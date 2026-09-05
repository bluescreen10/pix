package input

// TextInput is character entry: the text the user actually typed, already mapped
// through their keyboard layout and modifiers by the platform.
//
// This is deliberately separate from KeyBoardInput/KeyEvents, which report physical
// keys. Text cannot be reconstructed from key codes without reimplementing the
// platform's layout handling — KeyA is the letter "q" on AZERTY, Shift+2 is "@" on
// US and "\"" on French, and dead keys produce a character only on the *next*
// keystroke. Anything that accepts typed text (the console) reads it from here.
type TextInput interface {
	// Chars returns the characters typed since the previous call, in order, and
	// clears the buffer. Callers must drain it every frame — an unread buffer keeps
	// growing, and characters typed before a text field opened are not meant for it.
	Chars() []rune
}
