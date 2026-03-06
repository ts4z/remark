package mdparser

import (
	"bytes"
	"testing"
)

func TestDetectFrontmatterFormat(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   fmFormat
	}{
		{"yaml", "---\ntitle: Hello\n---\n", fmYAML},
		{"toml", "+++\ntitle = \"Hello\"\n+++\n", fmTOML},
		{"json", "{\n\"title\": \"Hello\"\n}\n", fmJSON},
		{"none plain md", "# Hello\n", fmNone},
		{"empty", "", fmNone},
		{"yaml with leading blank", "\n---\ntitle: Hello\n---\n", fmYAML},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectFrontmatterFormat([]byte(tt.source))
			if got != tt.want {
				t.Errorf("detectFrontmatterFormat(%q) = %d, want %d", tt.source, got, tt.want)
			}
		})
	}
}

func TestFrontmatterYAML(t *testing.T) {
	source := "---\ntitle: Hello\ndate: 2024-01-01\n---\n\n# Content\n\nSome text.\n"
	// Frontmatter is on by default, no need to pass WithFrontmatter.
	got := roundTripWith(t, source, WithWidth(79))

	want := "---\ntitle: Hello\ndate: 2024-01-01\n---\n\n# Content\n\nSome text.\n"
	if got != want {
		t.Errorf("YAML frontmatter round-trip:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestFrontmatterTOML(t *testing.T) {
	source := "+++\ntitle = \"Hello\"\n+++\n\n# Content\n"
	got := roundTripWith(t, source, WithWidth(79))

	if !bytes.HasPrefix([]byte(got), []byte("+++\n")) {
		t.Errorf("TOML output should start with +++\\n, got %q", got)
	}
	if !bytes.Contains([]byte(got), []byte("title")) {
		t.Errorf("TOML output should contain 'title', got %q", got)
	}
	if !bytes.Contains([]byte(got), []byte("# Content\n")) {
		t.Errorf("TOML output should contain rendered content, got %q", got)
	}
}

func TestFrontmatterJSON(t *testing.T) {
	source := "{\n\"title\": \"Hello\",\n\"draft\": true\n}\n\n# Content\n"
	got := roundTripWith(t, source, WithWidth(79))

	if !bytes.Contains([]byte(got), []byte("\"title\": \"Hello\"")) {
		t.Errorf("JSON output should contain re-indented title, got %q", got)
	}
	if !bytes.Contains([]byte(got), []byte("# Content\n")) {
		t.Errorf("JSON output should contain rendered content, got %q", got)
	}
}

func TestFrontmatterDisabled(t *testing.T) {
	// With --no-frontmatter (WithFrontmatter(false)), the --- delimiters
	// are treated as regular markdown (thematic breaks / paragraphs).
	source := "---\ntitle: Hello\n---\n\n# Content\n"
	got := roundTripWith(t, source, WithWidth(79), WithFrontmatter(false))

	if bytes.HasPrefix([]byte(got), []byte("---\ntitle:")) {
		t.Errorf("with frontmatter disabled, should not preserve frontmatter block, got %q", got)
	}
}

func TestFrontmatterDisabledWarning(t *testing.T) {
	// When frontmatter is disabled but detected, the warn callback fires.
	source := "---\ntitle: Hello\n---\n\n# Content\n"
	var warned bool
	warnFn := func(format string, args ...interface{}) {
		warned = true
	}
	_ = roundTripWith(t, source, WithWidth(79), WithFrontmatter(false), WithWarnFunc(warnFn))
	if !warned {
		t.Errorf("expected warning when frontmatter detected but disabled")
	}
}

func TestFrontmatterDisabledNoWarningWhenQuiet(t *testing.T) {
	// When frontmatter is disabled and no warn func is set (quiet mode),
	// no warning is emitted (and no panic).
	source := "---\ntitle: Hello\n---\n\n# Content\n"
	_ = roundTripWith(t, source, WithWidth(79), WithFrontmatter(false), WithWarnFunc(nil))
	// Just verify no panic occurs.
}

func TestFrontmatterNoContent(t *testing.T) {
	source := "---\ntitle: Hello\n---\n"
	got := roundTripWith(t, source, WithWidth(79))

	if !bytes.HasPrefix([]byte(got), []byte("---\ntitle: Hello\n---\n")) {
		t.Errorf("frontmatter-only file should preserve frontmatter, got %q", got)
	}
}

func TestNoFrontmatterPlainMarkdown(t *testing.T) {
	// Plain markdown without frontmatter should work fine with default settings.
	source := "# Hello\n\nSome text.\n"
	got := roundTripWith(t, source, WithWidth(79))
	want := "# Hello\n\nSome text.\n"
	if got != want {
		t.Errorf("plain markdown round-trip:\ngot:\n%s\nwant:\n%s", got, want)
	}
}
