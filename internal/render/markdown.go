package render

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	gohtml "html"
	"html/template"
	"net/url"
	"regexp"
	"strings"

	"miniwiki/internal/slug"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

var wikiLinkRe = regexp.MustCompile(`\[\[([^\]|]+)(?:\|([^\]]+))?\]\]`)
var categoryDeclRe = regexp.MustCompile(`(?im)^\s*\[\[\s*Category\s*:\s*([^\]|]+)(?:\|[^\]]+)?\]\]\s*$`)

type PageExistsFunc func(slug string) bool
type ImageResolver func(name string) (href, alt string, ok bool)

type linkReplacement struct{ Token, HTML string }

// EmbeddedImageNames returns only inline Image:/File: references (not ordinary links).
func EmbeddedImageNames(content string) []string {
	var result []string
	seen := map[string]bool{}
	for _, parts := range wikiLinkRe.FindAllStringSubmatch(content, -1) {
		if len(parts) < 2 {
			continue
		}
		target := strings.TrimSpace(parts[1])
		low := strings.ToLower(target)
		if !strings.HasPrefix(low, "image:") && !strings.HasPrefix(low, "file:") {
			continue
		}
		_, name, _ := strings.Cut(target, ":")
		name = strings.TrimSpace(name)
		if name != "" && !seen[strings.ToLower(name)] {
			seen[strings.ToLower(name)] = true
			result = append(result, name)
		}
	}
	return result
}

func WikiMarkdown(markdown string, exists PageExistsFunc) (template.HTML, error) {
	return WikiMarkdownWithImages(markdown, exists, nil)
}

func WikiMarkdownWithImages(markdown string, exists PageExistsFunc, imageResolver ImageResolver) (template.HTML, error) {
	processed := StripCategoryDeclarations(markdown)
	processed, links := renderWikiLinks(processed, exists, imageResolver)

	md := goldmark.New(goldmark.WithExtensions(extension.GFM))
	var buf bytes.Buffer
	if err := md.Convert([]byte(processed), &buf); err != nil {
		return "", err
	}
	html := buf.String()
	for _, link := range links {
		html = strings.ReplaceAll(html, link.Token, link.HTML)
	}
	return template.HTML(html), nil
}

func renderWikiLinks(input string, exists PageExistsFunc, imageResolver ImageResolver) (string, []linkReplacement) {
	var links []linkReplacement
	idx := 0
	// A fresh random prefix prevents content from pre-inserting placeholder tokens.
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		panic("secure wiki-link token generation failed: " + err.Error())
	}
	prefix := "MINIWIKI_LINK_TOKEN_" + hex.EncodeToString(nonce) + "_"
	processed := wikiLinkRe.ReplaceAllStringFunc(input, func(match string) string {
		parts := wikiLinkRe.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}
		target := strings.TrimSpace(parts[1])
		label := target
		if len(parts) > 2 && strings.TrimSpace(parts[2]) != "" {
			label = strings.TrimSpace(parts[2])
		}

		lower := strings.ToLower(target)
		var html string
		if strings.HasPrefix(lower, "image:") || strings.HasPrefix(lower, "file:") {
			prefixLen := strings.Index(target, ":") + 1
			name := strings.TrimSpace(target[prefixLen:])
			caption := strings.TrimSpace(label)
			if label == target {
				caption = ""
			}
			if imageResolver != nil {
				if href, alt, ok := imageResolver(name); ok {
					imgAlt := caption
					if imgAlt == "" {
						imgAlt = alt
					}
					html = `<a class="wiki-image-link" href="` + gohtml.EscapeString(href) + `"><img class="wiki-image" src="` + gohtml.EscapeString(href) + `" alt="` + gohtml.EscapeString(imgAlt) + `"></a>`
					if caption != "" {
						html = `<figure class="wiki-image-frame">` + html + `<figcaption>` + gohtml.EscapeString(caption) + `</figcaption></figure>`
					}
				}
			}
			if html == "" {
				href := "/upload?missing=" + url.QueryEscape(name)
				display := name
				if caption != "" {
					display = caption
				}
				html = `<a class="redlink" href="` + gohtml.EscapeString(href) + `">` + gohtml.EscapeString(display) + `</a>`
			}
		} else {
			var href, class string
			if strings.HasPrefix(lower, "category:") {
				name := strings.TrimSpace(target[len("category:"):])
				href = "/category/" + url.PathEscape(Slugify(name))
			} else if imageResolver != nil {
				if imgHref, _, ok := imageResolver(target); ok {
					href = imgHref
				} else {
					slug := Slugify(target)
					if exists != nil && exists(slug) {
						href = "/wiki/" + url.PathEscape(slug)
					} else {
						href = "/wiki/" + url.PathEscape(slug) + "/edit"
						class = ` class="redlink"`
					}
				}
			} else {
				slug := Slugify(target)
				if exists != nil && exists(slug) {
					href = "/wiki/" + url.PathEscape(slug)
				} else {
					href = "/wiki/" + url.PathEscape(slug) + "/edit"
					class = ` class="redlink"`
				}
			}
			html = `<a` + class + ` href="` + gohtml.EscapeString(href) + `">` + gohtml.EscapeString(label) + `</a>`
		}

		token := fmt.Sprintf("%s%06d", prefix, idx)
		idx++
		links = append(links, linkReplacement{Token: token, HTML: html})
		return token
	})
	return processed, links
}

func ExtractCategories(markdown string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range categoryDeclRe.FindAllStringSubmatch(markdown, -1) {
		name := strings.TrimSpace(m[1])
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if !seen[key] {
			seen[key] = true
			out = append(out, name)
		}
	}
	return out
}

func StripCategoryDeclarations(markdown string) string {
	return strings.TrimSpace(categoryDeclRe.ReplaceAllString(markdown, ""))
}

func RedirectTarget(markdown string) (string, bool) {
	text := strings.TrimSpace(StripCategoryDeclarations(markdown))
	m := wikiLinkRe.FindStringSubmatch(text)
	if len(m) < 2 || strings.TrimSpace(m[0]) != text {
		return "", false
	}
	target := strings.TrimSpace(m[1])
	lower := strings.ToLower(target)
	if target == "" || strings.HasPrefix(lower, "category:") || strings.HasPrefix(lower, "image:") || strings.HasPrefix(lower, "file:") {
		return "", false
	}
	return Slugify(target), true
}

func Slugify(title string) string { return slug.Slugify(title) }
