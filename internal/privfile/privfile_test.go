package privfile_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fleveque/quantic-cli/internal/privfile"
)

func TestWrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "quantic")
	path := filepath.Join(dir, "f.json")

	for _, content := range []string{"one", "two"} {
		if err := privfile.Write(path, []byte(content)); err != nil {
			t.Fatal(err)
		}
		if got, _ := os.ReadFile(path); string(got) != content {
			t.Errorf("file holds %q, want %q", got, content)
		}
	}

	info, _ := os.Stat(path)
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode %04o, want 0600", perm)
	}
	dirInfo, _ := os.Stat(dir)
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("directory mode %04o, want 0700", perm)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("directory holds %d files, want 1: temporary files left behind?", len(entries))
	}
}

// A file someone made readable is replaced by a private one.
func TestWriteReplacesAReadableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.json")
	os.WriteFile(path, []byte("old"), 0o644)
	if err := privfile.Write(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("mode %04o, want 0600", info.Mode().Perm())
	}
}
