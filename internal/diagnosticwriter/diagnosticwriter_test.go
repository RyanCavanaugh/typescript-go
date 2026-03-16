package diagnosticwriter

import (
	"strings"
	"testing"

	"github.com/microsoft/typescript-go/internal/core"
	"gotest.tools/v3/assert"
)

type mockFile struct {
	content string
	name    string
	lineMap []core.TextPos
}

func (m *mockFile) FileName() string { return m.name }
func (m *mockFile) Text() string     { return m.content }
func (m *mockFile) ECMALineMap() []core.TextPos {
	if m.lineMap == nil {
		m.lineMap = core.ComputeECMALineStarts(m.content)
	}
	return m.lineMap
}

func newMockFile(content string) *mockFile {
	return &mockFile{content: content, name: "test.ts"}
}

func TestWriteCodeSnippetExpandedContext(t *testing.T) {
	t.Parallel()

	content := "line0\nline1\nline2\nline3\nline4\nline5\nline6\n"
	// Error is on "line3" (starts at byte offset 18, length 5)
	file := newMockFile(content)
	errorStart := strings.Index(content, "line3")
	errorLen := len("line3")

	tests := []struct {
		name                 string
		expandedErrorContext int
		wantContextLines     []string
		wantNotContain       []string
	}{
		{
			name:                 "no context (default)",
			expandedErrorContext: 0,
			wantContextLines:     []string{"line3"},
			wantNotContain:       []string{"line0", "line1", "line2", "line4", "line5", "line6"},
		},
		{
			name:                 "1 line of context",
			expandedErrorContext: 1,
			wantContextLines:     []string{"line2", "line3", "line4"},
			wantNotContain:       []string{"line0", "line1", "line5", "line6"},
		},
		{
			name:                 "2 lines of context",
			expandedErrorContext: 2,
			wantContextLines:     []string{"line1", "line2", "line3", "line4", "line5"},
			wantNotContain:       []string{"line0", "line6"},
		},
		{
			name:                 "large context bounded by file",
			expandedErrorContext: 100,
			wantContextLines:     []string{"line0", "line1", "line2", "line3", "line4", "line5", "line6"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf strings.Builder
			formatOpts := &FormattingOptions{
				NewLine:              "\n",
				ExpandedErrorContext: tt.expandedErrorContext,
			}
			writeCodeSnippet(&buf, file, errorStart, errorLen, foregroundColorEscapeRed, "", formatOpts)
			output := buf.String()

			for _, want := range tt.wantContextLines {
				assert.Assert(t, strings.Contains(output, want), "expected output to contain %q, got:\n%s", want, output)
			}
			for _, notWant := range tt.wantNotContain {
				assert.Assert(t, !strings.Contains(output, notWant), "expected output to NOT contain %q, got:\n%s", notWant, output)
			}
		})
	}
}

func TestWriteCodeSnippetContextAtFileStart(t *testing.T) {
	t.Parallel()

	content := "errorLine\nline1\nline2\n"
	file := newMockFile(content)

	var buf strings.Builder
	formatOpts := &FormattingOptions{
		NewLine:              "\n",
		ExpandedErrorContext: 3,
	}
	writeCodeSnippet(&buf, file, 0, len("errorLine"), foregroundColorEscapeRed, "", formatOpts)
	output := buf.String()

	// Error is on the first line - no lines above to show
	assert.Assert(t, strings.Contains(output, "errorLine"))
	assert.Assert(t, strings.Contains(output, "line1")) // context below
	assert.Assert(t, strings.Contains(output, "line2")) // context below
}

func TestWriteCodeSnippetContextAtFileEnd(t *testing.T) {
	t.Parallel()

	content := "line0\nline1\nerrorLine\n"
	file := newMockFile(content)
	errorStart := strings.Index(content, "errorLine")

	var buf strings.Builder
	formatOpts := &FormattingOptions{
		NewLine:              "\n",
		ExpandedErrorContext: 3,
	}
	writeCodeSnippet(&buf, file, errorStart, len("errorLine"), foregroundColorEscapeRed, "", formatOpts)
	output := buf.String()

	// Error is near the end - should show lines above but none beyond file end
	assert.Assert(t, strings.Contains(output, "line0"))     // context above
	assert.Assert(t, strings.Contains(output, "line1"))     // context above
	assert.Assert(t, strings.Contains(output, "errorLine")) // error line
}
