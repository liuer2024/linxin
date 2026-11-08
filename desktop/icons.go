package main

import "github.com/egoist/mygo/ui"

func vector(body string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">` + body + `</svg>`))
}

var icons = map[string]*ui.SVG{
	"messages": vector(`<path d="M21 11.5a8.5 8.5 0 0 1-8.5 8.5H4l-2 2v-9.5A8.5 8.5 0 0 1 10.5 4h2A8.5 8.5 0 0 1 21 11.5Z"/><path d="M7 10h9M7 14h6"/>`),
	"discover": vector(`<circle cx="12" cy="12" r="9"/><path d="m16 8-2.5 5.5L8 16l2.5-5.5Z"/>`),
	"transfer": vector(`<path d="M7 3v14m-4-4 4 4 4-4M17 21V7m-4 4 4-4 4 4"/>`),
	"settings": vector(`<circle cx="12" cy="12" r="3"/><path d="m9 3-1 3-3 1-2 3 2 2-1 3 3 2 1 3h4l1-3 3-1 2-3-2-2 1-3-3-2-1-3Z"/>`),
	"desktop":  vector(`<rect x="3" y="4" width="18" height="13" rx="2"/><path d="M8 21h8m-4-4v4"/>`),
	"laptop":   vector(`<rect x="5" y="3" width="14" height="13" rx="2"/><path d="m5 16-3 4h20l-3-4"/>`),
	"file":     vector(`<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8Z"/><path d="M14 2v6h6M8 13h8m-8 4h5"/>`),
	"send":     vector(`<path d="m22 2-7 20-4-9-9-4Zm0 0L11 13"/>`),
	"info":     vector(`<circle cx="12" cy="12" r="9"/><path d="M12 11v6m0-10v.1"/>`),
	"link":     vector(`<path d="m10 13 4-4m-6 6-2 2a3 3 0 0 1-4-4l5-5a3 3 0 0 1 4 0m2 8a3 3 0 0 0 4 0l5-5a3 3 0 0 0-4-4l-2 2"/>`),
}

func platformIcon(os string) *ui.SVG {
	if os == "darwin" || os == "macOS" {
		return icons["laptop"]
	}
	return icons["desktop"]
}
