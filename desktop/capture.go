package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Optional local QA capture. CapturePage renders only this native window;
// it never records the desktop or other applications.
func (a *app) captureSettings(path string) {
	original := a.settings
	go func() {
		time.Sleep(time.Second)
		a.win.Update(func() { a.openSettings() })
		time.Sleep(time.Second)
		png, err := a.win.CapturePage()
		if err == nil {
			err = os.WriteFile(path, png, 0600)
		}
		if err != nil {
			log.Printf("capture settings: %v", err)
		}
		a.win.Update(func() { a.settingsDraft.AppIcon = "flow"; a.saveSettings() })
		time.Sleep(time.Second)
		a.win.Update(func() { a.openSettings() })
		time.Sleep(time.Second)
		png, err = a.win.CapturePage()
		if err == nil {
			err = os.WriteFile(strings.TrimSuffix(path, filepath.Ext(path))+"-flow.png", png, 0600)
		}
		if err != nil {
			log.Printf("capture flow settings: %v", err)
		}
		a.win.Update(func() { a.settingsDraft = original; a.saveSettings() })
	}()
}
