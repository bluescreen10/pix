package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
)

const metalMagic = "PIXMTL01"

// encodeMetal preserves the shader's local workgroup size alongside metallib
// bytes. Metal needs this metadata for dispatch; it is not a pipeline setting.
func encodeMetal(code []byte, group [3]uint32) []byte {
	out := make([]byte, 20, len(code)+20)
	copy(out, metalMagic)
	for i, n := range group {
		if n == 0 {
			n = 1
		}
		binary.LittleEndian.PutUint32(out[8+i*4:], n)
	}
	return append(out, code...)
}

func decodeMetal(data []byte) (code []byte, group [3]uint32, err error) {
	group = [3]uint32{1, 1, 1}
	if !bytes.HasPrefix(data, []byte(metalMagic)) {
		return data, group, nil
	}
	if len(data) < 20 {
		return nil, group, fmt.Errorf("truncated Metal shader header")
	}
	for i := range group {
		group[i] = binary.LittleEndian.Uint32(data[8+i*4:])
		if group[i] == 0 {
			return nil, group, fmt.Errorf("zero workgroup dimension")
		}
	}
	return data[20:], group, nil
}

func compileMetallib(dir string, spirv []byte, entry string) ([]byte, error) {
	data, err := translateMetal(spirv, entry)
	if err != nil {
		return nil, err
	}
	source, group, err := decodeMetal(data)
	if err != nil {
		return nil, err
	}
	src := filepath.Join(dir, "shader.metal")
	air := filepath.Join(dir, "shader.air")
	lib := filepath.Join(dir, "shader.metallib")
	if err := os.WriteFile(src, source, 0600); err != nil {
		return nil, err
	}
	// -fpreserve-invariance is what makes [[position, invariant]] mean anything. The
	// attribute is emitted (SPIRV-Cross translates SPIR-V's Invariant decoration into
	// it), but Metal ignores it by default and is free to fuse a multiply-add in one
	// program and not in another. Two shaders computing the same clip position then
	// disagree by a fraction of an ULP, which is exactly what a depth prepass cannot
	// tolerate: the shading pass meets depth written by a different program, and every
	// fragment that landed a hair behind fails the test and is dropped.
	//
	// It shows up only on geometry with a rotation in its model matrix — an axis-aligned
	// transform is exact, so the two programs agree by luck — and only on Metal, since
	// Vulkan drivers honour the SPIR-V decoration natively.
	steps := [][]string{
		{"-sdk", "macosx", "metal", "-std=metal3.0", "-mmacosx-version-min=13.0", "-fpreserve-invariance", "-c", src, "-o", air},
		{"-sdk", "macosx", "metallib", air, "-o", lib},
	}
	for _, args := range steps {
		if err := command("xcrun", args...); err != nil {
			return nil, err
		}
	}
	compiled, err := os.ReadFile(lib)
	if err != nil {
		return nil, err
	}
	return encodeMetal(compiled, group), nil
}

