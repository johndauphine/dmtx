package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Finding is a bounded, display-only advisory. It contains no raw evidence.
type Finding struct {
	Category string `json:"category"`
	Title    string `json:"title"`
	Summary  string `json:"summary"`
	Action   string `json:"action,omitempty"`
}

type Advisory struct {
	Summary  string    `json:"summary"`
	Findings []Finding `json:"findings,omitempty"`
	Warnings []string  `json:"warnings,omitempty"`
}

// ParseFailureClass is a safe, non-content classification of model output
// that could not be converted into a supported advisory.
type ParseFailureClass string

const (
	ParseFailureEmpty            ParseFailureClass = "empty"
	ParseFailureTooLarge         ParseFailureClass = "too_large"
	ParseFailureFenceShape       ParseFailureClass = "fence_shape"
	ParseFailureJSONSyntax       ParseFailureClass = "json_syntax"
	ParseFailureSchemaValidation ParseFailureClass = "schema_validation"
)

// AdvisoryParseError never retains or returns model response text.
type AdvisoryParseError struct {
	Class ParseFailureClass
}

func (e *AdvisoryParseError) Error() string {
	return "AI advisory response parse failed: " + string(e.Class)
}

func ParseFailureClassOf(err error) ParseFailureClass {
	var parseErr *AdvisoryParseError
	if errors.As(err, &parseErr) && parseErr.Class != "" {
		return parseErr.Class
	}
	return ParseFailureSchemaValidation
}

// DecodeAdvisory strictly validates the model response before it reaches any
// CLI/API/console output. Unknown fields, oversized text, prose, and multiple
// JSON values are rejected.
func DecodeAdvisory(data string) (Advisory, error) {
	if len(data) == 0 {
		return Advisory{}, &AdvisoryParseError{Class: ParseFailureEmpty}
	}
	if len(data) > 128<<10 {
		return Advisory{}, &AdvisoryParseError{Class: ParseFailureTooLarge}
	}
	normalized, class := normalizeAdvisoryJSON(data)
	if class != "" {
		return Advisory{}, &AdvisoryParseError{Class: class}
	}
	if normalized == "" {
		return Advisory{}, &AdvisoryParseError{Class: ParseFailureEmpty}
	}
	if !json.Valid([]byte(normalized)) {
		return Advisory{}, &AdvisoryParseError{Class: ParseFailureJSONSyntax}
	}
	decoder := json.NewDecoder(strings.NewReader(normalized))
	decoder.DisallowUnknownFields()
	var advisory Advisory
	if err := decoder.Decode(&advisory); err != nil {
		return Advisory{}, &AdvisoryParseError{Class: ParseFailureSchemaValidation}
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Advisory{}, &AdvisoryParseError{Class: ParseFailureJSONSyntax}
	}
	if strings.TrimSpace(advisory.Summary) == "" {
		return Advisory{}, &AdvisoryParseError{Class: ParseFailureSchemaValidation}
	}
	if len(advisory.Findings) > 32 || len(advisory.Warnings) > 32 {
		return Advisory{}, &AdvisoryParseError{Class: ParseFailureSchemaValidation}
	}
	if len(advisory.Summary) > 8192 {
		return Advisory{}, &AdvisoryParseError{Class: ParseFailureSchemaValidation}
	}
	for _, finding := range advisory.Findings {
		if strings.TrimSpace(finding.Title) == "" || len(finding.Title) > 512 || len(finding.Summary) > 4096 || len(finding.Action) > 2048 {
			return Advisory{}, &AdvisoryParseError{Class: ParseFailureSchemaValidation}
		}
	}
	return advisory, nil
}

func normalizeAdvisoryJSON(data string) (string, ParseFailureClass) {
	trimmed := strings.TrimSpace(strings.TrimPrefix(data, "\ufeff"))
	if trimmed == "" {
		return "", ParseFailureEmpty
	}
	fence := strings.Repeat(string(rune(96)), 3)
	if strings.HasPrefix(trimmed, fence) {
		lines := strings.Split(trimmed, "\n")
		if len(lines) < 3 {
			return "", ParseFailureFenceShape
		}
		opening := strings.ToLower(strings.TrimSpace(lines[0]))
		closing := strings.TrimSpace(lines[len(lines)-1])
		if (opening != fence && opening != fence+"json") || closing != fence {
			return "", ParseFailureFenceShape
		}
		body := strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
		if body == "" {
			return "", ParseFailureEmpty
		}
		return body, ""
	}
	if strings.Contains(trimmed, fence) {
		return "", ParseFailureFenceShape
	}
	return trimmed, ""
}

func (a Advisory) Validate() error {
	if strings.TrimSpace(a.Summary) == "" {
		return fmt.Errorf("AI advisory summary is required")
	}
	return nil
}
