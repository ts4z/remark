package mdparser

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/BurntSushi/toml"
	"github.com/adrg/frontmatter"
)

// fmFormat identifies a frontmatter format.
type fmFormat int

const (
	fmNone fmFormat = iota
	fmYAML
	fmTOML
	fmJSON
)

// detectFrontmatterFormat checks the first non-blank line of source to
// determine the Hugo frontmatter format.
func detectFrontmatterFormat(source []byte) fmFormat {
	for _, line := range bytes.Split(source, []byte("\n")) {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		switch string(trimmed) {
		case "---":
			return fmYAML
		case "+++":
			return fmTOML
		case "{":
			return fmJSON
		}
		return fmNone
	}
	return fmNone
}

// extractFrontmatter splits source into frontmatter output bytes and the
// remaining markdown body.  If no frontmatter is detected, fmOutput is nil
// and rest is the original source.
//
// YAML frontmatter is preserved verbatim.  JSON and TOML frontmatter are
// re-indented.
func extractFrontmatter(source []byte) (fmOutput []byte, rest []byte, err error) {
	format := detectFrontmatterFormat(source)
	if format == fmNone {
		return nil, source, nil
	}

	var data map[string]interface{}
	rest, err = frontmatter.Parse(bytes.NewReader(source), &data)
	if err != nil {
		return nil, nil, fmt.Errorf("frontmatter: %w", err)
	}

	// Original frontmatter bytes (including delimiters).
	originalFM := source[:len(source)-len(rest)]

	switch format {
	case fmYAML:
		fmOutput = originalFM
	case fmJSON:
		encoded, jerr := json.MarshalIndent(data, "", "  ")
		if jerr != nil {
			fmOutput = originalFM
			break
		}
		fmOutput = append(encoded, '\n')
	case fmTOML:
		var buf bytes.Buffer
		buf.WriteString("+++\n")
		if terr := toml.NewEncoder(&buf).Encode(data); terr != nil {
			fmOutput = originalFM
			break
		}
		buf.WriteString("+++\n")
		fmOutput = buf.Bytes()
	}

	return fmOutput, rest, nil
}
