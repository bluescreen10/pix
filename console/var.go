package console

import (
	"fmt"
	"strconv"
)

// Value is what Bind and BindFunc can parse from a typed command line. These are the
// exact types, not their underlying kinds: a `type Speed float32` needs BindFunc with
// its own conversion, or the untyped Console.Register.
type Value interface {
	bool | string | int | int8 | int16 | int32 | int64 |
		uint | uint8 | uint16 | uint32 | uint64 | float32 | float64
}

// variable is one registered value. The console only ever moves strings; get/set are
// the closures that convert at the edge, so nothing here needs reflection.
type variable struct {
	name string
	desc string
	get  func() string
	set  func(string) error
}

// Register adds a variable the console reads and writes as raw text. desc is the
// one-line description `list` prints. Use it for types Bind cannot parse (a vector, a
// color, an enum with names); prefer Bind/BindFunc otherwise.
//
// A nil set makes the variable read-only: `set` on it reports that rather than
// silently doing nothing.
func (c *Console) Register(name, desc string, get func() string, set func(string) error) {
	if get == nil {
		panic("console: Register needs a getter")
	}
	if _, dup := c.vars[name]; !dup {
		c.order = append(c.order, name)
	}
	c.vars[name] = &variable{name: name, desc: desc, get: get, set: set}
}

// Bind exposes a variable through a pointer — the common case, when the console
// should read and write a field directly:
//
//	console.Bind(c, "shadow.distance", &cfg.ShadowDistance, "directional shadow fit, world units")
//
// The type parameter drives parsing, so `set shadow.distance 40` is checked at
// compile time to match the field it lands in. When writing needs to run code
// (an setter with side effects), use BindFunc.
func Bind[T Value](c *Console, name string, p *T, desc string) {
	c.Register(name, desc,
		func() string { return format(*p) },
		func(s string) error {
			v, err := parse[T](s)
			if err != nil {
				return err
			}
			*p = v
			return nil
		})
}

// BindFunc exposes a variable through accessors, for when reading or writing has to
// do work — clamping, invalidating a cache, calling an existing setter:
//
//	console.BindFunc(c, "deferred",
//	    func() bool { return r.DeferredEnabled() },
//	    func(v bool) error { r.EnableDeferredRendering(v); return nil },
//	    "render through the G-buffer")
//
// A nil set makes the variable read-only.
func BindFunc[T Value](c *Console, name string, get func() T, set func(T) error, desc string) {
	var setStr func(string) error
	if set != nil {
		setStr = func(s string) error {
			v, err := parse[T](s)
			if err != nil {
				return err
			}
			return set(v)
		}
	}
	c.Register(name, desc, func() string { return format(get()) }, setStr)
}

// parse converts a console token into T. The switch is on *T rather than T so each
// case can assign through the pointer, which is what lets one function serve every
// type in the Value set.
func parse[T Value](s string) (T, error) {
	var v T
	var err error
	switch p := any(&v).(type) {
	case *string:
		*p = s
	case *bool:
		// Accept the shell-ish spellings too: a console is typed by hand, and
		// `set deferred on` should not be an error.
		switch s {
		case "on", "yes", "y":
			*p = true
		case "off", "no", "n":
			*p = false
		default:
			*p, err = strconv.ParseBool(s)
		}
	case *float32:
		var f float64
		f, err = strconv.ParseFloat(s, 32)
		*p = float32(f)
	case *float64:
		*p, err = strconv.ParseFloat(s, 64)
	case *int:
		var i int64
		i, err = strconv.ParseInt(s, 0, 0)
		*p = int(i)
	case *int8:
		var i int64
		i, err = strconv.ParseInt(s, 0, 8)
		*p = int8(i)
	case *int16:
		var i int64
		i, err = strconv.ParseInt(s, 0, 16)
		*p = int16(i)
	case *int32:
		var i int64
		i, err = strconv.ParseInt(s, 0, 32)
		*p = int32(i)
	case *int64:
		*p, err = strconv.ParseInt(s, 0, 64)
	case *uint:
		var u uint64
		u, err = strconv.ParseUint(s, 0, 0)
		*p = uint(u)
	case *uint8:
		var u uint64
		u, err = strconv.ParseUint(s, 0, 8)
		*p = uint8(u)
	case *uint16:
		var u uint64
		u, err = strconv.ParseUint(s, 0, 16)
		*p = uint16(u)
	case *uint32:
		var u uint64
		u, err = strconv.ParseUint(s, 0, 32)
		*p = uint32(u)
	case *uint64:
		*p, err = strconv.ParseUint(s, 0, 64)
	}
	if err != nil {
		// strconv's own message quotes the function name ("ParseFloat"), which means
		// nothing at a console prompt. Say what was expected instead.
		return v, fmt.Errorf("%q is not a valid %T", s, v)
	}
	return v, nil
}

// format renders a value for display. %v is already shortest-round-trip for floats,
// so 0.3 prints as "0.3" rather than 0.30000001192092896.
func format[T Value](v T) string {
	return fmt.Sprintf("%v", v)
}
