package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Image previews stay small so a conversation with many screenshots does not
// stall on decoding; files above this size show as plain attachments.
const previewLimit = 24 << 20

type imageEntry struct {
	bitmap  *ui.Bitmap
	loading bool
	failed  bool
}

func isImageName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return true
	}
	return false
}

// imagePreview shows a thumbnail of a local image file, decoding it once off
// the main thread. It draws nothing until the decode finishes.
func (a *app) imagePreview(c *ui.Context, path string) {
	entry := a.images[path]
	if entry == nil {
		entry = &imageEntry{loading: true}
		a.images[path] = entry
		go func() {
			data, err := os.ReadFile(path)
			var bm *ui.Bitmap
			if err == nil && len(data) <= previewLimit {
				bm, err = ui.DecodeBitmap(data)
			}
			a.win.Update(func() {
				entry.loading = false
				entry.bitmap = bm
				entry.failed = err != nil || bm == nil
			})
		}()
	}
	if entry.bitmap == nil {
		return
	}
	w, h := entry.bitmap.Size()
	const maxW, maxH = 300, 220
	scale := min(1, min(float32(maxW)/float32(w), float32(maxH)/float32(h)))
	ui.Image(c, entry.bitmap).Size(float32(w)*scale, float32(h)*scale).Fit(ui.Contain).Radius(6).Clip()
}

// imagePath picks an unused PNG name under the data directory, where pasted
// images and screenshots live so the transfer has a regular file to offer.
func (a *app) imagePath(prefix string) (string, error) {
	dir := filepath.Join(a.dataDir, "images")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	stamp := time.Now().Format("20060102-150405")
	path := filepath.Join(dir, fmt.Sprintf("%s-%s.png", prefix, stamp))
	for i := 1; ; i++ {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return path, nil
		}
		path = filepath.Join(dir, fmt.Sprintf("%s-%s-%d.png", prefix, stamp, i))
	}
}
func (a *app) savePastedImage(png []byte, prefix string) (string, error) {
	path, err := a.imagePath(prefix)
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, png, 0o600)
}

// pasteIntoConversation handles a paste that carries no text: an image goes
// out as a PNG attachment, copied files are offered as they are. It reports
// whether it consumed the paste.
func (a *app) pasteIntoConversation(peer string) bool {
	if a.busy {
		return false
	}
	if png := mygo.Clipboard.ReadImage(); len(png) > 0 {
		a.work("准备图片…", func(ctx context.Context) error {
			path, err := a.savePastedImage(png, "粘贴图片")
			if err != nil {
				return err
			}
			return a.service.Offer(ctx, peer, path)
		})
		return true
	}
	paths, err := mygo.Clipboard.ReadFiles()
	if err != nil || len(paths) == 0 {
		return false
	}
	a.work("准备文件…", func(ctx context.Context) error {
		for _, path := range paths {
			if err := a.service.Offer(ctx, peer, path); err != nil {
				return err
			}
		}
		return nil
	})
	return true
}

// captureScreenshot asks the system for a region screenshot and sends it to
// the peer. macOS runs its own interactive capture; Windows opens the Snip
// tool and waits for the result to land on the clipboard. Cancelling the
// capture sends nothing.
func (a *app) captureScreenshot(peer string) {
	a.work("正在截图，按 Esc 取消…", func(ctx context.Context) error {
		switch runtime.GOOS {
		case "darwin":
			path, err := a.imagePath("截图")
			if err != nil {
				return err
			}
			cmd := exec.CommandContext(ctx, "screencapture", "-i", "-x", path)
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("截图失败：%v。请在「系统设置 → 隐私与安全性 → 屏幕录制」中允许邻信", err)
			}
			if stat, err := os.Stat(path); err != nil || stat.Size() == 0 {
				return nil
			}
			return a.service.Offer(ctx, peer, path)
		case "windows":
			before := clipboardImageDigest()
			cmd := exec.Command("explorer.exe", "ms-screenclip:")
			configureCommand(cmd)
			if err := cmd.Start(); err != nil {
				return fmt.Errorf("无法打开 Windows 截图工具：%v", err)
			}
			deadline := time.Now().Add(90 * time.Second)
			for time.Now().Before(deadline) {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(400 * time.Millisecond):
				}
				png := mygo.Clipboard.ReadImage()
				if len(png) == 0 || bytes.Equal(digest(png), before) {
					continue
				}
				path, err := a.savePastedImage(png, "截图")
				if err != nil {
					return err
				}
				return a.service.Offer(ctx, peer, path)
			}
			return errors.New("没有等到截图，可以截好后直接在输入框粘贴")
		default:
			return errors.New("当前平台请用系统截图工具截图后粘贴到输入框")
		}
	})
}

func clipboardImageDigest() []byte {
	png := mygo.Clipboard.ReadImage()
	if len(png) == 0 {
		return nil
	}
	return digest(png)
}
func digest(b []byte) []byte { h := sha256.Sum256(b); return h[:] }
