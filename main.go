package main

import (
	"embed"
	"fmt"
	"log"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"truedeploy/internal/alerts"
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

	gh := services.NewGitHubService(st)
	projects := services.NewProjectService(st, gh)
	dbs := services.NewDatabaseService(st)
	notifier := notifications.New()
	mon := services.NewMonitorService(st, projects, notifier)
	projects.OnDeployFinished = mon.NotifyDeploy

	app := application.New(application.Options{
		Name:        "TrueDeploy",
		Description: "Set up servers, repos and Docker deployments over SSH",
		Services: []application.Service{
			application.NewService(services.NewServerService(st)),
			application.NewService(services.NewSetupService(st)),
			application.NewService(projects),
			application.NewService(gh),
			application.NewService(services.NewGitService(st, gh)),
			application.NewService(dbs),
			application.NewService(services.NewImportService(st, projects, dbs)),
			application.NewService(services.NewOffsiteService(st, projects)),
			application.NewService(services.NewTerminalService(st)),
			application.NewService(notifier),
			application.NewService(mon),
			application.NewService(services.NewAppService(st, notifier)),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			// With close-to-tray the app keeps running without windows.
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
	})

	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
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
		BackgroundColour: application.NewRGB(7, 11, 20),
		URL:              "/overview/",
	})

	showWindow := func() {
		window.Show()
		window.UnMinimise()
		window.Focus()
	}

	// System tray: keeps TrueDeploy (and monitoring) running when the window is closed.
	var quitting atomic.Bool
	tray := app.SystemTray.New()
	if icon, err := assets.ReadFile("frontend/out/logo.png"); err == nil {
		tray.SetIcon(icon)
	}
	tray.SetTooltip("TrueDeploy")
	menu := application.NewMenu()
	menu.Add("Open TrueDeploy").OnClick(func(*application.Context) { showWindow() })
	status := menu.Add("No alerts").SetEnabled(false)
	menu.Add("Check servers now").OnClick(func(*application.Context) { mon.CheckNow() })
	menu.AddSeparator()
	menu.Add("Quit TrueDeploy").OnClick(func(*application.Context) {
		quitting.Store(true)
		app.Quit()
	})
	tray.SetMenu(menu)
	tray.OnClick(showWindow)

	mon.OnChange = func(active []alerts.Alert) {
		label, tip := "No alerts", "TrueDeploy: all good"
		if n := len(active); n > 0 {
			label = fmt.Sprintf("%s: %s", alertCount(n), active[0].Title)
			tip = "TrueDeploy: " + alertCount(n)
		}
		status.SetLabel(label)
		menu.Update()
		tray.SetTooltip(tip)
	}

	// Closing the window hides it to the tray (if enabled) instead of quitting.
	var toldAboutTray atomic.Bool
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		set, _ := st.Settings()
		if quitting.Load() || !set.CloseToTray {
			return
		}
		e.Cancel()
		window.Hide()
		if !toldAboutTray.Swap(true) && set.Notify {
			_ = notifier.SendNotification(notifications.NotificationOptions{
				ID:    "td-tray",
				Title: "TrueDeploy is still running",
				Body:  "It keeps watching your servers from the system tray. Right-click the tray icon to quit.",
			})
		}
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

func alertCount(n int) string {
	if n == 1 {
		return "1 alert"
	}
	return fmt.Sprintf("%d alerts", n)
}
