package source

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadLines(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		content string
		want    []string
	}{
		{"", nil},
		{"a", []string{"a"}},
		{"a\nb\n", []string{"a", "b"}},
		{"a\r\nb\r\n", []string{"a", "b"}},
		{"a\n\n", []string{"a", ""}},
	}
	for _, tt := range tests {
		p := filepath.Join(dir, "f")
		write(t, p, tt.content)
		got, err := ReadLines(p)
		if err != nil || !slices.Equal(got, tt.want) {
			t.Errorf("ReadLines(%q) = %q, %v, want %q", tt.content, got, err, tt.want)
		}
	}
}

func TestLoadDirs(t *testing.T) {
	oldDir, newDir := t.TempDir(), t.TempDir()
	write(t, filepath.Join(oldDir, "mod.go"), "a\nb\n")
	write(t, filepath.Join(newDir, "mod.go"), "a\nc\n")
	write(t, filepath.Join(oldDir, "same.go"), "x\n")
	write(t, filepath.Join(newDir, "same.go"), "x\n")
	write(t, filepath.Join(oldDir, "gone.go"), "y\n")
	write(t, filepath.Join(newDir, "sub/new.go"), "z\n")
	write(t, filepath.Join(newDir, "bin.dat"), "\x00\x01")

	// git links work tree files into the new directory.
	target := filepath.Join(t.TempDir(), "linked.go")
	write(t, target, "new\n")
	write(t, filepath.Join(oldDir, "linked.go"), "old\n")
	if err := os.Symlink(target, filepath.Join(newDir, "linked.go")); err != nil {
		t.Fatal(err)
	}

	files, err := LoadDirs(oldDir, newDir)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	if want := []string{"bin.dat", "gone.go", "linked.go", "mod.go", "sub/new.go"}; !slices.Equal(paths, want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}

	byPath := func(p string) File { return files[slices.Index(paths, p)] }
	if f := byPath("bin.dat"); !f.Binary || f.InOld || !f.InNew {
		t.Errorf("bin.dat = %+v", f)
	}
	if f := byPath("gone.go"); !f.InOld || f.InNew || !slices.Equal(f.Old, []string{"y"}) {
		t.Errorf("gone.go = %+v", f)
	}
	if f := byPath("linked.go"); !slices.Equal(f.New, []string{"new"}) {
		t.Errorf("linked.go = %+v", f)
	}
	if f := byPath("sub/new.go"); f.InOld || !f.InNew {
		t.Errorf("sub/new.go = %+v", f)
	}

	// The sides on disk and the versions read, the link followed.
	f := byPath("linked.go")
	st, err := StampOf(target)
	if err != nil {
		t.Fatal(err)
	}
	if f.OldFile != filepath.Join(oldDir, "linked.go") || f.NewFile != filepath.Join(newDir, "linked.go") ||
		f.NewStamp != st || f.OldStamp.Size != 4 {
		t.Errorf("linked.go: files %q %q, stamps %+v %+v, want the link's target %+v",
			f.OldFile, f.NewFile, f.OldStamp, f.NewStamp, st)
	}
	if f := byPath("gone.go"); f.NewFile != "" || f.NewStamp != (Stamp{}) {
		t.Errorf("gone.go has a new side on disk: %q %+v", f.NewFile, f.NewStamp)
	}
}

func TestStampChanges(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	write(t, p, "a\n")
	lines, st, err := Read(p)
	if err != nil || !slices.Equal(lines, []string{"a"}) {
		t.Fatalf("Read = %q, %v", lines, err)
	}
	if again, _ := StampOf(p); again != st {
		t.Errorf("stamp changed without a write: %+v, then %+v", st, again)
	}
	write(t, p, "a\nb\n")
	if again, _ := StampOf(p); again == st {
		t.Errorf("stamp unchanged after a write: %+v", st)
	}
	if _, err := StampOf(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Errorf("StampOf of a missing file succeeded")
	}
}
