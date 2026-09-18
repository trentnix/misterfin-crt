package update

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	updateapi "mistervision/internal/update"
)

const maxFiles = 128
const maxExpanded = 128 << 20

// allowed limits an update to program files and notices, never active config.
// Forward-compatible license additions stay within the licenses directory.
func allowed(name string) bool {
	if name == "" || strings.ContainsAny(name, "\\\x00") || path.Clean(name) != name || strings.HasPrefix(name, "/") {
		return false
	}
	switch name {
	case "Scripts/MiSTerVision.sh", "INSTALL.txt", "SHA256SUMS":
		return true
	case "mistervision/mistervision", "mistervision/mplayer-arm", "mistervision/VERSION", "mistervision/UPDATE_FORMAT", "mistervision/BUILD.txt", "mistervision/LICENSE", "mistervision/THIRD_PARTY.md", "mistervision/jellyfin.conf.example", "mistervision/settings.example.json":
		return true
	}
	return strings.HasPrefix(name, "mistervision/licenses/")
}

func mode(name string) os.FileMode {
	if name == "Scripts/MiSTerVision.sh" || name == "mistervision/mistervision" || name == "mistervision/mplayer-arm" {
		return 0755
	}
	return 0644
}

func (i *Installer) destination(name string) string {
	switch name {
	case "Scripts/MiSTerVision.sh":
		return i.launcher
	case "INSTALL.txt":
		return filepath.Join(i.root, "INSTALL.txt")
	default:
		return filepath.Join(i.root, strings.TrimPrefix(name, "mistervision/"))
	}
}

func (i *Installer) unpack(ctx context.Context, stage, archive, version string) (result []entry, resultErr error) {
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(updateapi.ErrVerification, resultErr)
		}
	}()
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	if len(reader.File) > maxFiles {
		return nil, errors.New("too many release files")
	}
	files := make(map[string]*zip.File)
	var total uint64
	for _, file := range reader.File {
		if !allowed(file.Name) || !file.Mode().IsRegular() || files[file.Name] != nil {
			return nil, errors.New("unsafe release archive entry")
		}
		if file.UncompressedSize64 > 64<<20 {
			return nil, errors.New("release file is too large")
		}
		total += file.UncompressedSize64
		if total > maxExpanded {
			return nil, errors.New("expanded release is too large")
		}
		files[file.Name] = file
	}
	readSmall := func(name string, limit int64) ([]byte, error) {
		file := files[name]
		if file == nil {
			return nil, errors.New("required release file is missing")
		}
		src, err := file.Open()
		if err != nil {
			return nil, err
		}
		defer src.Close()
		data, err := io.ReadAll(io.LimitReader(src, limit+1))
		if err == nil && int64(len(data)) > limit {
			err = errors.New("release metadata is too large")
		}
		return data, err
	}
	format, err := readSmall("mistervision/UPDATE_FORMAT", 16)
	if err != nil || string(format) != "1\n" {
		return nil, updateapi.ErrManual
	}
	label, err := readSmall("mistervision/VERSION", 64)
	if err != nil || string(label) != version+"\n" {
		return nil, errors.New("release version mismatch")
	}
	data, err := readSmall("SHA256SUMS", 64<<10)
	if err != nil {
		return nil, err
	}
	sums, err := checksums(data)
	if err != nil {
		return nil, err
	}
	if len(sums) != len(files)-1 {
		return nil, errors.New("release file checksums do not match the archive")
	}
	for _, name := range []string{"mistervision/mistervision", "mistervision/mplayer-arm", "Scripts/MiSTerVision.sh", "mistervision/LICENSE", "mistervision/THIRD_PARTY.md", "mistervision/BUILD.txt"} {
		if files[name] == nil {
			return nil, errors.New("required release file is missing")
		}
	}
	names := make([]string, 0, len(sums))
	for name := range sums {
		if name == "SHA256SUMS" || files[name] == nil {
			return nil, errors.New("invalid release file checksum")
		}
		names = append(names, name)
	}
	sort.Strings(names)
	entries := make([]entry, 0, len(names))
	for index, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		src, err := files[name].Open()
		if err != nil {
			return nil, err
		}
		dest := filepath.Join(stage, fmt.Sprintf("new-%d", index))
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			src.Close()
			return nil, err
		}
		hash := sha256.New()
		_, err = io.Copy(io.MultiWriter(out, hash), io.LimitReader(src, (64<<20)+1))
		src.Close()
		if err == nil {
			err = out.Sync()
		}
		if closeErr := out.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return nil, err
		}
		if hex.EncodeToString(hash.Sum(nil)) != sums[name] {
			return nil, errors.New("release file checksum mismatch")
		}
		if name == "mistervision/mistervision" || name == "mistervision/mplayer-arm" {
			if err := checkARM(dest); err != nil {
				return nil, err
			}
		}
		entries = append(entries, entry{Name: name})
	}
	return entries, nil
}

func checkARM(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	var header [52]byte
	if _, err := io.ReadFull(file, header[:]); err != nil {
		return err
	}
	if string(header[:6]) != "\x7fELF\x01\x01" || header[18] != 40 || header[19] != 0 || (header[16] != 2 && header[16] != 3) || header[17] != 0 {
		return errors.New("release does not contain ARM executables")
	}
	return nil
}
