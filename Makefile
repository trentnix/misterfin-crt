# Build the Go client. The native framebuffer adapter is compiled through cgo.
GO ?= go
GO_ARM_CC ?= $(CURDIR)/tools/zig-cc-go.sh

.DEFAULT_GOAL := host

.PHONY: host arm test test-browse headless clean
host:
	CGO_ENABLED=1 $(GO) build -trimpath -o build/misterfin-crt ./cmd/misterfin-crt

arm:
	CGO_ENABLED=1 GOOS=linux GOARCH=arm GOARM=7 CC="$(GO_ARM_CC)" $(GO) build -trimpath -o build/misterfin-crt-arm ./cmd/misterfin-crt

test:
	CGO_ENABLED=1 $(GO) test ./...
	CGO_ENABLED=0 $(GO) test ./...
	python3 -m unittest -v tools/ghostty/test_ghostty_harness.py tools/ghostty/test_video_player.py tools/test_native_overlay.py tools/test_native_picture.py tools/test_mplayer_timing.py

test-browse: host
	python3 -m unittest -v tools/ghostty/test_go_browse.py

headless: host
	./build/misterfin-crt -headless 640x288 -output build/go-frame.raw
	python3 tools/raw_to_png.py build/go-frame.raw 640 288 build/go-frame.png

clean:
	rm -f build/misterfin-crt build/misterfin-crt-arm build/go-frame.raw build/go-frame.png
