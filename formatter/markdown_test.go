package formatter

import (
	"strings"
	"testing"
)

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
