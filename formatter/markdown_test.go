package formatter

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestMarkdownSeparatesBlankLineBlockquotes(t *testing.T) {
	output := string(Markdown([]byte("> **Warning**\n> First message.\n\n> **Note**\n> Second message.\n")))
	if strings.Count(output, "<blockquote>") != 2 || !strings.Contains(output, "<strong>Warning</strong>") || !strings.Contains(output, "<strong>Note</strong>") {
		t.Fatalf("separate author quotations were merged: %s", output)
	}
}

func TestConcurrentMarkdownRendering(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, source, anchor, code string
	}{
		{"Go", "# First\n\n```Go\nvar value = 1\n```\n", `name="first"`, `<span class="k">var</span>`},
		{"diff", "# Second\n\n```diff\n-old\n+new\n```\n", `name="second"`, `class="gi input-block"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			for range 50 {
				output := string(Markdown([]byte(tt.source)))
				if !strings.Contains(output, tt.anchor) || !strings.Contains(output, tt.code) {
					t.Fatalf("concurrent render lost request-local content: %s", output)
				}
			}
		})
	}
}

func TestMarkdownPreservesHeadingsAndCode(t *testing.T) {
	output := string(Markdown([]byte("# Pets\n\n```go\nvar count = 1\n```\n\n<script>alert(1)</script>\n")))
	for _, want := range []string{`name="pets"`, `href="#pets"`, `highlight-go`, "count"} {
		if !strings.Contains(output, want) {
			t.Errorf("rendered markdown missing %q: %s", want, output)
		}
	}

	if strings.Contains(output, "<script") {
		t.Fatal("markdown sanitizer allowed a script element")
	}
}

func TestMarkdownPreservesDiffHighlighting(t *testing.T) {
	for _, source := range []string{
		"@@ -1 +1 @@\n-original\n+replacement\n",
		"@@ -1 +1 @@\n-original\n unchanged\n",
		"@@ -1 +1 @@\n+replacement\n unchanged\n",
	} {
		output := string(Markdown([]byte("```diff\n" + source + "```\n")))
		if !strings.Contains(output, `highlight-diff`) || !strings.Contains(output, `input-block`) {
			t.Fatalf("diff highlighting changed: %s", output)
		}
	}
}

func TestMarkdownAuthoringContracts(t *testing.T) {
	tests := []struct {
		name, source    string
		present, absent []string
	}{
		{
			name:    "formatted and Unicode headings",
			source:  "# Café & `Pets`\n\n# Repeat\n\n# Repeat\n",
			present: []string{`name="café-pets"`, `href="#caf%C3%A9-pets"`, `class="anchor"`, `class="octicon octicon-link"`, `aria-hidden="true"`, `<code>Pets</code>`},
		},
		{
			name:    "tables and inline formatting",
			source:  "| Name | Value |\n| --- | --- |\n| **pet** | ~~dog~~ |\n\none_two_three\n",
			present: []string{"<table>", "<thead>", "<tbody>", "<strong>pet</strong>", "<del>dog</del>", "one_two_three"},
			absent:  []string{"<em>two</em>"},
		},
		{
			name:    "tasks and links",
			source:  "- [x] Done\n- [ ] Pending\n\n[guide](/guides/overview#stable-heading)\n",
			present: []string{`type="checkbox"`, `checked=""`, `disabled=""`, `href="/guides/overview#stable-heading"`},
		},
		{
			name:    "embedded HTML stays sanitized",
			source:  "<div>Trusted <b>HTML</b><script>alert(1)</script><img src=\"image.png\" onerror=\"alert(2)\"><a href=\"javascript:alert(3)\">bad</a></div>\n",
			present: []string{"<div>", "<b>HTML</b>", `src="image.png"`, "bad"},
			absent:  []string{"<script", "alert(", "onerror", "javascript:"},
		},
		{
			name:    "author download links and images",
			source:  "[file](files.json) <a href=\"files.json\" download>Download</a>\n\n![diagram](/images/diagram.png)\n",
			present: []string{`href="files.json"`, ">Download</a>", `src="/images/diagram.png"`},
		},
		{
			name:    "Go token styles",
			source:  "```Go\nvar count = 1 // comment\n```\n",
			present: []string{`class="highlight highlight-Go"`, `<pre>`, `<span class="k">var</span>`, `<span class="n">count</span>`, `<span class="m">1</span>`, `<span class="c">// comment</span>`},
		},
		{
			name:    "unformatted Go and escaped unknown code",
			source:  "```Go-unformatted\nvar count=1\n```\n\n```shell\n<script>command & args</script>\n```\n",
			present: []string{`highlight-Go-unformatted`, `highlight-shell`, "&lt;script&gt;command &amp; args&lt;/script&gt;"},
			absent:  []string{"<script>"},
		},
		{
			name:    "plain fenced and indented code",
			source:  "```\n<p>& text</p>\n```\n\n    code & text\n",
			present: []string{"<pre><code>", "&lt;p&gt;&amp; text&lt;/p&gt;", "code &amp; text"},
			absent:  []string{`class="highlight`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := string(Markdown([]byte(tt.source)))
			for _, text := range tt.present {
				if !strings.Contains(output, text) {
					t.Errorf("missing %q in %s", text, output)
				}
			}

			for _, text := range tt.absent {
				if strings.Contains(output, text) {
					t.Errorf("unexpected %q in %s", text, output)
				}
			}
		})
	}
}

