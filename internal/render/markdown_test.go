package render

import (
	"strings"
	"testing"
)

func TestSlugify(t *testing.T) {
	got := Slugify("Bayesian IRT!")
	if got != "bayesian-irt" {
		t.Fatalf("expected bayesian-irt, got %q", got)
	}
}

func TestExtractCategories(t *testing.T) {
	cats := ExtractCategories("Hello\n[[Category:Statistics]]\n[[Category:Bayesian]]")
	if len(cats) != 2 {
		t.Fatalf("expected 2 categories, got %d", len(cats))
	}
}

func TestWikiMarkdownImagesLinksAndSafeMarkup(t *testing.T) {
	resolver := func(name string) (string, string, bool) {
		if name == "logo.png" {
			return "/uploads/123-logo.png", "logo.png", true
		}
		return "", "", false
	}
	exists := func(slug string) bool { return slug == "statistics" }
	cases := []struct{ label, markdown, contains string }{
		{"embedded image", "[[Image:logo.png]]", `<img class="wiki-image" src="/uploads/123-logo.png"`},
		{"embedded file alias", "[[File:logo.png|Example]]", "<figcaption>Example</figcaption>"},
		{"plain file link", "[[logo.png]]", `<a href="/uploads/123-logo.png">logo.png</a>`},
		{"missing page", "[[missing article]]", `class="redlink"`},
		{"category declaration", "[[Category:Statistics]]\nWiki page", "Wiki page"},
		{"escaped HTML in label", "[[Statistics|<script>alert(1)</script>]]", "&lt;script&gt;"},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			html, err := WikiMarkdownWithImages(tc.markdown, exists, resolver)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(html), tc.contains) {
				t.Fatalf("expected %q in %s", tc.contains, html)
			}
			if strings.Contains(string(html), `<script>`) {
				t.Fatalf("script tag was rendered: %s", html)
			}
		})
	}
	if to, ok := RedirectTarget("[[Statistics]]"); !ok || to != "statistics" {
		t.Fatalf("redirect didn't resolve to Statistics: %q, %t", to, ok)
	}
	if _, ok := RedirectTarget("[[Image:logo.png]]"); ok {
		t.Fatal("image embed was mistaken for page redirect")
	}
}

func TestV23EmbeddedFilesExtraction(t *testing.T) {
	got := EmbeddedImageNames("[[Image:Logo.png]] [[File:chart.png|Chart]] [[Statistics]] [[Image:Logo.png]]")
	if len(got) != 2 || got[0] != "Logo.png" || got[1] != "chart.png" {
		t.Fatalf("unexpected embedded list: %#v", got)
	}
}
