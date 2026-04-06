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

func TestWriteCodeSnippetContextWithMultiLineError(t *testing.T) {
	t.Parallel()

	// Error spans two lines (line2 and line3)
	content := "line0\nline1\nline2\nline3\nline4\nline5\n"
	file := newMockFile(content)
	errorStart := strings.Index(content, "line2")
	errorLen := len("line2\nline3")

	var buf strings.Builder
	formatOpts := &FormattingOptions{
		NewLine:              "\n",
		ExpandedErrorContext: 1,
	}
	writeCodeSnippet(&buf, file, errorStart, errorLen, foregroundColorEscapeRed, "", formatOpts)
	output := buf.String()

	// With 1 context line, should show line1 (above), line2+line3 (error), line4 (below)
	assert.Assert(t, strings.Contains(output, "line1"), "expected context line above") // context above
	assert.Assert(t, strings.Contains(output, "line2"), "expected error line 1")       // error line
	assert.Assert(t, strings.Contains(output, "line3"), "expected error line 2")       // error line
	assert.Assert(t, strings.Contains(output, "line4"), "expected context line below") // context below
	assert.Assert(t, !strings.Contains(output, "line0"), "should not contain line0")
	assert.Assert(t, !strings.Contains(output, "line5"), "should not contain line5")
}

func TestWriteCodeSnippetContextWithZeroLengthSpan(t *testing.T) {
	t.Parallel()

	content := "line0\nline1\nline2\nline3\n"
	file := newMockFile(content)
	// Zero-length error at start of "line1"
	errorStart := strings.Index(content, "line1")

	var buf strings.Builder
	formatOpts := &FormattingOptions{
		NewLine:              "\n",
		ExpandedErrorContext: 1,
	}
	writeCodeSnippet(&buf, file, errorStart, 0, foregroundColorEscapeRed, "", formatOpts)
	output := buf.String()

	// Should still show the error line with context
	assert.Assert(t, strings.Contains(output, "line0"), "expected context line above")
	assert.Assert(t, strings.Contains(output, "line1"), "expected error line")
	assert.Assert(t, strings.Contains(output, "line2"), "expected context line below")
	assert.Assert(t, !strings.Contains(output, "line3"), "should not contain line3")
}

func TestWriteCodeSnippetContextWithIndent(t *testing.T) {
	t.Parallel()

	content := "line0\nline1\nline2\nline3\n"
	file := newMockFile(content)
	errorStart := strings.Index(content, "line1")

	var buf strings.Builder
	indent := "    "
	formatOpts := &FormattingOptions{
		NewLine:              "\n",
		ExpandedErrorContext: 1,
	}
	writeCodeSnippet(&buf, file, errorStart, len("line1"), foregroundColorEscapeRed, indent, formatOpts)
	output := buf.String()

	// All lines (error + context) should have the indent
	lines := strings.Split(output, "\n")
	nonEmptyLines := 0
	for _, line := range lines {
		if line == "" {
			continue
		}
		nonEmptyLines++
		assert.Assert(t, strings.HasPrefix(line, indent) || strings.HasPrefix(line, foregroundColorEscapeGrey),
			"expected line to have indent or grey prefix, got: %q", line)
	}
	assert.Assert(t, nonEmptyLines > 0, "expected non-empty output")
}

func TestWriteCodeSnippetContextWithTabs(t *testing.T) {
	t.Parallel()

	content := "\tindented0\n\tindented1\n\tindented2\n\tindented3\n"
	file := newMockFile(content)
	errorStart := strings.Index(content, "\tindented1")

	var buf strings.Builder
	formatOpts := &FormattingOptions{
		NewLine:              "\n",
		ExpandedErrorContext: 1,
	}
	writeCodeSnippet(&buf, file, errorStart, len("\tindented1"), foregroundColorEscapeRed, "", formatOpts)
	output := buf.String()

	// Tabs should be converted to spaces
	assert.Assert(t, !strings.Contains(output, "\t"), "tabs should be converted to spaces")
	assert.Assert(t, strings.Contains(output, "indented0"), "expected context line above")
	assert.Assert(t, strings.Contains(output, "indented1"), "expected error line")
	assert.Assert(t, strings.Contains(output, "indented2"), "expected context line below")
}

func TestWriteCodeSnippetContextSingleLineFile(t *testing.T) {
	t.Parallel()

	content := "only line"
	file := newMockFile(content)

	var buf strings.Builder
	formatOpts := &FormattingOptions{
		NewLine:              "\n",
		ExpandedErrorContext: 5,
	}
	writeCodeSnippet(&buf, file, 0, len("only line"), foregroundColorEscapeRed, "", formatOpts)
	output := buf.String()

	// Should show the single line without panic
	assert.Assert(t, strings.Contains(output, "only line"), "expected the single line to appear")
}

