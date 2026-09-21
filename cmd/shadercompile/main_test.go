package main

import (
	"bytes"
	"testing"
)

func TestOutputFlag(t *testing.T) {
	var got outputs
	for _, value := range []string{"spv:build/a.spv", "metallib:build/a.metalbin"} {
		if err := got.Set(value); err != nil {
			t.Fatal(err)
		}
	}
	if len(got) != 2 || got[0] != (output{"spv", "build/a.spv"}) || got[1] != (output{"metallib", "build/a.metalbin"}) {
		t.Fatalf("outputs = %#v", got)
	}
	for _, value := range []string{"spv", "wat:a.out"} {
		if err := got.Set(value); err == nil {
			t.Fatalf("Set(%q) succeeded", value)
		}
	}
}

func TestShaderStage(t *testing.T) {
	tests := map[string]string{
		"shader.vert.glsl": "vertex",
		"shader.frag.glsl": "fragment",
		"shader.comp.glsl": "compute",
		"shader.glsl":      "",
	}
	for path, want := range tests {
		if got := shaderStage(path); got != want {
			t.Errorf("shaderStage(%q) = %q; want %q", path, got, want)
		}
	}
}

func TestEncodeDecodeMetal(t *testing.T) {
	code := []byte("MTLBexample")
	encoded := encodeMetal(code, [3]uint32{8, 4, 2})
	got, size, err := decodeMetal(encoded)
	if err != nil || !bytes.Equal(code, got) || size != [3]uint32{8, 4, 2} {
		t.Fatalf("round trip: %v %v %v", got, size, err)
	}
	if _, _, err := decodeMetal([]byte(metalMagic)); err == nil {
		t.Fatal("truncated header accepted")
	}
	if _, err := remapMetalBindings([]byte("invalid")); err == nil {
		t.Fatal("invalid SPIR-V accepted")
	}
}
