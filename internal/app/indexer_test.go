package app

import "testing"

// TestComputeChecksum validates determinism and sensitivity.
func TestComputeChecksum(t *testing.T) {
	a := []byte("hello")
	b := []byte("hello")
	c := []byte("hellp")

	if computeChecksum(a) != computeChecksum(b) {
		t.Fatal("checksum should be deterministic for identical inputs")
	}
	if computeChecksum(a) == computeChecksum(c) {
		t.Fatal("checksum should differ for different inputs")
	}
}

// TestIsValidUTF8 validates UTF-8 detection for valid and invalid inputs.
func TestIsValidUTF8(t *testing.T) {
	if !isValidUTF8([]byte("ok")) {
		t.Fatal("expected valid UTF-8 to be true")
	}
	if isValidUTF8([]byte{0xff, 0xfe, 0xfd}) {
		t.Fatal("expected invalid UTF-8 to be false")
	}
}
