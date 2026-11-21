package slug

import "testing"

// Regression coverage for GO-2026-5970: malformed UTF-8 must not cause
// Unicode normalization to loop forever.
func TestMalformedUTF8SlugDoesNotHang(t *testing.T) {
	inputs := []string{
		string([]byte{0xff, 0xfe, 'A'}),
		string([]byte{0xc3, 0x28}),
		string([]byte{0xed, 0xa0, 0x80}),
	}
	for _, input := range inputs {
		result := Slugify(input)
		if result == "" {
			t.Errorf("empty slug for malformed UTF-8 input %q", input)
		}
	}
}
