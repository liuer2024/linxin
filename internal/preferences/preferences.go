// Package preferences persists user-controlled desktop features.
package preferences

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type Settings struct {
	TailscaleEnabled bool `json:"tailscale_enabled"`
	StartTailscale   bool `json:"start_tailscale_with_app"`
}

func Default() Settings { return Settings{TailscaleEnabled: true} }
func Load(dir string) (Settings, error) {
	settings := Default()
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if errors.Is(err, os.ErrNotExist) {
		return settings, nil
	}
	if err != nil {
		return settings, err
	}
	// Decode over defaults so newly added fields retain their default value.
	if err = json.Unmarshal(b, &settings); err != nil {
		return Default(), err
	}
	return settings, nil
}
func Save(dir string, settings Settings) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".settings-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, "settings.json"))
}
