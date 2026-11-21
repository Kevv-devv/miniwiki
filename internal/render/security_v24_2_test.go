package render

import (
	"strings"
	"testing"
)

// This guards against GO-2026-5320. Goldmark must normalize character
// references before deciding whether a Markdown link target is dangerous.
func TestMarkdownRejectsEncodedJavascriptSchemes(t *testing.T) {
	tests := []struct {
		name, markdown, expected string
	}{
		{"encoded scheme", `[click](&#106;avascript:alert(1))`, `<a href="">click</a>`},
		{"dangerous autolink", `<javascript:alert(document.domain)>`, `<a href="">javascript:alert(document.domain)</a>`},
		{"dangerous image", `![alt](javascript:alert(document.domain))`, `<img src="" alt="alt">`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := WikiMarkdown(tc.markdown, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(result), tc.expected) {
				t.Fatalf("unsafe URL not rejected as expected: %s", result)
			}
		})
	}
}
