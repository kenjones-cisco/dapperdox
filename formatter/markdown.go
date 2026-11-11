// Package formatter renders sanitized Markdown with stable heading links and code styles.
package formatter

import (
	"bytes"
	"fmt"
	"html"
	"log/slog"
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/shurcooL/sanitized_anchor_name"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	goldhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
	nethtml "golang.org/x/net/html"

	"github.com/kenjones-cisco/dapperdox/logger"
)

const compatibilityPriority = 100

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

var markdown = newMarkdown()

var inlineRenderer = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithRendererOptions(goldhtml.WithUnsafe()),
).Renderer()

func newMarkdown() goldmark.Markdown {
	blocks := parser.DefaultBlockParsers()
	for i := range blocks {
		if blocks[i].Value == parser.NewListParser() {
			blocks[i].Value = &compatibilityListParser{parser.NewListParser()}
		}

		if blocks[i].Value == parser.NewHTMLBlockParser() {
			blocks[i].Value = &compatibilityHTMLParser{parser.NewHTMLBlockParser()}
		}
	}

	return goldmark.New(
		goldmark.WithParser(parser.NewParser(
			parser.WithBlockParsers(blocks...),
			parser.WithInlineParsers(parser.DefaultInlineParsers()...),
			parser.WithParagraphTransformers(parser.DefaultParagraphTransformers()...),
		)),
		goldmark.WithExtensions(extension.GFM),
		// Embedded author HTML is accepted only through the sanitizer below.
		goldmark.WithRendererOptions(goldhtml.WithUnsafe(), renderer.WithNodeRenderers(
			util.Prioritized(&compatibilityRenderer{}, compatibilityPriority),
		)),
	)
}

// Markdown renders CommonMark/GFM while retaining the documentation theme contracts.
func Markdown(text []byte) []byte {
	var out bytes.Buffer
	if err := markdown.Convert(text, &out); err != nil {
		logger.Logger().Error("render Markdown", slog.Any("error", err))

		return []byte("<p>Unable to render Markdown.</p>")
	}

	return policy.SanitizeBytes(out.Bytes())
}

type compatibilityRenderer struct{}

func (*compatibilityRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindHeading, renderHeading)
	reg.Register(ast.KindFencedCodeBlock, renderCodeBlock)
}

func renderHeading(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	heading := node.(*ast.Heading)
	if !entering {
		_, err := fmt.Fprintf(w, "</h%d>\n", heading.Level)

		return ast.WalkContinue, err
	}

	var content bytes.Buffer
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		if err := inlineRenderer.Render(&content, source, child); err != nil {
			return ast.WalkStop, err
		}
	}

	tree, err := nethtml.Parse(&content)
	if err != nil {
		return ast.WalkStop, err
	}

	anchor := sanitized_anchor_name.Create(extractText(tree))
	_, err = fmt.Fprintf(w, `<h%d><a name="%s" class="anchor" href="#%s" rel="nofollow" aria-hidden="true"><span class="octicon octicon-link"></span></a>`,
		heading.Level, html.EscapeString(anchor), html.EscapeString(anchor))

	return ast.WalkContinue, err
}

func extractText(node *nethtml.Node) string {
	var out strings.Builder

	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == nethtml.TextNode {
			out.WriteString(child.Data)
		} else {
			out.WriteString(extractText(child))
		}
	}

	return out.String()
}

func renderCodeBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}

	block := node.(*ast.FencedCodeBlock)
	language := strings.TrimPrefix(string(block.Language(source)), ".")

	var code bytes.Buffer

	for i := range block.Lines().Len() {
		line := block.Lines().At(i)
		code.Write(line.Value(source))
	}

	if language == "" {
		_, err := fmt.Fprintf(w, "<pre><code>%s</code></pre>\n", html.EscapeString(code.String()))

		return ast.WalkSkipChildren, err
	}

	highlighted, err := highlightCode(code.String(), language)
	if err != nil {
		return ast.WalkStop, err
	}

	_, err = fmt.Fprintf(w, "<div class=\"highlight highlight-%s\"><pre>%s</pre></div>\n",
		html.EscapeString(language), highlighted)

	return ast.WalkSkipChildren, err
}
