package main

import (
	"embed"
	"os"
	"runtime"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

func setDefaultEnv(key, value string) {
	if os.Getenv(key) == "" {
		_ = os.Setenv(key, value)
	}
}

func configureLinuxDesktopEnv() {
	if runtime.GOOS != "linux" {
		return
	}
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		return
	}

	// WebKitGTK can hit compositor/protocol errors on some Wayland stacks.
	setDefaultEnv("WEBKIT_DISABLE_COMPOSITING_MODE", "1")
	setDefaultEnv("WEBKIT_DISABLE_DMABUF_RENDERER", "1")
	if os.Getenv("DISPLAY") != "" {
		setDefaultEnv("GDK_BACKEND", "x11")
	}
}

//go:embed all:frontend/dist
var assets embed.FS

// parseStartupAddr scans args for a bken:// URL and returns the host:port.
// Returns "" if no bken:// argument is found or if the addr portion is empty.
func parseStartupAddr(args []string) string {
	const scheme = "bken://"
	for _, arg := range args {
		if strings.HasPrefix(arg, scheme) {
			addr := strings.TrimPrefix(arg, scheme)
			addr = strings.TrimRight(addr, "/")
			return addr
		}
	}
	return ""
}

func main() {
	configureLinuxDesktopEnv()

	app := NewApp()
	app.startupAddr = parseStartupAddr(os.Args[1:])

	err := wails.Run(&options.App{
		Title:     "bken",
		Width:     800,
		Height:    600,
		MinWidth:  400,
		MinHeight: 300,
		Frameless: true,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true,
			DisableWebViewDrop: true,
			CSSDropProperty:    "--wails-drop-target",
			CSSDropValue:       "drop",
		},
		Linux: &linux.Options{
			ProgramName: "bken",
		},
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
