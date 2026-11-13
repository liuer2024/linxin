package preferences

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIconPreferenceCompatibility(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"tailscale_enabled":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir)
	if err != nil || s.AppIcon != "minimal" || s.TailscaleEnabled {
		t.Fatalf("legacy settings: %+v, %v", s, err)
	}
	s.AppIcon = "flow"
	if err := Save(dir, s); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil || got != s {
		t.Fatalf("reload: %+v, %v", got, err)
	}
	s.AppIcon = "unknown"
	if err := Save(dir, s); err != nil {
		t.Fatal(err)
	}
	got, err = Load(dir)
	if err != nil || got.AppIcon != "minimal" {
		t.Fatalf("fallback: %+v, %v", got, err)
	}
}
