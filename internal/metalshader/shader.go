// Package metalshader translates the GPU RHI's SPIR-V shader ABI into native Metal
// shaders at build time. It invokes the SPIRV-Cross executable, not a GPU API.
package metalshader

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
)

const magic = "PIXMTL01"

// Encode preserves the shader's local workgroup size alongside MSL or metallib
// bytes. Metal needs this metadata for dispatch; it is not a pipeline setting.
func Encode(code []byte, group [3]uint32) []byte {
	out := make([]byte, 20, len(code)+20)
	copy(out, magic)
	for i, n := range group {
		if n == 0 {
			n = 1
		}
		binary.LittleEndian.PutUint32(out[8+i*4:], n)
	}
	return append(out, code...)
}

// Decode accepts either an encoded shader or raw MSL/metallib (one thread per
// group). The returned code aliases data.
func Decode(data []byte) (code []byte, group [3]uint32, err error) {
	group = [3]uint32{1, 1, 1}
	if !bytes.HasPrefix(data, []byte(magic)) {
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

// Translate converts SPIR-V using spirv-cross on PATH. The input must follow
// gpu's ABI: one 64-bit push constant root, set 0 binding 0 sampled textures,
// binding 1 storage textures, and binding 2 samplers. The result contains MSL
// plus reflected local-size metadata and uses the entry point main0.
// Conversion is intended for go generate; applications embed the result.
func Translate(spirv []byte, entry string) ([]byte, error) {
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
				for _, s := range e.Spec {
					if s {
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
	remapped, err := remap(spirv)
	if err != nil {
		return nil, err
	}
	options := []string{"--msl", "--msl-version", "30000", "--msl-argument-buffers", "--msl-argument-buffer-tier", "1", "--msl-device-argument-buffer", "1", "--msl-device-argument-buffer", "2", "--msl-device-argument-buffer", "3", "--msl-decoration-binding", "--rename-entry-point", entry, "main0", stage}
	if stage == "vert" {
		options = append(options, "--flip-vert-y")
	}
	source, err := run(remapped, options...)
	if err != nil {
		return nil, err
	}
	// SPIRV-Cross's CLI places push constants after argument tables. Normalize
	// only the reflected root argument, leaving table slots 1, 2, and 3 untouched.
	if len(info.Push) == 1 {
		name := regexp.QuoteMeta(info.Push[0].Name)
		re := regexp.MustCompile(`\b` + name + `\s+\[\[buffer\([0-9]+\)\]\]`)
		if len(re.FindAll(source, -1)) != 1 {
			return nil, fmt.Errorf("cannot locate root argument %q in generated MSL", info.Push[0].Name)
		}
		source = re.ReplaceAllLiteral(source, []byte(info.Push[0].Name+" [[buffer(0)]]"))
	}
	return Encode(source, group), nil
}

// remap separates unsized descriptor arrays into distinct Metal argument tables.
// SPIRV-Cross otherwise aliases their storage because each unsized array is
// represented as a one-element member. This copy is used only for translation.
func remap(data []byte) ([]byte, error) {
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
					return nil, fmt.Errorf("descriptor %s lacks binding", strconv.Itoa(int(id)))
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
	for i, w := range words {
		binary.LittleEndian.PutUint32(out[i*4:], w)
	}
	return out, nil
}
