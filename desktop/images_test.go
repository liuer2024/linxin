package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsImageName(t *testing.T) {
	for name, want := range map[string]bool{"a.png": true, "B.JPG": true, "c.webp": true, "d.pdf": false, "noext": false} {
		if got := isImageName(name); got != want {
			t.Errorf("isImageName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestImagePathAvoidsExisting(t *testing.T) {
	a := &app{dataDir: t.TempDir()}
	first, err := a.imagePath("截图")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(first) != filepath.Join(a.dataDir, "images") || filepath.Ext(first) != ".png" {
		t.Fatalf("unexpected path %q", first)
	}
	if err := os.WriteFile(first, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := a.imagePath("截图")
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Fatalf("second path %q collides with existing file", second)
	}
}
