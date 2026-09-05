package console

import (
	"sort"
	"strings"
)

// commands are the built-ins. They are a fixed set rather than a registry: everything
// application-specific is a variable, and these five exist so a console is usable
// without the application registering anything at all.
var commands = map[string]func(*Console, []string){
	"help":  cmdHelp,
	"list":  cmdList,
	"get":   cmdGet,
	"set":   cmdSet,
	"clear": cmdClear,
}

// commandNames is the completion source for the built-ins, sorted once.
var commandNames = func() []string {
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}()

func cmdHelp(c *Console, _ []string) {
	c.Print("commands:")
	c.Print("  list [prefix]      list variables and their values")
	c.Print("  get <name>         print one variable")
	c.Print("  set <name> <value> assign one variable")
	c.Print("  clear              clear the scrollback")
	c.Print("  help               this")
	c.Print("a bare `<name>` prints a variable and `<name> <value>` assigns it.")
	c.Print("keys: tab completes, up/down recall, pgup/pgdn scroll, ctrl-u clears, esc closes.")
}

func cmdList(c *Console, args []string) {
	prefix := ""
	if len(args) > 0 {
		prefix = args[0]
	}
	names := append([]string(nil), c.order...)
	sort.Strings(names)

	// Pad the name column so values line up — a list of thirty variables is much
	// easier to scan down than a ragged one.
	width := 0
	var shown []string
	for _, name := range names {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		shown = append(shown, name)
		width = max(width, len(name))
	}
	if len(shown) == 0 {
		if prefix == "" {
			c.Print("no variables registered")
		} else {
			c.Printf("no variables matching %q", prefix)
		}
		return
	}
	for _, name := range shown {
		v := c.vars[name]
		line := name + strings.Repeat(" ", width-len(name)) + "  " + v.get()
		if v.set == nil {
			line += "  (read-only)"
		}
		if v.desc != "" {
			line += "  — " + v.desc
		}
		c.Print(line)
	}
}

func cmdGet(c *Console, args []string) {
	if len(args) != 1 {
		c.Print("usage: get <name>")
		return
	}
	v, ok := c.vars[args[0]]
	if !ok {
		c.Printf("unknown variable: %s", args[0])
		return
	}
	c.Printf("%s = %s", v.name, v.get())
}

func cmdSet(c *Console, args []string) {
	if len(args) < 2 {
		c.Print("usage: set <name> <value>")
		return
	}
	v, ok := c.vars[args[0]]
	if !ok {
		c.Printf("unknown variable: %s", args[0])
		return
	}
	c.assign(v, strings.Join(args[1:], " "))
}

func cmdClear(c *Console, _ []string) {
	c.lines = c.lines[:0]
	c.scroll = 0
}
