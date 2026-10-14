package ui

import (
	"fmt"
	"image/color"
	"net/url"
	"runtime/debug"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

type oledTheme struct {
	fyne.Theme
}

func (t oledTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if name == theme.ColorNameBackground {
		return color.NRGBA{A: 0xff}
	}
	return t.Theme.Color(name, variant)
}

func (d *DesktopApp) showSettings() {
	preferences := d.fyneApp.Preferences()
	profile := d.backend.Profile()

	profileAvatar := newAvatarView(64)
	profileAvatar.set(profile.AvatarURI, profile.DisplayName, d.avatars)
	displayName := profile.DisplayName
	if displayName == "" {
		displayName = "(not set)"
	}
	accountPanel := container.NewVBox(
		profileAvatar.root,
		widget.NewLabelWithStyle(displayName, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel(profile.UserID),
		widget.NewLabel("Homeserver: "+profile.Homeserver),
		widget.NewLabel("This device: "+profile.DeviceID),
		widget.NewLabel("Session data and encryption keys are stored locally on this device."),
		widget.NewButton("Log out…", d.confirmLogout),
	)

	themeSelect := widget.NewSelect([]string{"System default", "Light", "Dark", "OLED black"}, func(value string) {
		preferences.SetString("appearance.theme", value)
		d.applyTheme(value)
	})
	themeSelect.SetSelected(preferences.StringWithFallback("appearance.theme", "System default"))
	avatarCheck := widget.NewCheck("Show avatars in the message timeline", func(value bool) {
		preferences.SetBool("appearance.show_avatars", value)
		d.chat.SetShowAvatars(value)
	})
	avatarCheck.SetChecked(preferences.BoolWithFallback("appearance.show_avatars", true))
	timeSelect := widget.NewSelect([]string{"12-hour clock", "24-hour clock"}, func(value string) {
		preferences.SetBool("appearance.time_24_hour", value == "24-hour clock")
		d.chat.Set24HourTime(value == "24-hour clock")
	})
	if preferences.BoolWithFallback("appearance.time_24_hour", false) {
		timeSelect.SetSelected("24-hour clock")
	} else {
		timeSelect.SetSelected("12-hour clock")
	}
	appearancePanel := container.NewVBox(
		widget.NewLabel("Theme"),
		themeSelect,
		avatarCheck,
		widget.NewLabel("Timeline timestamps"),
		timeSelect,
	)
	d.chat.SetShowAvatars(preferences.BoolWithFallback("appearance.show_avatars", true))
	d.chat.Set24HourTime(preferences.BoolWithFallback("appearance.time_24_hour", false))

	notificationsCheck := widget.NewCheck("Enable desktop notifications", func(value bool) {
		preferences.SetBool("notifications.enabled", value)
	})
	notificationsCheck.SetChecked(preferences.BoolWithFallback("notifications.enabled", true))
	privacySelect := widget.NewSelect(
		[]string{"Sender and message preview", "Sender only", "Hidden content"},
		func(value string) { preferences.SetString("notifications.privacy", value) },
	)
	privacySelect.SetSelected(preferences.StringWithFallback("notifications.privacy", "Sender and message preview"))
	notificationPanel := container.NewVBox(
		notificationsCheck,
		widget.NewLabel("Notification content"),
		privacySelect,
		widget.NewLabel("Notifications are delivered by the operating system. Sound controls depend on its notification settings."),
	)

	sendMode := widget.NewCheck("Use Ctrl+Enter to send (Enter inserts a new line)", func(value bool) {
		preferences.SetBool("composer.ctrl_enter", value)
		d.chat.SetSendOnCtrlEnter(value)
	})
	sendMode.SetChecked(preferences.BoolWithFallback("composer.ctrl_enter", false))
	minimizeToTray := widget.NewCheck("Keep running in the system tray when the window closes", func(value bool) {
		preferences.SetBool("window.minimize_to_tray", value)
	})
	minimizeToTray.SetChecked(preferences.Bool("window.minimize_to_tray"))
	if d.trayApp == nil {
		minimizeToTray.Disable()
		minimizeToTray.SetText("Keep running in the system tray when the window closes (tray unavailable)")
	}
	d.chat.SetSendOnCtrlEnter(sendMode.Checked)
	keyboardPanel := container.NewVBox(
		sendMode,
		minimizeToTray,
		widget.NewLabel("Mark the selected chat as answered with E. Use Ctrl+Z or ⌘Z to undo the most recent action. Triage shortcuts are disabled while the message composer has focus."),
	)

	storageSummary := widget.NewLabel("Reading local storage usage…")
	storageSummary.Wrapping = fyne.TextWrapWord
	updateStorage := func() {
		dbBytes, walBytes, err := d.backend.StorageUsage()
		if err != nil {
			storageSummary.SetText("Unable to read storage usage: " + err.Error())
			return
		}
		storageSummary.SetText(fmt.Sprintf(
			"Matrix database: %s\nDatabase WAL: %s\nAvatar cache: %d in-memory entries\nMedia files are not cached on disk by this client.",
			humanBytes(dbBytes), humanBytes(walBytes), d.avatars.Count(),
		))
	}
	updateStorage()
	storagePanel := container.NewVBox(
		storageSummary,
		widget.NewButton("Clear in-memory avatar cache", func() {
			d.avatars.Clear()
			d.roomList.SetAvatarCache(d.avatars)
			d.chat.SetAvatarCache(d.avatars)
			updateStorage()
		}),
		widget.NewButton("Optimize database (VACUUM)", func() {
			storageSummary.SetText("Optimizing the local database…")
			go func() {
				err := d.backend.OptimizeDatabase(d.ctx)
				fyne.Do(func() {
					if d.ctx.Err() != nil {
						return
					}
					if err != nil {
						dialog.ShowError(err, d.window)
					}
					updateStorage()
				})
			}()
		}),
	)

	securityStatus := "Verified"
	if !profile.Verified {
		securityStatus = "This device needs verification."
	}
	securityPanel := container.NewVBox(
		widget.NewLabel("Device verification: "+securityStatus),
		widget.NewLabel("Use a recovery key or Secure Backup passphrase to restore encryption keys for this device."),
		widget.NewButton("Enter recovery key…", d.showVerification),
	)

	gomuksVersion := "unknown"
	if buildInfo, ok := debug.ReadBuildInfo(); ok {
		for _, dependency := range buildInfo.Deps {
			if dependency.Path == "go.mau.fi/gomuks" {
				gomuksVersion = dependency.Version
				break
			}
		}
	}
	repositoryURL, _ := url.Parse("https://github.com/cataWalter/Gomuks-Desktop")
	aboutPanel := container.NewVBox(
		widget.NewLabelWithStyle("Gomuks Desktop", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Version: "+Version),
		widget.NewLabel("Commit: "+Commit),
		widget.NewLabel("Build date: "+BuildDate),
		widget.NewLabel("gomuks engine: "+gomuksVersion),
		widget.NewHyperlink("Project repository", repositoryURL),
		widget.NewLabel("gomuks is licensed under AGPL-3.0-or-later."),
	)

	tabs := container.NewAppTabs(
		container.NewTabItem("Account", accountPanel),
		container.NewTabItem("Appearance", appearancePanel),
		container.NewTabItem("Notifications", notificationPanel),
		container.NewTabItem("Keyboard", keyboardPanel),
		container.NewTabItem("Storage", storagePanel),
		container.NewTabItem("Security", securityPanel),
		container.NewTabItem("About", aboutPanel),
	)
	tabs.SetTabLocation(container.TabLocationLeading)
	settings := dialog.NewCustom("Settings", "Close", tabs, d.window)
	settings.Resize(fyne.NewSize(720, 540))
	settings.Show()
}

func (d *DesktopApp) applyTheme(choice string) {
	switch choice {
	case "Light":
		d.fyneApp.Settings().SetTheme(theme.LightTheme())
		d.themeDark = false
	case "Dark":
		d.fyneApp.Settings().SetTheme(theme.DarkTheme())
		d.themeDark = true
	case "OLED black":
		d.fyneApp.Settings().SetTheme(oledTheme{Theme: theme.DarkTheme()})
		d.themeDark = true
	default:
		d.fyneApp.Settings().SetTheme(theme.DefaultTheme())
		d.themeDark = false
	}
}

func humanBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for value := bytes / unit; value >= unit; value /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
