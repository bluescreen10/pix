// metalshader converts an RHI SPIR-V shader into packaged MSL or metallib.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/bluescreen10/pix/internal/metalshader"
)

func main() {
	in := flag.String("in", "", "input .spv")
	out := flag.String("out", "", "output shader package")
	entry := flag.String("entry", "main", "SPIR-V entry point")
	lib := flag.Bool("metallib", false, "compile MSL with xcrun metal/metallib")
	flag.Parse()
	if err := convert(*in, *out, *entry, *lib); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func convert(in, out, entry string, lib bool) error {
	if in == "" || out == "" {
		return fmt.Errorf("-in and -out are required")
	}
	data, err := os.ReadFile(in)
	if err != nil {
		return err
	}
	data, err = metalshader.Translate(data, entry)
	if err != nil {
		return err
	}
	if lib {
		source, group, err := metalshader.Decode(data)
		if err != nil {
			return err
		}
		dir, err := os.MkdirTemp("", "pix-metalshader-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		src := filepath.Join(dir, "shader.metal")
		air := filepath.Join(dir, "shader.air")
		mtl := filepath.Join(dir, "shader.metallib")
		if err = os.WriteFile(src, source, 0600); err != nil {
			return err
		}
		compileSteps := [][]string{
			{"-sdk", "macosx", "metal", "-std=metal3.0", "-mmacosx-version-min=13.0", "-c", src, "-o", air},
			{"-sdk", "macosx", "metallib", air, "-o", mtl},
		}
		for _, args := range compileSteps {
			if log, err := exec.Command("xcrun", args...).CombinedOutput(); err != nil {
				return fmt.Errorf("xcrun: %w: %s", err, log)
			}
		}
		compiled, err := os.ReadFile(mtl)
		if err != nil {
			return err
		}
		data = metalshader.Encode(compiled, group)
	}
	return os.WriteFile(out, data, 0644)
}
