package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"

	"truedeploy/internal/store"
	"truedeploy/services"
)

// The Next.js static export (frontend/out) is embedded into the binary.
//
//go:embed all:frontend/out
var assets embed.FS

func main() {
	st, err := store.Open()
	if err != nil {
		log.Fatal(err)
	}

	app := application.New(application.Options{
		Name:        "TrueDeploy",
		Description: "Set up servers, repos and Docker deployments over SSH",
		Services: []application.Service{
			application.NewService(services.NewServerService(st)),
			application.NewService(services.NewSetupService(st)),
			application.NewService(services.NewProjectService(st)),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     "TrueDeploy",
		Width:     1200,
		Height:    780,
		MinWidth:  900,
		MinHeight: 600,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(11, 13, 16),
		URL:              "/",
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
