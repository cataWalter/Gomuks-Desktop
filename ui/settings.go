package ui

import (
	"fmt"
	"image/color"
	"net/url"
	"runtime/debug"
	"strings"
	"time"

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

type appTheme struct {
	fyne.Theme
	textScale float32
	oledBlack bool
}

func (t appTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if t.oledBlack && name == theme.ColorNameBackground {
		return color.NRGBA{A: 0xff}
	}
	return t.Theme.Color(name, variant)
}

func (t appTheme) Size(name fyne.ThemeSizeName) float32 {
	size := t.Theme.Size(name)
	if name == theme.SizeNameText {
		return size * t.textScale
	}
	return size
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
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Active devices", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	)
	devicesPanel := container.NewVBox(widget.NewLabel("Loading devices…"))
	accountPanel.Add(devicesPanel)
	accountPanel.Add(
		widget.NewButton("Log out…", d.confirmLogout),
	)
	var refreshDevices func()
	refreshDevices = func() {
		devicesPanel.Objects = []fyne.CanvasObject{widget.NewLabel("Loading devices…")}
		devicesPanel.Refresh()
		go func() {
			devices, err := d.backend.Devices(d.ctx)
			fyne.Do(func() {
				if d.ctx.Err() != nil {
					return
				}
				if err != nil {
					devicesPanel.Objects = []fyne.CanvasObject{widget.NewLabel("Unable to load devices: " + err.Error())}
					devicesPanel.Refresh()
					return
				}
				rows := make([]fyne.CanvasObject, 0, len(devices)+1)
				for _, device := range devices {
					device := device
					name := device.DisplayName
					if name == "" {
						name = "(unnamed device)"
					}
					details := fmt.Sprintf("%s · %s", name, device.ID)
					if device.Current {
						details += " · this device"
					}
					if !device.LastSeenAt.IsZero() {
						details += " · last active " + device.LastSeenAt.Local().Format(time.DateTime)
					}
					row := container.NewBorder(nil, nil, nil, nil, widget.NewLabel(details))
					if !device.Current {
						revoke := widget.NewButton("Revoke", func() {
							dialog.ShowConfirm("Revoke device", "Sign out "+name+" ("+device.ID+")?", func(ok bool) {
								if !ok {
									return
								}
								go func() {
									revokeErr := d.backend.RevokeDevice(d.ctx, device.ID)
									fyne.Do(func() {
										if d.ctx.Err() != nil {
											return
										}
										if revokeErr != nil {
											dialog.ShowError(revokeErr, d.window)
											return
										}
										refreshDevices()
									})
								}()
							}, d.window)
						})
						row = container.NewBorder(nil, nil, nil, revoke, widget.NewLabel(details))
					}
					rows = append(rows, row)
				}
				if len(rows) == 0 {
					rows = append(rows, widget.NewLabel("No devices were returned by the homeserver."))
				}
				devicesPanel.Objects = rows
				devicesPanel.Refresh()
			})
		}()
	}
	refreshDevices()

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
	bubbleColorPicker := widget.NewColorEntry()
	bubbleColorPicker.SetMinLength(3)
	bubbleColorPicker.SetMaxLength(7)
	bubbleColorPicker.OnChanged = func(value string) {
		color, err := color.Parse(value)
		if err != nil || d.chat == nil {
			return
		}
		preferences.SetString("appearance.bubble_color", value)
		d.chat.SetBubbleColor(color)
	}
	bubbleColorPicker.SetColor(d.fyneApp.Preferences().ColorWithFallback("appearance.bubble_color", theme.Color(theme.ColorNameSecondary)))
	timeSelect := widget.NewSelect([]string{"12-hour clock", "24-hour clock"}, func(value string) {
		preferences.SetBool("appearance.time_24_hour", value == "24-hour clock")
		d.chat.Set24HourTime(value == "24-hour clock")
	})
	if preferences.BoolWithFallback("appearance.time_24_hour", false) {
		timeSelect.SetSelected("24-hour clock")
	} else {
		timeSelect.SetSelected("12-hour clock")
	}
	relativeSelect := widget.NewSelect([]string{"Absolute dates", "Relative dates"}, func(value string) {
		relative := value == "Relative dates"
		preferences.SetBool("appearance.relative_timestamps", relative)
		d.chat.SetRelativeTimestamps(relative)
	})
	if preferences.Bool("appearance.relative_timestamps") {
		relativeSelect.SetSelected("Relative dates")
	} else {
		relativeSelect.SetSelected("Absolute dates")
	}
	fontSizeSelect := widget.NewSelect([]string{"Small", "Medium", "Large"}, func(value string) {
		preferences.SetString("appearance.font_size", value)
		d.applyTheme(preferences.StringWithFallback("appearance.theme", "System default"))
	})
	fontSizeSelect.SetSelected(preferences.StringWithFallback("appearance.font_size", "Medium"))
	appearancePanel := container.NewVBox(
		widget.NewLabel("Theme"),
		themeSelect,
		widget.NewLabel("Font size"),
		fontSizeSelect,
		avatarCheck,
		widget.NewLabel("Bubble color"),
		bubbleColorPicker,
		widget.NewCheck("Compact message layout (IRC style)", func(value bool) {
			preferences.SetBool("appearance.compact", value)
			d.applyCompact(value)
		}),
		widget.NewLabel("Timeline timestamps"),
		timeSelect,
		widget.NewLabel("Date format"),
		relativeSelect,
	)
	d.chat.SetShowAvatars(preferences.BoolWithFallback("appearance.show_avatars", true))
	d.applyCompact(preferences.Bool("appearance.compact"))
	d.chat.Set24HourTime(preferences.BoolWithFallback("appearance.time_24_hour", false))
	d.chat.SetRelativeTimestamps(preferences.Bool("appearance.relative_timestamps"))

	notificationsCheck := widget.NewCheck("Enable desktop notifications", func(value bool) {
		preferences.SetBool("notifications.enabled", value)
	})
	notificationsCheck.SetChecked(preferences.BoolWithFallback("notifications.enabled", true))
	unfocusedOnly := widget.NewCheck("Only notify when the window is unfocused", func(value bool) {
		preferences.SetBool("notifications.only_when_unfocused", value)
	})
	unfocusedOnly.SetChecked(preferences.BoolWithFallback("notifications.only_when_unfocused", true))
	soundCheck := widget.NewCheck("Play a sound with notifications", func(value bool) {
		preferences.SetBool("notifications.sound", value)
	})
	soundCheck.SetChecked(preferences.Bool("notifications.sound"))
	privacySelect := widget.NewSelect(
		[]string{"Sender and message preview", "Sender only", "Hidden content"},
		func(value string) { preferences.SetString("notifications.privacy", value) },
	)
	privacySelect.SetSelected(preferences.StringWithFallback("notifications.privacy", "Sender and message preview"))
	notificationPanel := container.NewVBox(
		notificationsCheck,
		unfocusedOnly,
		soundCheck,
		widget.NewLabel("Notification content"),
		privacySelect,
		widget.NewLabel("System notifications cannot be clicked through to a room in this Fyne version; use the tray or Quick Switcher to jump to a chat."),
	)

	sendMode := widget.NewCheck("Use Ctrl+Enter to send (Enter inserts a new line)", func(value bool) {
		preferences.SetBool("composer.ctrl_enter", value)
		d.chat.SetSendOnCtrlEnter(value)
	})
	sendMode.SetChecked(preferences.BoolWithFallback("composer.ctrl_enter", false))
	answeredShortcut := widget.NewEntry()
	answeredShortcut.SetPlaceHolder("E")
	answeredShortcut.SetText(preferences.StringWithFallback("shortcuts.mark_answered", "E"))
	answeredShortcut.OnChanged = func(value string) {
		value = strings.ToUpper(strings.TrimSpace(value))
		if value == "" {
			value = "E"
		}
		runes := []rune(value)
		value = string(runes[:1])
		if answeredShortcut.Text != value {
			answeredShortcut.SetText(value)
		}
		preferences.SetString("shortcuts.mark_answered", value)
	}
	minimizeToTray := widget.NewCheck("Keep running in the system tray when the window closes", func(value bool) {
		preferences.SetBool("window.minimize_to_tray", value)
	})
	minimizeToTray.SetChecked(preferences.Bool("window.minimize_to_tray"))
	if d.trayApp == nil {
		minimizeToTray.Disable()
		minimizeToTray.SetText("Keep running in the system tray when the window closes (tray unavailable)")
	}
	var launchAtStartup *widget.Check
	launchAtStartup = widget.NewCheck("Launch on system startup", func(value bool) {
		if err := setLaunchAtStartup(value); err != nil {
			dialog.ShowError(err, d.window)
			launchAtStartup.SetChecked(!value)
			return
		}
		preferences.SetBool("system.launch_at_startup", value)
	})
	launchAtStartup.SetChecked(preferences.Bool("system.launch_at_startup"))
	d.chat.SetSendOnCtrlEnter(sendMode.Checked)
	keyboardPanel := container.NewVBox(
		sendMode,
		widget.NewLabel("Mark answered shortcut (single letter)"),
		answeredShortcut,
		minimizeToTray,
		launchAtStartup,
		widget.NewLabel("Mark the selected chat as answered with the shortcut above. Use Ctrl+Z or ⌘Z to undo the most recent action. Triage shortcuts are disabled while the message composer has focus."),
	)

	storageSummary := widget.NewLabel("Reading local storage usage…")
	storageSummary.Wrapping = fyne.TextWrapWord
	updateStorage := func() {
		dbBytes, walBytes, err := d.backend.StorageUsage()
		if err != nil {
			storageSummary.SetText("Unable to read storage usage: " + err.Error())
			return
		}
		mediaBytes, mediaFiles, mediaErr := d.backend.MediaCacheUsage()
		summary := fmt.Sprintf(
			"Matrix database: %s\nDatabase WAL: %s\nAvatar cache: %d in-memory entries",
			humanBytes(dbBytes), humanBytes(walBytes), d.avatars.Count(),
		)
		if mediaErr != nil {
			summary += "\nMedia cache: unavailable (" + mediaErr.Error() + ")"
		} else {
			summary += fmt.Sprintf("\nMedia cache: %s in %d files\nCache folder: %s", humanBytes(mediaBytes), mediaFiles, d.backend.MediaCacheDir())
		}
		storageSummary.SetText(summary)
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
		widget.NewButton("Clear media cache", func() {
			if err := d.backend.ClearMediaCache(); err != nil {
				dialog.ShowError(err, d.window)
				return
			}
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
		widget.NewButton("Start emoji verification…", d.showSASVerification),
		widget.NewSeparator(),
		widget.NewLabel("Room key backups are encrypted with a passphrase you choose. Keep that passphrase separate from the backup file."),
		widget.NewButton("Export room keys…", d.exportRoomKeys),
		widget.NewButton("Import room keys…", d.importRoomKeys),
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
	issuesURL, _ := url.Parse("https://github.com/cataWalter/Gomuks-Desktop/issues")
	licensesURL, _ := url.Parse("https://www.gnu.org/licenses/agpl-3.0.html")
	aboutPanel := container.NewVBox(
		widget.NewLabelWithStyle("Gomuks Desktop", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Version: "+Version),
		widget.NewLabel("Commit: "+Commit),
		widget.NewLabel("Build date: "+BuildDate),
		widget.NewLabel("gomuks engine: "+gomuksVersion),
		widget.NewHyperlink("Project repository", repositoryURL),
		widget.NewHyperlink("Report an issue", issuesURL),
		widget.NewHyperlink("AGPL-3.0-or-later license", licensesURL),
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

func (d *DesktopApp) applyCompact(compact bool) {
	if d.chat != nil {
		d.chat.SetCompact(compact)
	}
}

func (d *DesktopApp) applyTheme(choice string) {
	base := theme.DefaultTheme()
	oledBlack := false
	switch choice {
	case "Light":
		base = theme.LightTheme()
		d.themeDark = false
	case "Dark":
		base = theme.DarkTheme()
		d.themeDark = true
	case "OLED black":
		base = theme.DarkTheme()
		oledBlack = true
		d.themeDark = true
	default:
		d.themeDark = false
	}
	textScale := float32(1)
	switch d.fyneApp.Preferences().StringWithFallback("appearance.font_size", "Medium") {
	case "Small":
		textScale = 0.9
	case "Large":
		textScale = 1.15
	}
	d.fyneApp.Settings().SetTheme(appTheme{Theme: base, textScale: textScale, oledBlack: oledBlack})
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
