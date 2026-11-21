// Package slug provides stable, Unicode-aware wiki slugs shared by storage and rendering.
package slug

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// LegacyV22 identifies generated slugs from versions before punctuation support.
func LegacyV22(title string) string { return slugify(title, false) }
func Slugify(title string) string   { return slugify(title, true) }
func slugify(title string, punctuation bool) string {
	input := norm.NFC.String(strings.TrimSpace(title))
	var b strings.Builder
	dash := false
	for _, ch := range strings.ToLower(input) {
		switch {
		case unicode.IsLetter(ch), unicode.IsNumber(ch), unicode.IsMark(ch), ch == ':':
			if dash && b.Len() > 0 && ch != ':' && !strings.HasSuffix(b.String(), ":") {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(ch)
		case punctuation && (ch == '+' || ch == '#'):
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") && !strings.HasSuffix(b.String(), ":") {
				b.WriteByte('-')
			}
			if ch == '+' {
				b.WriteString("plus")
			} else {
				b.WriteString("sharp")
			}
			dash = true
		case unicode.IsSpace(ch), ch == '-', ch == '_':
			dash = b.Len() > 0
		}
	}
	result := strings.Trim(b.String(), "-:")
	if result == "" {
		sum := sha256.Sum256([]byte(input))
		return "untitled-" + hex.EncodeToString(sum[:4])
	}
	return result
}
