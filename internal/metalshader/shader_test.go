package metalshader

import (
	"bytes"
	"testing"
)

func TestEncodeDecode(t *testing.T) {
	code := []byte("MTLBexample")
	encoded := Encode(code, [3]uint32{8, 4, 2})
	got, size, err := Decode(encoded)
	if err != nil || !bytes.Equal(code, got) || size != [3]uint32{8, 4, 2} {
		t.Fatalf("round trip: %v %v %v", got, size, err)
	}
	if _, _, err := Decode([]byte(magic)); err == nil {
		t.Fatal("truncated header accepted")
	}
	if _, err := remap([]byte("invalid")); err == nil {
		t.Fatal("invalid SPIR-V accepted")
	}
}
