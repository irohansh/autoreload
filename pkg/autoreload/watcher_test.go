package autoreload

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIsRelevantFile(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"main.go", true},
		{"pkg.go", true},
		{"sub/pkg.go", true},
		{"go.mod", true},
		{"go.sum", true},
		{".env", true},
		{"sub/.env", true},
		{"README.md", false},
		{"image.png", false},
		{"file.json", false},
		{"doc.txt", false},
		{"Makefile", false},
	}
	for _, c := range cases {
		got := isRelevantFile(c.path, defaultExtensions)
		if got != c.want {
			t.Errorf("isRelevantFile(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestShouldIgnore(t *testing.T) {
	dir := t.TempDir()
	root, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("default_ignore", func(t *testing.T) {
		w := &watcher{root: root, extraIgnoreDirs: nil}
		cases := []struct {
			path string
			want bool
		}{
			{filepath.Join(root, ".git", "config"), true},
			{filepath.Join(root, "node_modules", "pkg", "x.js"), true},
			{filepath.Join(root, ".vscode", "settings.json"), true},
			{filepath.Join(root, ".idea", "workspace.xml"), true},
			{filepath.Join(root, "vendor", "pkg"), true},
			{filepath.Join(root, "main.go"), false},
			{filepath.Join(root, "pkg", "foo.go"), false},
			{filepath.Join(root, "file.tmp"), true},
			{filepath.Join(root, "x.swp"), true},
			{filepath.Join(root, ".#foo.go"), true},
			{filepath.Join(root, ".tmpfoo"), true},
		}
		for _, c := range cases {
			got := w.shouldIgnore(c.path)
			if got != c.want {
				t.Errorf("shouldIgnore(%q) = %v, want %v", c.path, got, c.want)
			}
		}
	})

	t.Run("extra_ignore_dirs", func(t *testing.T) {
		w := &watcher{root: root, extraIgnoreDirs: []string{"custom"}}
		path := filepath.Join(root, "custom", "file.go")
		if !w.shouldIgnore(path) {
			t.Errorf("shouldIgnore(%q) = false, want true (extra ignore)", path)
		}
	})
}

func TestShouldIgnoreEvent(t *testing.T) {
	dir := t.TempDir()
	root, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	w := &watcher{root: root, extraIgnoreDirs: nil}
	cases := []struct {
		name string
		want bool
	}{
		{filepath.Join(root, ".git", "HEAD"), true},
		{filepath.Join(root, "node_modules", "x"), true},
		{filepath.Join(root, "main.go"), false},
		{filepath.Join(root, "pkg", "foo.go"), false},
		{filepath.Join(root, "file.tmp"), true},
		{filepath.Join(root, ".#bar"), true},
		{filepath.Join(root, ".tmpfile"), true},
	}
	for _, c := range cases {
		got := w.shouldIgnoreEvent(c.name)
		if got != c.want {
			t.Errorf("shouldIgnoreEvent(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func waitForChange(t *testing.T, w *watcher, timeout time.Duration, msg string) {
	t.Helper()
	select {
	case <-w.Changes():
	case <-time.After(timeout):
		t.Fatal(msg)
	}
}

// TestDebounceBurst covers in-place writes collapsing into one debounced change (the kqueue-regressed case).
func TestDebounceBurst(t *testing.T) {
	dir := t.TempDir()
	fpath := filepath.Join(dir, "x.go")
	if err := os.WriteFile(fpath, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	logger := slog.Default()
	w, err := newWatcher(dir, logger, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	for i := 0; i < 5; i++ {
		if err := os.WriteFile(fpath, []byte("package main\n// edit\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	waitForChange(t, w, 2*time.Second, "expected one debounced change from in-place writes within 2s")
}

// TestRenameWrite covers the atomic write-temp-then-rename save pattern plus watch re-registration.
func TestRenameWrite(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main.go")
	if err := os.WriteFile(target, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	w, err := newWatcher(dir, slog.Default(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// Temp name uses an ignored suffix so only the rename onto main.go triggers.
	tmp := filepath.Join(dir, "main.go.tmp")
	if err := os.WriteFile(tmp, []byte("package main\n// v2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, target); err != nil {
		t.Fatal(err)
	}
	waitForChange(t, w, 2*time.Second, "expected a debounced change from rename-based write within 2s")

	if err := os.WriteFile(target, []byte("package main\n// v3\n"), 0644); err != nil {
		t.Fatal(err)
	}
	waitForChange(t, w, 2*time.Second, "expected a debounced change from post-rename in-place write within 2s")
}
