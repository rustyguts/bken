package main

import (
	"embed"
	"os"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

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
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
