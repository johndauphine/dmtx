package app

import "github.com/johndauphine/dmtx/internal/config"

// appendConfigDiagnostics exposes parser compatibility warnings through the
// shared application outcome, so CLI, API, and WebUI operators receive the
// same deprecation notice before execution continues.
func appendConfigDiagnostics(out *outcomeBuilder, cfg config.Config) {
	for _, diagnostic := range cfg.Diagnostics() {
		out.fail("warning: " + diagnostic.Message)
	}
}
