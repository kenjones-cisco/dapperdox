package formatter

import (
	"fmt"
	"html"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

const diffLanguage = "diff"

func highlightCode(source, language string) (string, error) {
	var name string

	switch language {
	case "Go", "Go-unformatted":
		name = "go"
	case diffLanguage:
		name = diffLanguage
	default:
		return html.EscapeString(source), nil
	}

	lexer := lexers.Get(name)

	tokens, err := lexer.Tokenise(nil, source)
	if err != nil {
		return "", fmt.Errorf("highlight %s: %w", language, err)
	}

	var out strings.Builder

	for token := tokens(); token != chroma.EOF; token = tokens() {
		text := html.EscapeString(token.Value)
		if class := tokenClass(token.Type); class != "" {
			fmt.Fprintf(&out, `<span class="%s">%s</span>`, class, text)
		} else {
			out.WriteString(text)
		}
	}

	return out.String(), nil
}

// Keep existing theme CSS rather than introduce Chroma-specific token classes.
func tokenClass(kind chroma.TokenType) string {
	switch kind {
	case chroma.KeywordConstant:
		return "o"
	case chroma.KeywordType:
		return "n"
	case chroma.GenericDeleted:
		return "gd input-block"
	case chroma.GenericInserted:
		return "gi input-block"
	case chroma.GenericHeading:
		return "gh input-block"
	case chroma.GenericSubheading:
		return "gu input-block"
	case chroma.Punctuation:
		return "n"
	default:
		switch kind.Category() {
		case chroma.Keyword, chroma.Operator:
			return "k"
		case chroma.Name:
			return "n"
		case chroma.Comment:
			return "c"
		case chroma.Literal:
			if kind.SubCategory() == chroma.LiteralNumber {
				return "m"
			}

			return "s"
		default:
			return ""
		}
	}
}
