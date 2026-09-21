// shadercompile compiles GLSL shaders into one or more runtime formats.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type output struct {
	format string
	path   string
}

type outputs []output

func (o *outputs) String() string {
	parts := make([]string, len(*o))
	for i, out := range *o {
		parts[i] = out.format + ":" + out.path
	}
	return strings.Join(parts, ", ")
}

func (o *outputs) Set(value string) error {
	format, path, ok := strings.Cut(value, ":")
	if !ok || path == "" {
		return fmt.Errorf("output must have the form format:path")
	}
	switch format {
	case "spv", "metallib":
	default:
		return fmt.Errorf("unsupported output format %q", format)
	}
	*o = append(*o, output{format: format, path: path})
	return nil
}

type stringsFlag []string

func (s *stringsFlag) String() string { return strings.Join(*s, ", ") }

func (s *stringsFlag) Set(value string) error {
	if value == "" {
		return fmt.Errorf("value must not be empty")
	}
	*s = append(*s, value)
	return nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("shadercompile", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	input := flags.String("i", "", "input GLSL shader")
	entry := flags.String("e", "main", "shader entry point")
	stage := flags.String("stage", "", "shader stage (inferred from the input filename by default)")
	var defines stringsFlag
	flags.Var(&defines, "D", "preprocessor definition (repeatable)")
	var outputs outputs
	flags.Var(&outputs, "o", "output as format:path; format is spv or metallib (repeatable)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	if *input == "" {
		return fmt.Errorf("-i is required")
	}
	if len(outputs) == 0 {
		return fmt.Errorf("at least one -o is required")
	}
	if *stage == "" {
		*stage = shaderStage(*input)
	}
	if *stage != "vertex" && *stage != "fragment" && *stage != "compute" {
		return fmt.Errorf("cannot infer shader stage from %q; use -stage", *input)
	}
	return compile(*input, *entry, *stage, defines, outputs)
}

func shaderStage(path string) string {
	name := strings.TrimSuffix(filepath.Base(path), ".glsl")
	switch filepath.Ext(name) {
	case ".vert":
		return "vertex"
	case ".frag":
		return "fragment"
	case ".comp":
		return "compute"
	default:
		return ""
	}
}

func compile(input, entry, stage string, defines []string, outputs []output) error {
	dir, err := os.MkdirTemp("", "pix-shadercompile-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	spvPath := filepath.Join(dir, "shader.spv")
	args := []string{"-fshader-stage=" + stage, "--target-env=vulkan1.4", "-O"}
	for _, define := range defines {
		args = append(args, "-D"+define)
	}
	args = append(args, input, "-o", spvPath)
	if err := command("glslc", args...); err != nil {
		return err
	}
	spirv, err := os.ReadFile(spvPath)
	if err != nil {
		return err
	}

	var metallib []byte
	for _, out := range outputs {
		switch out.format {
		case "spv":
			if err := os.WriteFile(out.path, spirv, 0644); err != nil {
				return fmt.Errorf("write %s: %w", out.path, err)
			}
		case "metallib":
			if metallib == nil {
				metallib, err = compileMetallib(dir, spirv, entry)
				if err != nil {
					return err
				}
			}
			if err := os.WriteFile(out.path, metallib, 0644); err != nil {
				return fmt.Errorf("write %s: %w", out.path, err)
			}
		}
	}
	return nil
}

func command(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
