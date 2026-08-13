package app

import (
	"regexp"
	"strings"
)

// Public output is a trust boundary. Commands usually classify expected
// failures themselves, but a driver or another lower layer can still attach a
// diagnostic that includes the password or DSN it was given. Renderers and
// long-lived API jobs must never relay that text verbatim.
//
// These expressions deliberately require a credential-shaped value (an
// assignment, a JSON property, a command flag, or URI user-info). A table
// called "tokens", or an ordinary message mentioning a password, stays useful
// to an operator; only the value that could authenticate is removed.
var (
	publicCredentialName = `password|passwd|pwd|secret|token|api[_ -]?key|access[_ -]?token|client[_ -]?secret|credential`
	publicAssignment     = regexp.MustCompile(`(?i)\b(` + publicCredentialName + `)\b\s*([=:])\s*("(\\.|[^"\\])*"|'(\\.|[^'\\])*'|[^\s;,&]+)`)
	publicJSONCredential = regexp.MustCompile(`(?i)(["'](` + publicCredentialName + `)["']\s*:\s*)("(\\.|[^"\\])*"|'(\\.|[^'\\])*'|[^\s,}]+)`)
	publicCredentialFlag = regexp.MustCompile(`(?i)(--(` + publicCredentialName + `)|-[pP])\s+("(\\.|[^"\\])*"|'(\\.|[^'\\])*'|\S+)`)
	// Any nonempty authority userinfo is sensitive. It need not be a
	// username:password pair: URL schemes also permit an empty username or a
	// token-only userinfo value before @.
	publicURIUserInfo   = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^\s/@]+@`)
	publicBearer        = regexp.MustCompile(`(?i)\bbearer\s+("(\\.|[^"\\])*"|'(\\.|[^'\\])*'|[^\s,;]+)`)
	publicAuthorization = regexp.MustCompile(`(?i)\bauthorization\s*:\s*[^\r\n]+`)
	// Unterminated quoted diagnostics are malformed, but must not leak the
	// suffix after the first whitespace. Each pattern is line-anchored so it
	// only wins when no unescaped closing quote exists before line end.
	publicUnterminatedAssignment = regexp.MustCompile(`(?im)\b(` + publicCredentialName + `)\b\s*([=:])\s*("(\\.|[^"\\])*|'(\\.|[^'\\])*)$`)
	publicUnterminatedJSON       = regexp.MustCompile(`(?im)(["'](` + publicCredentialName + `)["']\s*:\s*)("(\\.|[^"\\])*|'(\\.|[^'\\])*)$`)
	publicUnterminatedFlag       = regexp.MustCompile(`(?im)(--(` + publicCredentialName + `)|-[pP])\s+("(\\.|[^"\\])*|'(\\.|[^'\\])*)$`)
	publicUnterminatedBearer     = regexp.MustCompile(`(?im)(\bbearer)\s+("(\\.|[^"\\])*|'(\\.|[^'\\])*)$`)
)

var bearerProse = map[string]bool{
	"authentication": true,
	"credentials":    true,
	"header":         true,
	"scheme":         true,
	"token":          true,
}

// RedactPublicOutcome returns the safe presentation form of an Outcome. It
// preserves command, exit classification, stream selection, and app-owned
// structured payloads. Payload schemas are separately wire-gated and are not
// text-scrubbed here: changing arbitrary JSON would corrupt the public command
// contract and could conceal a schema regression.
func RedactPublicOutcome(outcome Outcome) Outcome {
	public := outcome
	if outcome.Messages == nil {
		return public
	}
	public.Messages = make([]Message, len(outcome.Messages))
	for index, message := range outcome.Messages {
		public.Messages[index] = message
		public.Messages[index].Text = redactPublicDiagnostic(message.Text)
	}
	return public
}

// RedactPublicProgress returns the safe presentation form of a progress
// record. Table names remain intact unless a broken lower layer supplied a
// credential-shaped diagnostic in one of the string fields; counters and kind
// remain exactly as reported.
func RedactPublicProgress(progress Progress) Progress {
	public := progress
	public.Table = redactPublicDiagnostic(progress.Table)
	if progress.Tables != nil {
		public.Tables = make([]string, len(progress.Tables))
		for index, table := range progress.Tables {
			public.Tables[index] = redactPublicDiagnostic(table)
		}
	}
	return public
}

func redactPublicDiagnostic(text string) string {
	text = publicURIUserInfo.ReplaceAllString(text, `${1}[REDACTED]@`)
	text = publicAuthorization.ReplaceAllStringFunc(text, redactAuthorizationHeader)
	text = publicUnterminatedJSON.ReplaceAllString(text, `${1}"[REDACTED]"`)
	text = publicUnterminatedAssignment.ReplaceAllString(text, `${1}${2}[REDACTED]`)
	text = publicUnterminatedFlag.ReplaceAllString(text, `${1} [REDACTED]`)
	text = publicUnterminatedBearer.ReplaceAllString(text, `${1} [REDACTED]`)
	text = publicJSONCredential.ReplaceAllString(text, `${1}"[REDACTED]"`)
	text = redactBearerCredentials(text)
	text = publicAssignment.ReplaceAllString(text, `${1}${2}[REDACTED]`)
	return publicCredentialFlag.ReplaceAllString(text, `${1} [REDACTED]`)
}

// redactAuthorizationHeader removes every authorization scheme and value to
// the end of its header line. Schemes such as Digest and AWS can contain
// multiple comma-separated credential parameters, so replacing only a first
// token would leave suffixes behind.
func redactAuthorizationHeader(match string) string {
	if colon := strings.IndexByte(match, ':'); colon >= 0 {
		return match[:colon+1] + " [REDACTED]"
	}
	return "Authorization: [REDACTED]"
}

// redactBearerCredentials handles standalone Bearer values. The normal
// assignment rule cannot distinguish `Bearer X` from ordinary prose that
// merely mentions the scheme.
func redactBearerCredentials(text string) string {
	return publicBearer.ReplaceAllStringFunc(text, func(match string) string {
		lower := strings.ToLower(match)
		position := strings.Index(lower, "bearer")
		prefixEnd := position + len("bearer")
		value := strings.TrimSpace(match[prefixEnd:])
		if bearerProse[strings.ToLower(strings.Trim(value, "\"'"))] {
			return match
		}
		return match[:prefixEnd] + " [REDACTED]"
	})
}
