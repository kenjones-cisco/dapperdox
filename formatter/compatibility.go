package formatter

import (
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

type compatibilityListParser struct{ parser.BlockParser }

func (p *compatibilityListParser) Open(parent ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	node, state := p.BlockParser.Open(parent, reader, pc)
	// Existing guides use literal "1)" steps, including steps split by images.
	// Parsing those as lists would restart their numbering after sanitization.
	if list, ok := node.(*ast.List); ok && list.Marker == ')' {
		return nil, parser.NoChildren
	}

	return node, state
}

type compatibilityHTMLParser struct{ parser.BlockParser }

func (p *compatibilityHTMLParser) Continue(node ast.Node, reader text.Reader, pc parser.Context) parser.State {
	block := node.(*ast.HTMLBlock)
	if block.HTMLBlockType == ast.HTMLBlockType6 && block.Lines().Len() > 0 {
		first := block.Lines().At(0)
		last := block.Lines().At(block.Lines().Len() - 1)
		open := strings.ToLower(strings.TrimSpace(string(first.Value(reader.Source()))))
		closeTag := strings.ToLower(strings.TrimSpace(string(last.Value(reader.Source()))))
		// Guides place Markdown immediately after a complete HTML paragraph spacer.
		if (strings.HasPrefix(open, "<p>") || strings.HasPrefix(open, "<p ")) && strings.HasSuffix(closeTag, "</p>") {
			return parser.Close
		}
	}

	return p.BlockParser.Continue(node, reader, pc)
}
