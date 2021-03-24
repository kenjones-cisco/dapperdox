/*
Package formatter provides a GitHub Flavored Markdown renderer
with fenced code block highlighting, clickable heading anchor links.

The functionality should be equivalent to the GitHub Markdown API endpoint specified at
https://developer.github.com/v3/markdown/#render-a-markdown-document-in-raw-mode, except
the rendering is performed locally.

See examples for how to generate a complete HTML page, including CSS styles.
*/
package formatter

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"text/template"

	"github.com/microcosm-cc/bluemonday"
	"github.com/russross/blackfriday"
	"github.com/shurcooL/highlight_diff"
	"github.com/shurcooL/highlight_go"
	"github.com/shurcooL/sanitized_anchor_name"
	"github.com/sourcegraph/annotate"
	"github.com/sourcegraph/syntaxhighlight"
	"golang.org/x/net/html"
)

const htmlFlags = 0

// extensions for GitHub Flavored Markdown-like parsing.
const extensions = blackfriday.EXTENSION_NO_INTRA_EMPHASIS |
	blackfriday.EXTENSION_TABLES |
	blackfriday.EXTENSION_FENCED_CODE |
	blackfriday.EXTENSION_AUTOLINK |
	blackfriday.EXTENSION_STRIKETHROUGH |
	blackfriday.EXTENSION_SPACE_HEADERS |
	blackfriday.EXTENSION_NO_EMPTY_LINE_BEFORE_BLOCK

// policy for GitHub Flavored Markdown-like sanitization.
var policy = func() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AllowAttrs("class").Matching(bluemonday.SpaceSeparatedTokens).OnElements("div", "span")
	p.AllowAttrs("class", "name").Matching(bluemonday.SpaceSeparatedTokens).OnElements("a")
	p.AllowAttrs("rel").Matching(regexp.MustCompile(`^nofollow$`)).OnElements("a")
	p.AllowAttrs("aria-hidden").Matching(regexp.MustCompile(`^true$`)).OnElements("a")
	p.AllowAttrs("type").Matching(regexp.MustCompile(`^checkbox$`)).OnElements("input")
	p.AllowAttrs("checked", "disabled").Matching(regexp.MustCompile(`^$`)).OnElements("input")
	p.AllowDataURIImages()

	return p
}()

// Markdown renders GitHub Flavored Markdown text.
func Markdown(text []byte) []byte {
	r := &renderer{Html: blackfriday.HtmlRenderer(htmlFlags, "", "").(*blackfriday.Html)}
	unsanitized := blackfriday.Markdown(text, r, extensions)

	return policy.SanitizeBytes(unsanitized)
}

type renderer struct {
	*blackfriday.Html
}

// GitHub Flavored Markdown heading with clickable and hidden anchor.
func (*renderer) Header(out *bytes.Buffer, text func() bool, level int, _ string) {
	marker := out.Len()
	doubleSpace(out)

	if !text() {
		out.Truncate(marker)

		return
	}

	textHTML := out.String()[marker:]
	out.Truncate(marker)

	// Extract text content of the heading.
	var textContent string
	if node, err := html.Parse(strings.NewReader(textHTML)); err == nil {
		textContent = extractText(node)
	} else {
		// Failed to parse HTML (probably can never happen), so just use the whole thing.
		textContent = html.UnescapeString(textHTML)
	}

	anchorName := sanitized_anchor_name.Create(textContent)

	_, _ = out.WriteString(fmt.Sprintf(`<h%d><a name=%q class="anchor" href="#%s" rel="nofollow" aria-hidden="true"><span class="octicon octicon-link"></span></a>`, level, anchorName, anchorName))
	_, _ = out.WriteString(textHTML)
	_, _ = out.WriteString(fmt.Sprintf("</h%d>\n", level))
}

// extractText returns the recursive concatenation of the text content of an html node.
func extractText(n *html.Node) string {
	var out string

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			out += c.Data
		} else {
			out += extractText(c)
		}
	}

	return out
}

// TODO: Clean up and improve this code.
// GitHub Flavored Markdown fenced code block with highlighting.
func (*renderer) BlockCode(out *bytes.Buffer, text []byte, lang string) {
	doubleSpace(out)

	// parse out the language name
	count := 0

	for _, elt := range strings.Fields(lang) {
		if elt[0] == '.' {
			elt = elt[1:]
		}

		if elt == "" {
			continue
		}

		_, _ = out.WriteString(`<div class="highlight highlight-`)
		attrEscape(out, []byte(elt))
		lang = elt
		_, _ = out.WriteString(`"><pre>`)
		count++

		break
	}

	if count == 0 {
		_, _ = out.WriteString("<pre><code>")
	}

	if highlightedCode, ok := highlightCode(text, lang); ok {
		_, _ = out.Write(highlightedCode)
	} else {
		attrEscape(out, text)
	}

	if count == 0 {
		_, _ = out.WriteString("</code></pre>\n")
	} else {
		_, _ = out.WriteString("</pre></div>\n")
	}
}

