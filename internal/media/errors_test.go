package media

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestNetworkErrorClassifiesWithoutKeepingSecrets(t *testing.T) {
	for _, tc := range []struct{ cause, want error }{
		{context.Canceled, context.Canceled}, {context.DeadlineExceeded, context.DeadlineExceeded},
		{os.ErrDeadlineExceeded, context.DeadlineExceeded}, {errors.New("private network error"), ErrUnavailable},
	} {
		raw := &url.Error{Op: "Get", URL: "https://private/?token=secret", Err: tc.cause}
		err := NetworkError(raw)
		if !errors.Is(err, tc.want) || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "secret") {
			t.Fatalf("unsafe or unclassified error: %v", err)
		}
		if errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, ErrUnavailable) {
			t.Fatal("timeout must remain eligible for server address recovery")
		}
	}
}