func TestWriteCodeSnippetContextLinesAreGrey(t *testing.T) {
	t.Parallel()

	content := "line0\nline1\nline2\nline3\nline4\n"
	file := newMockFile(content)
	errorStart := strings.Index(content, "line2")

	var buf strings.Builder
	formatOpts := &FormattingOptions{
		NewLine:              "\n",
		ExpandedErrorContext: 1,
	}
	writeCodeSnippet(&buf, file, errorStart, len("line2"), foregroundColorEscapeRed, "", formatOpts)
	output := buf.String()

	// Context lines should use grey styling, error lines use gutter style
	// Grey escape is used for context line numbers and content
	assert.Assert(t, strings.Contains(output, foregroundColorEscapeGrey), "context lines should use grey styling")
	assert.Assert(t, strings.Contains(output, gutterStyleSequence), "error lines should use gutter styling")
}

func TestWriteCodeSnippetContextGutterWidthWithHighLineNumbers(t *testing.T) {
	t.Parallel()

	// Create a file where context lines push the gutter width wider
	// Error on line 1 (0-indexed), with context showing lines through line 999+
	var sb strings.Builder
	for i := range 12 {
		sb.WriteString("line")
		sb.WriteString(strings.Repeat("x", i))
		sb.WriteString("\n")
	}
	content := sb.String()
	file := newMockFile(content)
	// Error on line 0
	errorStart := 0
	errorLen := len("line")

	var buf strings.Builder
	formatOpts := &FormattingOptions{
		NewLine:              "\n",
		ExpandedErrorContext: 11, // Show all 12 lines
	}
	writeCodeSnippet(&buf, file, errorStart, errorLen, foregroundColorEscapeRed, "", formatOpts)
	output := buf.String()

	// All lines should be present
	assert.Assert(t, strings.Contains(output, "line"), "expected error line")
	// Line numbers should be right-aligned - verify line 1 and line 12 are both present
	assert.Assert(t, strings.Contains(output, "1"), "expected line number 1")
	assert.Assert(t, strings.Contains(output, "12"), "expected line number 12")
}

func TestWriteCodeSnippetNoContextDoesNotChangeExistingBehavior(t *testing.T) {
	t.Parallel()

	content := "line0\nline1\nline2\nline3\n"
	file := newMockFile(content)
	errorStart := strings.Index(content, "line1")

	// With context = 0, output should be same as if ExpandedErrorContext is not set
	var bufWithZero strings.Builder
	var bufDefault strings.Builder

	optsZero := &FormattingOptions{
		NewLine:              "\n",
		ExpandedErrorContext: 0,
	}
	optsDefault := &FormattingOptions{
		NewLine: "\n",
	}

	writeCodeSnippet(&bufWithZero, file, errorStart, len("line1"), foregroundColorEscapeRed, "", optsZero)
	writeCodeSnippet(&bufDefault, file, errorStart, len("line1"), foregroundColorEscapeRed, "", optsDefault)

	assert.Equal(t, bufWithZero.String(), bufDefault.String(), "context=0 should produce same output as default")
}

func TestWriteCodeSnippetContextWithErrorOnLastLine(t *testing.T) {
	t.Parallel()

	content := "line0\nline1\nline2\nlastLine"
	file := newMockFile(content)
	errorStart := strings.Index(content, "lastLine")

	var buf strings.Builder
	formatOpts := &FormattingOptions{
		NewLine:              "\n",
		ExpandedErrorContext: 2,
	}
	writeCodeSnippet(&buf, file, errorStart, len("lastLine"), foregroundColorEscapeRed, "", formatOpts)
	output := buf.String()

	// Error on last line with no trailing newline
	assert.Assert(t, strings.Contains(output, "line1"), "expected context line above")
	assert.Assert(t, strings.Contains(output, "line2"), "expected context line above")
	assert.Assert(t, strings.Contains(output, "lastLine"), "expected error line")
}

func TestWriteCodeSnippetContextWithEmptyLines(t *testing.T) {
	t.Parallel()

	content := "before\n\nerrorLine\n\nafter\n"
	file := newMockFile(content)
	errorStart := strings.Index(content, "errorLine")

	var buf strings.Builder
	formatOpts := &FormattingOptions{
		NewLine:              "\n",
		ExpandedErrorContext: 2,
	}
	writeCodeSnippet(&buf, file, errorStart, len("errorLine"), foregroundColorEscapeRed, "", formatOpts)
	output := buf.String()

	// Should handle empty lines in context
	assert.Assert(t, strings.Contains(output, "before"), "expected context line 'before'")
	assert.Assert(t, strings.Contains(output, "errorLine"), "expected error line")
	assert.Assert(t, strings.Contains(output, "after"), "expected context line 'after'")
}