// Task List support.
func (r *renderer) ListItem(out *bytes.Buffer, text []byte, flags int) {
	switch {
	case bytes.HasPrefix(text, []byte("[ ] ")):
		text = append([]byte(`<input type="checkbox" disabled="">`), text[3:]...)
	case bytes.HasPrefix(text, []byte("[x] ")) || bytes.HasPrefix(text, []byte("[X] ")):
		text = append([]byte(`<input type="checkbox" checked="" disabled="">`), text[3:]...)
	}

	r.Html.ListItem(out, text, flags)
}

var gfmHTMLConfig = syntaxhighlight.HTMLConfig{
	String:        "s",
	Keyword:       "k",
	Comment:       "c",
	Type:          "n",
	Literal:       "o",
	Punctuation:   "p",
	Plaintext:     "n",
	Tag:           "tag",
	HTMLTag:       "htm",
	HTMLAttrName:  "atn",
	HTMLAttrValue: "atv",
	Decimal:       "m",
}

func highlightCode(src []byte, lang string) ([]byte, bool) {
	switch lang {
	case "Go", "Go-unformatted":
		return highlightGo(src)
	case "diff":
		return highlightDiff(src)
	default:
		return nil, false
	}
}

func highlightGo(src []byte) ([]byte, bool) {
	var buf bytes.Buffer

	if err := highlight_go.Print(src, &buf, syntaxhighlight.HTMLPrinter(gfmHTMLConfig)); err != nil {
		return nil, false
	}

	return buf.Bytes(), true
}

func highlightDiff(src []byte) ([]byte, bool) {
	anns, err := highlight_diff.Annotate(src)
	if err != nil {
		return nil, false
	}

	lines := bytes.Split(src, []byte("\n"))
	lineStarts := make([]int, len(lines))

	var offset int

	for lineIndex := 0; lineIndex < len(lines); lineIndex++ {
		lineStarts[lineIndex] = offset
		offset += len(lines[lineIndex]) + 1
	}

	lastDel, lastIns := -1, -1

	for lineIndex := 0; lineIndex < len(lines); lineIndex++ {
		var lineFirstChar byte

		if len(lines[lineIndex]) > 0 {
			lineFirstChar = lines[lineIndex][0]
		}

		switch lineFirstChar {
		case '+':
			if lastIns == -1 {
				lastIns = lineIndex
			}
		case '-':
			if lastDel == -1 {
				lastDel = lineIndex
			}
		default:
			if lastDel != -1 || lastIns != -1 {
				if lastDel == -1 {
					lastDel = lastIns
				} else if lastIns == -1 {
					lastIns = lineIndex
				}

				beginOffsetLeft := lineStarts[lastDel]
				endOffsetLeft := lineStarts[lastIns]
				beginOffsetRight := lineStarts[lastIns]
				endOffsetRight := lineStarts[lineIndex]

				anns = append(anns,
					&annotate.Annotation{
						Start:     beginOffsetLeft,
						End:       endOffsetLeft,
						Left:      []byte(`<span class="gd input-block">`),
						Right:     []byte(`</span>`),
						WantInner: 0,
					},
					&annotate.Annotation{
						Start:     beginOffsetRight,
						End:       endOffsetRight,
						Left:      []byte(`<span class="gi input-block">`),
						Right:     []byte(`</span>`),
						WantInner: 0,
					},
				)

				if lineFirstChar != '@' {
					// This is needed to filter out the "-" and "+" at the beginning of each line from being highlighted.
					// TODO: Still not completely filtered out.
					leftContent := ""
					for line := lastDel; line < lastIns; line++ {
						leftContent += "\x00" + string(lines[line][1:]) + "\n"
					}

					rightContent := ""
					for line := lastIns; line < lineIndex; line++ {
						rightContent += "\x00" + string(lines[line][1:]) + "\n"
					}

					var sectionSegments [2][]*annotate.Annotation

					highlight_diff.HighlightedDiffFunc(leftContent, rightContent, &sectionSegments, [2]int{beginOffsetLeft, beginOffsetRight})

					anns = append(anns, sectionSegments[0]...)
					anns = append(anns, sectionSegments[1]...)
				}
			}

			lastDel, lastIns = -1, -1
		}
	}

	sort.Sort(anns)

	out, err := annotate.Annotate(src, anns, template.HTMLEscape)
	if err != nil {
		return nil, false
	}

	return out, true
}

// Unexported blackfriday helpers.

func doubleSpace(out *bytes.Buffer) {
	if out.Len() > 0 {
		_ = out.WriteByte('\n')
	}
}

func escapeSingleChar(char byte) (string, bool) {
	if char == '"' {
		return "&quot;", true
	}

	if char == '&' {
		return "&amp;", true
	}

	if char == '<' {
		return "&lt;", true
	}

	if char == '>' {
		return "&gt;", true
	}

	return "", false
}

func attrEscape(out *bytes.Buffer, src []byte) {
	org := 0

	for i, ch := range src {
		if entity, ok := escapeSingleChar(ch); ok {
			if i > org {
				// copy all the normal characters since the last escape
				_, _ = out.Write(src[org:i])
			}

			org = i + 1
			_, _ = out.WriteString(entity)
		}
	}

	if org < len(src) {
		_, _ = out.Write(src[org:])
	}
}
