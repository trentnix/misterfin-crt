package main

import (
	"context"
	"fmt"
	"time"

	"misterfin-crt/internal/platform"
	"misterfin-crt/internal/testframe"
)

// runPreview presents one test frame and optionally waits for a duration or
// cancellation. It borrows d. The caller closes the display after it returns.
func runPreview(ctx context.Context, d platform.Display, o launchOptions) error {
	if err := testframe.Present(d); err != nil {
		return err
	}
	g := d.Geometry()
	fmt.Printf("Presented BGRX test frame: logical %dx%d, output %dx%d\n", g.Width, g.Height, g.OutputWidth, g.OutputHeight)
	if o.wait {
		<-ctx.Done()
	} else if o.hold > 0 {
		timer := time.NewTimer(o.hold)
		defer timer.Stop()
		select {
		case <-ctx.Done():
		case <-timer.C:
		}
	}
	return nil
}