func TestMarkdownPreservesDuplicateAnchorTargets(t *testing.T) {
	output := string(Markdown([]byte("# Repeat\n\n# Repeat\n")))
	if strings.Count(output, `name="repeat"`) != 2 || strings.Contains(output, `name="repeat-1"`) {
		t.Fatalf("legacy heading targets changed: %s", output)
	}
}

func TestMarkdownAfterHTMLParagraph(t *testing.T) {
	output := string(Markdown([]byte("<p>&nbsp;</p>\nContinue to [next step](/guides/next)\n")))
	if !strings.Contains(output, `href="/guides/next"`) || strings.Contains(output, "[next step]") {
		t.Fatalf("HTML spacer swallowed the following Markdown link: %s", output)
	}
}

func TestMarkdownTaskCheckboxes(t *testing.T) {
	tokens := html.NewTokenizer(strings.NewReader(string(Markdown([]byte("- [x] Done\n- [ ] Pending\n")))))

	var checkboxes []map[string]string

	for token := tokens.Next(); token != html.ErrorToken; token = tokens.Next() {
		if token != html.StartTagToken {
			continue
		}

		tag := tokens.Token()
		if tag.Data != "input" {
			continue
		}

		attrs := make(map[string]string)
		for _, attribute := range tag.Attr {
			attrs[attribute.Key] = attribute.Val
		}

		checkboxes = append(checkboxes, attrs)
	}

	if len(checkboxes) != 2 {
		t.Fatalf("expected two task checkboxes, got %v", checkboxes)
	}

	for i, checkbox := range checkboxes {
		_, disabled := checkbox["disabled"]

		_, checked := checkbox["checked"]
		if checkbox["type"] != "checkbox" || !disabled || checked != (i == 0) {
			t.Errorf("task checkbox %d changed semantics: %v", i, checkbox)
		}
	}
}

func TestMarkdownPreservesParenthesizedGuideSteps(t *testing.T) {
	source := "Instructions:  \n  1) Download a file  \n  2) Import the file\n\n3) Select a source\n\n4) Continue\n"

	output := string(Markdown([]byte(source)))
	for _, step := range []string{"1) Download", "2) Import", "3) Select", "4) Continue"} {
		if !strings.Contains(output, step) {
			t.Errorf("guide step numbering changed: %q in %s", step, output)
		}
	}

	if strings.Contains(output, "<ol>") {
		t.Fatalf("parenthesized guide steps became renumbered HTML lists: %s", output)
	}
}
