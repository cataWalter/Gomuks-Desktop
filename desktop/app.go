package desktop

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"path/filepath"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"mychat/matrix"
	"mychat/notifications"
)

const (
	updateEventName            = "matrix:update"
	hostErrorEventName         = "matrix:host-error"
	attachmentSentEventName    = "matrix:attachment-sent"
	scheduledReminderEventName = "scheduler:reminder"
	notificationOpenRoomName   = "notification:open-room"
)

func Run(client *matrix.Client, assets fs.FS, preferencesPath string) error {
	frontend, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		return fmt.Errorf("open embedded frontend assets: %w", err)
	}
	preferences, err := NewPreferenceStore(preferencesPath)
	if err != nil {
		return fmt.Errorf("open desktop preferences: %w", err)
	}
	scheduledJobs, err := newScheduledJobStore(filepath.Join(filepath.Dir(preferencesPath), "scheduled-jobs.json"))
	if err != nil {
		return fmt.Errorf("open scheduled jobs: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	notifier := notifications.New()
	service := &AppService{client: client, preferences: preferences, notifier: notifier, ctx: ctx, cancel: cancel, scheduled: scheduledJobs}
	var shutdownOnce sync.Once
	shutdown := func() {
		shutdownOnce.Do(func() {
			service.close()
			client.Close()
			if lifecycle, ok := notifier.(notifications.Lifecycle); ok {
				if closeErr := lifecycle.Close(); closeErr != nil {
					log.Printf("stop native notifications: %v", closeErr)
				}
			}
		})
	}

	app := application.New(application.Options{
		Name:        "Pinger",
		Description: "A Matrix desktop client",
		Services: []application.Service{
			application.NewService(service),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(frontend),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		OnShutdown: shutdown,
	})
	if lifecycle, ok := notifier.(notifications.Lifecycle); ok {
		if startErr := lifecycle.Start(ctx); startErr != nil {
			log.Printf("native notifications unavailable: %v", startErr)
		}
	}
	service.quit = app.Quit
	service.emitReminder = func(reminder ScheduledJob) {
		app.Event.Emit(scheduledReminderEventName, reminder)
	}
	service.setAutostart = func(enabled bool) error {
		if enabled {
			return app.Autostart.Enable()
		}
		return app.Autostart.Disable()
	}
	launchAtStartup, _ := preferences.All()["system.launch_at_startup"].(bool)
	if err = service.setAutostart(launchAtStartup); err != nil {
		log.Printf("apply launch at startup preference: %v", err)
	}

	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:               "main",
		Title:              "Pinger",
		Width:              1100,
		Height:             720,
		MinWidth:           760,
		MinHeight:          520,
		URL:                "/",
		BackgroundColour:   application.NewRGB(238, 243, 239),
		EnableFileDrop:     true,
		UseApplicationMenu: true,
	})

	normalIcon, unreadIcon := trayIcons()
	tray := app.SystemTray.New()
	tray.SetIcon(normalIcon)
	tray.SetTooltip("Pinger")
	tray.AttachWindow(window)
	if handler, ok := notifier.(notifications.ClickHandler); ok {
		handler.SetClickHandler(func(roomID string) {
			tray.ShowWindow()
			app.Event.Emit(notificationOpenRoomName, roomID)
		})
	}
	menu := application.NewMenu()
	menu.Add("Open").OnClick(func(_ *application.Context) {
		tray.ShowWindow()
	})
	menu.Add("Mark All as Read").OnClick(func(_ *application.Context) {
		go func() {
			if markErr := service.markAllRead(); markErr != nil && service.ctx.Err() == nil {
				log.Printf("mark all rooms as read: %v", markErr)
				app.Event.Emit(hostErrorEventName, map[string]string{"error": markErr.Error()})
			}
		}()
	})
	menu.AddSeparator()
	menu.Add("Quit").OnClick(func(_ *application.Context) {
		app.Quit()
	})
	tray.SetMenu(menu)

	emitHostError := func(hostErr error) {
		if hostErr != nil {
			app.Event.Emit(hostErrorEventName, map[string]string{"error": hostErr.Error()})
		}
	}
	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if minimizeToTray, _ := preferences.All()["window.minimize_to_tray"].(bool); minimizeToTray {
			event.Cancel()
			window.Hide()
		}
	})
	window.OnWindowEvent(events.Common.WindowFilesDropped, func(event *application.WindowEvent) {
		if event.Context() == nil {
			return
		}
		paths := append([]string(nil), event.Context().DroppedFiles()...)
		if len(paths) == 0 {
			return
		}
		go func() {
			roomID, sent, dropErr := service.sendDroppedFiles(paths)
			if sent {
				app.Event.Emit(attachmentSentEventName, roomID)
			}
			emitHostError(dropErr)
		}()
	})

	lastUnread := -1
	service.startEventPump(func(update Update) {
		unread := 0
		for _, room := range update.Rooms {
			unread += room.Unread
		}
		if unread != lastUnread {
			if unread > 0 {
				tray.SetIcon(unreadIcon)
				tray.SetTooltip(fmt.Sprintf("Pinger (%d unread)", unread))
			} else {
				tray.SetIcon(normalIcon)
				tray.SetTooltip("Pinger")
			}
			lastUnread = unread
		}
		app.Event.Emit(updateEventName, update)
	})
	service.startScheduledJobWorker()

	if client.IsLoggedIn() {
		service.startSync()
	}

	err = app.Run()
	shutdown()
	return err
}
