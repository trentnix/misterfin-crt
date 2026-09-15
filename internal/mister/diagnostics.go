// Package mister provides MiSTer-specific startup observations. Display control
// and background-music coordination remain in their respective subpackages.
package mister

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"misterfin-crt/internal/diagnostics"
)

// RecordStartup reads a bounded snapshot of display settings when logging is
// enabled. INI entries describe configured values, not resolved hardware state.
// It never changes settings or reads credentials, device serials, or environments.
func RecordStartup(log *diagnostics.Log, interlaced bool) {
	if log == nil {
		return
	}
	log.Record("mister.display", slog.Bool("interlaced", interlaced))
	f, err := os.Open("/media/fat/MiSTer.ini")
	if err != nil {
		log.Record("mister.settings", slog.String("error_kind", diagnostics.ErrorKind(err)))
	} else {
		recordSettings(log, f)
		f.Close()
	}
	f, err = os.Open("/sys/module/MiSTer_fb/parameters/mode")
	if err != nil {
		log.Record("mister.framebuffer", slog.String("error_kind", diagnostics.ErrorKind(err)))
		return
	}
	defer f.Close()
	var format, swap, width, height, stride int
	n, err := fmt.Fscanf(io.LimitReader(f, 256), "%d %d %d %d %d", &format, &swap, &width, &height, &stride)
	if err != nil || n != 5 {
		log.Record("mister.framebuffer", slog.String("error_kind", "invalid-mode"))
		return
	}
	log.Record("mister.framebuffer", slog.Int("format", format), slog.Int("swap", swap), slog.Int("width", width), slog.Int("height", height), slog.Int("stride", stride))
}

// recordSettings preserves section identity rather than guessing INI precedence.
// Only display-related sections and numeric display values are eligible. Limits
// bound startup reads and queue use even when the INI is unexpectedly large.
func recordSettings(log *diagnostics.Log, source io.Reader) {
	const limit = 128 << 10
	reader := &io.LimitedReader{R: source, N: limit + 1}
	scan := bufio.NewScanner(reader)
	section := "top"
	entries, rejected := 0, 0
	for scan.Scan() {
		line := strings.TrimSpace(strings.SplitN(strings.SplitN(scan.Text(), ";", 2)[0], "#", 2)[0])
		if strings.HasPrefix(line, "[") {
			section = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")))
			continue
		}
		switch section {
		case "top", "mister", "menu", "misterfininterlaced":
		default:
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, value = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value)
		switch key {
		case "ypbpr", "composite_sync", "forced_scandoubler", "vga_scaler", "direct_video", "vsync_adjust", "video_mode", "video_mode_ntsc", "video_mode_pal":
		default:
			continue
		}
		if !numericSetting(value) {
			rejected++
			continue
		}
		if entries == 64 {
			break
		}
		log.Record("mister.setting", slog.String("section", section), slog.String("key", key), slog.String("value", value))
		entries++
	}
	log.Record("mister.settings", slog.Int("entries", entries), slog.Int("rejected_values", rejected), slog.Bool("limited", reader.N == 0 || entries == 64), slog.Bool("read_failed", scan.Err() != nil))
}

// numericSetting permits numeric mode indices and timing tuples, never arbitrary
// INI strings. Unknown value formats are counted but not copied into the log.
func numericSetting(value string) bool {
	if len(value) == 0 || len(value) > 192 {
		return false
	}
	digit := false
	for _, c := range value {
		if c >= '0' && c <= '9' {
			digit = true
			continue
		}
		if !strings.ContainsRune(" ,.+-\t", c) {
			return false
		}
	}
	return digit
}
