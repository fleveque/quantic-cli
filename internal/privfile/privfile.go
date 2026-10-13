// Package privfile writes files only their owner can read: the token file
// (internal/auth) and the cache (internal/cache), which holds portfolio data.
package privfile

import (
	"os"
	"path/filepath"
)

// Write replaces the file at path with data, in one step: data goes to a
// temporary file next to it, which is then renamed over it. A crash leaves
// the old file or the new one, never half of either, and a reader never
// sees a file being written. os.CreateTemp makes the file 0600 from the
// start, so it is never readable by others, not even for a moment. A missing
// directory is made 0700.
func Write(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // after a successful rename, there's nothing to remove

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	// Checked, not deferred: on some filesystems a failed write only shows
	// up here, and renaming a half-written file over a good one is what
	// this function exists to prevent.
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
