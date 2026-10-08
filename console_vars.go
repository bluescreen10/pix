package pix

import (
	"github.com/bluescreen10/pix/colors"
	"github.com/bluescreen10/pix/console"
)

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
