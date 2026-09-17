# Build the Go client. The native framebuffer adapter is compiled through cgo.
GO ?= go
STATICCHECK_VERSION := v0.8.1
GOVULNCHECK_VERSION := v1.8.0
VERSION ?= dev
GO_LDFLAGS = -X mistervision/internal/release.Version=$(VERSION)
GO_ARM_CC ?= sh $(CURDIR)/tools/zig-cc-go.sh

.DEFAULT_GOAL := host

.PHONY: host arm lint vulnerability-check native-player release-manifest release test test-browse headless clean
host:
	CGO_ENABLED=1 $(GO) build -trimpath -ldflags "$(GO_LDFLAGS)" -o build/mistervision ./cmd/mistervision

arm:
	CGO_ENABLED=1 GOOS=linux GOARCH=arm GOARM=7 CC="$(GO_ARM_CC)" $(GO) build -trimpath -ldflags "$(GO_LDFLAGS)" -o build/mistervision-arm ./cmd/mistervision

# A versioned tool invocation leaves go.mod and go.sum unchanged.
lint:
	@files="$$(gofmt -l cmd internal)"; if [ -n "$$files" ]; then printf '%s\n' "$$files"; exit 1; fi
	$(GO) vet ./...
	$(GO) run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) ./...

vulnerability-check:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

native-player:
	mkdir -p build
	docker build -f docker/Dockerfile.mistervision -t mistervision-mplayer docker
	docker run --rm --mount "type=bind,src=$(CURDIR)/build,dst=/output" -e OUTPUT_DIR=/output mistervision-mplayer

# Run after arm and native-player so the manifest describes the matching pair.
release-manifest:
	install -m 644 "$$($(GO) env GOROOT)/LICENSE" build/go-LICENSE
	$(GO) version -m build/mistervision-arm > build/release-manifest.txt
	cat build/mistervision-mplayer-build.txt >> build/release-manifest.txt
	cd build && sha256sum mistervision-arm mistervision-mplayer-arm >> release-manifest.txt

# Build both executables before packaging. Refuse dirty source or an existing bundle.
release:
	python3 tools/package_release.py --version "$(VERSION)" --go "$(GO)"

test:
	CGO_ENABLED=1 $(GO) test ./...
	CGO_ENABLED=0 $(GO) test ./...
	python3 -m unittest -v tools/ghostty/test_ghostty_harness.py tools/ghostty/test_video_player.py tools/test_native_overlay.py tools/test_interlaced_console.py tools/test_native_picture.py tools/test_mplayer_timing.py tools/test_native_captions.py tools/test_package_release.py

test-browse: host
	python3 -m unittest -v tools/ghostty/test_go_browse.py

headless: host
	./build/mistervision -headless 640x288 -output build/go-frame.raw
	python3 tools/raw_to_png.py build/go-frame.raw 640 288 build/go-frame.png

clean:
	rm -f build/mistervision build/mistervision-arm build/go-frame.raw build/go-frame.png