// translateMetal converts SPIR-V using spirv-cross on PATH. The input must
// follow gpu's ABI and the resulting MSL entry point is named main0.
func translateMetal(spirv []byte, entry string) ([]byte, error) {
	if entry == "" {
		entry = "main"
	}
	run := func(input []byte, args ...string) ([]byte, error) {
		cmd := exec.Command("spirv-cross", append([]string{"-"}, args...)...)
		cmd.Stdin = bytes.NewReader(input)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("spirv-cross: %w: %s", err, stderr.String())
		}
		return out, nil
	}
	reflection, err := run(spirv, "--reflect")
	if err != nil {
		return nil, err
	}
	var info struct {
		Entries []struct {
			Name, Mode string
			Size       [3]uint32 `json:"workgroup_size"`
			Spec       [3]bool   `json:"workgroup_size_is_spec_constant_id"`
		} `json:"entryPoints"`
		Push []struct{ Name, Type string } `json:"push_constants"`
	}
	if err = json.Unmarshal(reflection, &info); err != nil {
		return nil, err
	}
	stage := ""
	group := [3]uint32{1, 1, 1}
	for _, e := range info.Entries {
		if e.Name == entry {
			stage = e.Mode
			if stage == "comp" {
				group = e.Size
				for _, specialized := range e.Spec {
					if specialized {
						return nil, fmt.Errorf("specialized workgroup sizes are not supported")
					}
				}
			}
			break
		}
	}
	if stage != "vert" && stage != "frag" && stage != "comp" {
		return nil, fmt.Errorf("missing or unsupported entry %q", entry)
	}
	if len(info.Push) > 1 {
		return nil, fmt.Errorf("expected at most one root push constant")
	}
	remapped, err := remapMetalBindings(spirv)
	if err != nil {
		return nil, err
	}
	options := []string{
		"--msl",
		"--msl-version", "30000",
		"--msl-argument-buffers",
		"--msl-argument-buffer-tier", "1",
		"--msl-device-argument-buffer", "1",
		"--msl-device-argument-buffer", "2",
		"--msl-device-argument-buffer", "3",
		"--msl-decoration-binding",
		"--rename-entry-point", entry, "main0",
		stage,
	}
	if stage == "vert" {
		options = append(options, "--flip-vert-y")
	}
	source, err := run(remapped, options...)
	if err != nil {
		return nil, err
	}
	if len(info.Push) == 1 {
		name := regexp.QuoteMeta(info.Push[0].Name)
		re := regexp.MustCompile(`\b` + name + `\s+\[\[buffer\([0-9]+\)\]\]`)
		// A stage that declares the root but never reads it has none in the MSL:
		// SPIRV-Cross leaves out what the entry point does not use.
		switch len(re.FindAll(source, -1)) {
		case 0:
		case 1:
			source = re.ReplaceAllLiteral(source, []byte(info.Push[0].Name+" [[buffer(0)]]"))
		default:
			return nil, fmt.Errorf("cannot locate root argument %q in generated MSL", info.Push[0].Name)
		}
	}
	return encodeMetal(source, group), nil
}

// remapMetalBindings separates unsized descriptor arrays into distinct Metal
// argument tables. SPIRV-Cross otherwise aliases their storage.
func remapMetalBindings(data []byte) ([]byte, error) {
	if len(data) < 20 || len(data)%4 != 0 || binary.LittleEndian.Uint32(data) != 0x07230203 {
		return nil, fmt.Errorf("invalid SPIR-V")
	}
	words := make([]uint32, len(data)/4)
	for i := range words {
		words[i] = binary.LittleEndian.Uint32(data[i*4:])
	}
	bindings := map[uint32]uint32{}
	sets := map[uint32]uint32{}
	for i := 5; i < len(words); {
		n := int(words[i] >> 16)
		if n == 0 || i+n > len(words) {
			return nil, fmt.Errorf("malformed SPIR-V instruction")
		}
		if words[i]&65535 == 71 && n >= 4 {
			switch words[i+2] {
			case 33:
				bindings[words[i+1]] = words[i+3]
			case 34:
				sets[words[i+1]] = words[i+3]
			}
		}
		i += n
	}
	for id, binding := range bindings {
		set, ok := sets[id]
		if !ok || set != 0 || binding > 2 {
			return nil, fmt.Errorf("unsupported descriptor set/binding %d/%d", set, binding)
		}
	}
	for i := 5; i < len(words); {
		n := int(words[i] >> 16)
		if words[i]&65535 == 71 && n >= 4 {
			id := words[i+1]
			switch words[i+2] {
			case 33:
				words[i+3] = 0
			case 34:
				binding, ok := bindings[id]
				if !ok {
					return nil, fmt.Errorf("descriptor %d lacks binding", id)
				}
				words[i+3] = 1
				if binding == 2 {
					words[i+3] = 2
				}
				if binding == 1 {
					words[i+3] = 3
				}
			}
		}
		i += n
	}
	out := make([]byte, len(data))
	for i, word := range words {
		binary.LittleEndian.PutUint32(out[i*4:], word)
	}
	return out, nil
}
