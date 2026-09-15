package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	updateapi "misterfin-crt/internal/update"
)

// entry records whether rollback restores an original or removes a new file.
// Backup and staged filenames are numeric, never supplied by archive paths.
type entry struct {
	Name         string
	HadOriginal  bool
	OriginalMode uint32
}

func (i *Installer) lock() (func(), error) {
	if !filepath.IsAbs(i.root) || !filepath.IsAbs(i.launcher) {
		return nil, errors.New("update destinations must be absolute")
	}
	actual, err := filepath.EvalSymlinks(i.root)
	if err != nil || actual != i.root {
		return nil, errors.New("update directory is missing or uses symlinks")
	}
	fd, err := syscall.Open(filepath.Join(i.root, ".update.lock"), syscall.O_RDWR|syscall.O_CREAT|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "update lock")
	if info, err := file.Stat(); err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, errors.New("update lock is not a regular file")
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, errors.New("another update is active")
	}
	return func() { _ = syscall.Flock(fd, syscall.LOCK_UN); file.Close() }, nil
}

func (i *Installer) backup(ctx context.Context, stage string, entries []entry) error {
	var size int64
	for index := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		dest := i.destination(entries[index].Name)
		if err := safeParent(filepath.Dir(dest)); err != nil {
			return err
		}
		info, err := os.Lstat(dest)
		if errors.Is(err, os.ErrNotExist) {
			if entries[index].Name == "misterfin-crt/misterfin-crt" || entries[index].Name == "misterfin-crt/mplayer-arm" {
				return errors.New("installed player pair is incomplete")
			}
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("installed update target is not a regular file")
		}
		size += info.Size()
		if info.Size() > 64<<20 || size > maxExpanded {
			return errors.New("installed files exceed rollback limits")
		}
		if err := copyFile(filepath.Join(stage, fmt.Sprintf("old-%d", index)), dest, 0600); err != nil {
			return err
		}
		entries[index].HadOriginal = true
		entries[index].OriginalMode = uint32(info.Mode().Perm())
	}
	return nil
}

func writeJournal(stage string, entries []entry) error {
	data, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	if err := durableWrite(filepath.Join(stage, "journal.json"), data, 0600); err != nil {
		return err
	}
	return syncDir(stage)
}

func (i *Installer) recover() (bool, error) {
	pending := filepath.Join(i.root, pendingName)
	info, err := os.Lstat(pending)
	if errors.Is(err, os.ErrNotExist) {
		i.removeStages()
		return false, nil
	}
	if err != nil {
		return false, errors.Join(updateapi.ErrRecovery, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, updateapi.ErrRecovery
	}
	committed, err := readRecord(filepath.Join(pending, "committed"), 16)
	if err == nil && string(committed) == "1\n" {
		_ = os.RemoveAll(pending)
		i.removeStages()
		return false, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, errors.Join(updateapi.ErrRecovery, err)
	}
	data, err := readRecord(filepath.Join(pending, "journal.json"), 64<<10)
	var entries []entry
	if err != nil || len(data) > 64<<10 || json.Unmarshal(data, &entries) != nil || len(entries) == 0 || len(entries) > maxFiles {
		return false, updateapi.ErrRecovery
	}
	seen := make(map[string]bool)
	// Validate every entry and backup before changing any destination.
	for index, entry := range entries {
		if !allowed(entry.Name) || entry.Name == "SHA256SUMS" || seen[entry.Name] || entry.OriginalMode > 0777 {
			return false, updateapi.ErrRecovery
		}
		seen[entry.Name] = true
		if entry.HadOriginal {
			info, err := os.Lstat(filepath.Join(pending, fmt.Sprintf("old-%d", index)))
			if err != nil || !info.Mode().IsRegular() || info.Size() > 64<<20 {
				return false, updateapi.ErrRecovery
			}
		}
	}
	for index, entry := range entries {
		dest := i.destination(entry.Name)
		if entry.HadOriginal {
			err = replaceFile(dest, filepath.Join(pending, fmt.Sprintf("old-%d", index)), os.FileMode(entry.OriginalMode))
		} else {
			err = safeParent(filepath.Dir(dest))
			if err == nil {
				err = os.Remove(dest)
				if errors.Is(err, os.ErrNotExist) {
					err = nil
				}
				if err == nil {
					err = syncDir(filepath.Dir(dest))
				}
			}
		}
		if err != nil {
			return false, errors.Join(updateapi.ErrRecovery, err)
		}
	}
	if err := os.RemoveAll(pending); err != nil {
		return true, errors.Join(updateapi.ErrRecovery, err)
	}
	if err := syncDir(i.root); err != nil {
		return true, errors.Join(updateapi.ErrRecovery, err)
	}
	i.removeStages()
	return true, nil
}

// removeStages discards downloads left by a process that stopped before the
// journal was published. No installed file has changed at that stage.
func (i *Installer) removeStages() {
	entries, _ := os.ReadDir(i.root)
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), ".update-stage-") {
			_ = os.RemoveAll(filepath.Join(i.root, entry.Name()))
		}
	}
}

// safeParent rejects symlinked directory components before creating missing
// notice directories. Archive paths have already passed the fixed allowlist.
func safeParent(dir string) error {
	parent := filepath.Dir(dir)
	if parent != dir {
		if err := safeParent(parent); err != nil {
			return err
		}
	}
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return os.Mkdir(dir, 0755)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("update destination uses a non-directory or symlink")
	}
	return nil
}

func syncDir(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}

func durableWrite(path string, data []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, err = file.Write(data)
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}

func copyFile(dest, source string, mode os.FileMode) error {
	src, err := os.Open(source)
	if err != nil {
		return err
	}
	defer src.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	size, err := io.Copy(out, io.LimitReader(src, (64<<20)+1))
	if err == nil && size > 64<<20 {
		err = errors.New("update file exceeds size limit")
	}
	if err == nil {
		err = out.Sync()
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	return err
}

// replaceFile keeps the destination present throughout replacement. Backups
// remain intact until all files and the commit marker have been synchronized.
func replaceFile(dest, source string, mode os.FileMode) error {
	if err := safeParent(filepath.Dir(dest)); err != nil {
		return err
	}
	if info, err := os.Lstat(dest); err == nil && !info.Mode().IsRegular() {
		return errors.New("update target is not a regular file")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(dest), ".update-file-")
	if err != nil {
		return err
	}
	name := temp.Name()
	temp.Close()
	defer os.Remove(name)
	// Remove the reserved empty file so copyFile can require exclusive creation.
	if err := os.Remove(name); err != nil {
		return err
	}
	if err := copyFile(name, source, mode); err != nil {
		return err
	}
	if err := os.Rename(name, dest); err != nil {
		return err
	}
	return syncDir(filepath.Dir(dest))
}

// readRecord bounds recovery metadata and refuses non-regular files.
func readRecord(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("invalid update recovery record")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err == nil && int64(len(data)) > limit {
		err = errors.New("update recovery record is too large")
	}
	return data, err
}
