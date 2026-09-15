package diagnostics

import (
	"errors"
	"log/slog"
	"os"
)

// ConfigurationFallback records a rejected setting or asset and its replacement
// behavior. Configuration and fallback must be static identifiers, never paths,
// setting values, or notice text. Only the error category is recorded. A nil or
// disabled log does nothing, as with Record. Call when the failure is handled,
// not when its notice is rendered.
func (l *Log) ConfigurationFallback(configuration, fallback string, err error) {
	kind := ErrorKind(err)
	if kind == "other" {
		// Configuration loaders return validation errors or file errors. Preserve
		// the distinction without inspecting potentially sensitive error messages.
		var fileError *os.PathError
		if errors.As(err, &fileError) {
			kind = "file-io"
		} else {
			kind = "invalid"
		}
	}
	l.Record("configuration.fallback", slog.String("configuration", configuration),
		slog.String("error_kind", kind), slog.String("fallback", fallback))
}
