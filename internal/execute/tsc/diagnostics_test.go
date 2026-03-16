package tsc

import (
	"io"
	"testing"
	"time"

	"github.com/microsoft/typescript-go/internal/core"
	"github.com/microsoft/typescript-go/internal/vfs"
	"gotest.tools/v3/assert"
)

// mockSystem implements the System interface for testing.
type mockSystem struct {
	envVars map[string]string
}

func (m *mockSystem) GetEnvironmentVariable(name string) string {
	return m.envVars[name]
}

func (m *mockSystem) Writer() io.Writer                { return io.Discard }
func (m *mockSystem) FS() vfs.FS                       { return nil }
func (m *mockSystem) DefaultLibraryPath() string       { return "" }
func (m *mockSystem) GetCurrentDirectory() string      { return "" }
func (m *mockSystem) WriteOutputIsTTY() bool           { return false }
func (m *mockSystem) GetWidthOfTerminal() int          { return 80 }
func (m *mockSystem) Now() time.Time                   { return time.Time{} }
func (m *mockSystem) SinceStart() time.Duration        { return 0 }

func TestGetExpandedErrorContext(t *testing.T) {
	t.Parallel()

	intPtr := func(v int) *int { return &v }

	tests := []struct {
		name     string
		options  *core.CompilerOptions
		envVars  map[string]string
		expected int
	}{
		{
			name:     "defaults to 0 with no option or env",
			options:  &core.CompilerOptions{},
			expected: 0,
		},
		{
			name:     "uses compiler option when set",
			options:  &core.CompilerOptions{ExpandedErrorContext: intPtr(4)},
			expected: 4,
		},
		{
			name:     "uses env var when option not set",
			options:  &core.CompilerOptions{},
			envVars:  map[string]string{"TSC_EEC": "3"},
			expected: 3,
		},
		{
			name:     "compiler option takes priority over env var",
			options:  &core.CompilerOptions{ExpandedErrorContext: intPtr(2)},
			envVars:  map[string]string{"TSC_EEC": "5"},
			expected: 2,
		},
		{
			name:     "ignores invalid env var",
			options:  &core.CompilerOptions{},
			envVars:  map[string]string{"TSC_EEC": "abc"},
			expected: 0,
		},
		{
			name:     "ignores negative env var",
			options:  &core.CompilerOptions{},
			envVars:  map[string]string{"TSC_EEC": "-1"},
			expected: 0,
		},
		{
			name:     "ignores negative compiler option",
			options:  &core.CompilerOptions{ExpandedErrorContext: intPtr(-5)},
			expected: 0,
		},
		{
			name:     "nil options defaults to env var",
			options:  nil,
			envVars:  map[string]string{"TSC_EEC": "6"},
			expected: 6,
		},
		{
			name:     "compiler option of zero",
			options:  &core.CompilerOptions{ExpandedErrorContext: intPtr(0)},
			expected: 0,
		},
		{
			name:     "compiler option of zero takes priority over env var",
			options:  &core.CompilerOptions{ExpandedErrorContext: intPtr(0)},
			envVars:  map[string]string{"TSC_EEC": "5"},
			expected: 0,
		},
		{
			name:     "env var of zero",
			options:  &core.CompilerOptions{},
			envVars:  map[string]string{"TSC_EEC": "0"},
			expected: 0,
		},
		{
			name:     "empty string env var ignored",
			options:  &core.CompilerOptions{},
			envVars:  map[string]string{"TSC_EEC": ""},
			expected: 0,
		},
		{
			name:     "env var with whitespace ignored",
			options:  &core.CompilerOptions{},
			envVars:  map[string]string{"TSC_EEC": " 3 "},
			expected: 0,
		},
		{
			name:     "env var with float ignored",
			options:  &core.CompilerOptions{},
			envVars:  map[string]string{"TSC_EEC": "3.5"},
			expected: 0,
		},
		{
			name:     "very large compiler option value",
			options:  &core.CompilerOptions{ExpandedErrorContext: intPtr(10000)},
			expected: 10000,
		},
		{
			name:     "very large env var value",
			options:  &core.CompilerOptions{},
			envVars:  map[string]string{"TSC_EEC": "10000"},
			expected: 10000,
		},
		{
			name:     "nil options with no env var",
			options:  nil,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sys := &mockSystem{envVars: tt.envVars}
			result := getExpandedErrorContext(sys, tt.options)
			assert.Equal(t, result, tt.expected)
		})
	}
}
