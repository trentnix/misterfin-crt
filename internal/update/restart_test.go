package update

import (
	"errors"
	"fmt"
	"testing"
)

func TestRestartRequiresSuccessfulCleanup(t *testing.T) {
	failure := errors.New("display cleanup failed")
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"normal exit", nil, false},
		{"restart", ErrRestart, true},
		{"wrapped restart", fmt.Errorf("browser: %w", ErrRestart), true},
		{"clean deferred closes", errors.Join(errors.Join(ErrRestart, nil), nil), true},
		{"failed cleanup", errors.Join(ErrRestart, failure), false},
		{"nested failed cleanup", errors.Join(errors.Join(ErrRestart, failure), nil), false},
		{"recovery required", ErrRecovery, false},
		{"ordinary failure", failure, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := RestartRequested(tc.err); got != tc.want {
				t.Fatalf("RestartRequested(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
