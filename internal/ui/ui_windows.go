//go:build windows

// Package ui implements the native Windows tray and dialogs.
package ui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"syscall"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
	"github.com/tokitoki-dev/tokitoki-cli/pkg/agentlib"
	"github.com/tokitoki-dev/tokitoki-windows/internal/apikey"
	coreapp "github.com/tokitoki-dev/tokitoki-windows/internal/app"
	"github.com/tokitoki-dev/tokitoki-windows/internal/launch"
	"github.com/tokitoki-dev/tokitoki-windows/internal/version"
)

// Run starts the Windows message loop.
func Run(ctx context.Context, trayApp *coreapp.App, logger *slog.Logger) error {
	enableProcessDPIAwareness()
	// Before any window or menu exists, so everything is born themed.
	enableDarkMenus()

	mainWindow, err := walk.NewMainWindow()
	if err != nil {
		return err
	}
	mainWindow.SetTitle("Tokitoki")
	mainWindow.SetVisible(false)
	applyWindowTheme(mainWindow.Handle())

	// A tray app outlives its windows. walk's default close disposes the
	// form, which ends the message loop and with it the process, so closing
	// this one is refused outright: only Quit — or a shutdown signal — exits.
	mainWindow.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		*canceled = true
		mainWindow.SetVisible(false)
	})

	icon, err := newAppIcon()
	if err != nil {
		return err
	}
	defer icon.Dispose()
	if err := mainWindow.SetIcon(icon); err != nil {
		return err
	}

	notifyIcon, err := walk.NewNotifyIcon(mainWindow)
	if err != nil {
		return err
	}
	defer notifyIcon.Dispose()

	if err := notifyIcon.SetToolTip("Tokitoki"); err != nil {
		return err
	}

	up := newUpdater(mainWindow, notifyIcon, logger, trayApp.AutomaticUpdatesEnabled)
	if err := buildMenu(mainWindow, notifyIcon, trayApp, up, logger); err != nil {
		return err
	}
	// Left click opens Settings; the Dashboard stays a deliberate choice from
	// the right-click menu, since it leaves for the browser.
	notifyIcon.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton {
			showSettings(mainWindow, trayApp, up)
		}
	})

	go func() {
		<-ctx.Done()
		mainWindow.Synchronize(func() {
			walk.App().Exit(0)
		})
	}()

	// walk owns the shell registration; we only supply a theme-aware glyph
	// through its standard SetIcon path and keep it in sync with the taskbar
	// theme. The mark is monochrome and rendered on the fly: white on the
	// default dark taskbar, dark on a light one.
	lightTaskbar := taskbarUsesLightTheme()
	trayIcon, err := newTrayIcon(notifyIcon.DPI(), lightTaskbar)
	if err != nil {
		return err
	}
	defer func() { trayIcon.Dispose() }()
	if err := notifyIcon.SetIcon(trayIcon); err != nil {
		return err
	}
	if err := notifyIcon.SetVisible(true); err != nil {
		return err
	}
	watchTaskbarTheme(mainWindow.Handle(), func() {
		flushMenuThemes()
		light := taskbarUsesLightTheme()
		if light == lightTaskbar {
			return
		}
		next, err := newTrayIcon(notifyIcon.DPI(), light)
		if err != nil {
			logger.Warn("update tray icon", "error", err)
			return
		}
		if err := notifyIcon.SetIcon(next); err != nil {
			logger.Warn("update tray icon", "error", err)
			next.Dispose()
			return
		}
		// next is now walk's icon; the previous one is safe to release.
		trayIcon.Dispose()
		trayIcon = next
		lightTaskbar = light
	})
	showStartupUI(mainWindow, notifyIcon, trayApp, up, logger)
	go up.run(ctx)

	mainWindow.Run()
	return nil
}

func showStartupUI(owner *walk.MainWindow, notifyIcon *walk.NotifyIcon, trayApp *coreapp.App, up *updater, logger *slog.Logger) {
	if _, err := trayApp.APIKey(); errors.Is(err, agentlib.ErrMissingAPIKey) {
		_ = notifyIcon.ShowInfo("Tokitoki setup required", "Paste your API key to start syncing.")
		showSettings(owner, trayApp, up)
		return
	} else if err != nil {
		logger.Warn("check api key", "error", err)
		_ = notifyIcon.ShowWarning("Tokitoki setup check failed", truncateStatus(err.Error()))
	}
}

func buildMenu(owner *walk.MainWindow, notifyIcon *walk.NotifyIcon, trayApp *coreapp.App, up *updater, logger *slog.Logger) error {
	menu := notifyIcon.ContextMenu()

	// The tracking switch leads the menu, as on macOS: pausing the app is the
	// one action that must never be buried in a dialog.
	trackingAction := walk.NewAction()
	if err := trackingAction.SetText("Tracking enabled"); err != nil {
		return err
	}
	if err := trackingAction.SetCheckable(true); err != nil {
		return err
	}
	if err := trackingAction.SetChecked(trayApp.TrackingEnabled()); err != nil {
		return err
	}
	trackingAction.Triggered().Attach(func() {
		enabled := !trayApp.TrackingEnabled()
		if err := trayApp.SetTrackingEnabled(enabled); err != nil {
			showError(owner, "Couldn't change tracking", err)
		}
		_ = trackingAction.SetChecked(trayApp.TrackingEnabled())
	})
	if err := menu.Actions().Add(trackingAction); err != nil {
		return err
	}

	if err := addAction(menu, "Dashboard", func() { openDashboard(owner, trayApp) }); err != nil {
		return err
	}
	if err := addAction(menu, "Settings", func() { showSettings(owner, trayApp, up) }); err != nil {
		return err
	}
	if err := menu.Actions().Add(walk.NewSeparatorAction()); err != nil {
		return err
	}
	return addAction(menu, "Quit Tokitoki", func() {
		logger.Info("quit requested")
		walk.App().Exit(0)
	})
}

func addAction(menu *walk.Menu, text string, handler func()) error {
	action := walk.NewAction()
	if err := action.SetText(text); err != nil {
		return err
	}
	action.Triggered().Attach(handler)
	return menu.Actions().Add(action)
}

// settingsOpen guards against stacking dialogs. A tray click is far easier to
// repeat than a menu item, and every path here runs on the UI thread, so a
// plain flag is enough.
var settingsOpen bool

func showSettings(owner walk.Form, trayApp *coreapp.App, up *updater) {
	if settingsOpen {
		return
	}
	settingsOpen = true
	defer func() { settingsOpen = false }()

	apiKey, err := trayApp.APIKey()
	if err != nil && !errors.Is(err, agentlib.ErrMissingAPIKey) {
		showError(owner, "Couldn't load settings", err)
	}

	var dialog *walk.Dialog
	var apiKeyEdit *walk.LineEdit
	var verifyButton *walk.PushButton
	var verifyStatus *walk.Label

	family, pointSize := messageFont()
	headerFont := Font{Family: family, PointSize: pointSize, Bold: true}

	// Both switches are staged locally and written by Save, so Cancel still
	// means "change nothing" — the same contract the API key field has.
	dpi := owner.AsFormBase().DPI()
	launchToggle, err := newToggle(dpi, launch.IsEnabled(), nil)
	if err != nil {
		showError(owner, "Couldn't open settings", err)
		return
	}
	defer launchToggle.dispose()
	updatesToggle, err := newToggle(dpi, trayApp.AutomaticUpdatesEnabled(), nil)
	if err != nil {
		showError(owner, "Couldn't open settings", err)
		return
	}
	defer updatesToggle.dispose()

	children := []Widget{
		Label{
			Text: "API Key",
			Font: headerFont,
		},
		LineEdit{
			AssignTo:  &apiKeyEdit,
			Text:      apiKey,
			CueBanner: "Paste your API key",
			OnTextChanged: func() {
				// A changed key invalidates the previous answer, as on macOS.
				_ = verifyStatus.SetText("")
				verifyButton.SetEnabled(strings.TrimSpace(apiKeyEdit.Text()) != "")
			},
		},
		Composite{
			Layout: HBox{MarginsZero: true, Spacing: 8},
			Children: []Widget{
				PushButton{
					AssignTo:    &verifyButton,
					Text:        "Verify Key",
					Enabled:     strings.TrimSpace(apiKey) != "",
					ToolTipText: "Check this key with the Tokitoki server",
					OnClicked: func() {
						runKeyVerification(dialog, verifyButton, verifyStatus,
							strings.TrimSpace(apiKeyEdit.Text()))
					},
				},
				Label{AssignTo: &verifyStatus},
				HSpacer{},
			},
		},
		separatorLine(),
	}
	muted := mutedTextColor()
	children = append(children,
		settingRow(headerFont, muted, launchToggle,
			"Launch at login", "Start automatically when you sign in"),
		settingRow(headerFont, muted, updatesToggle,
			"Automatic updates", "Check for new versions in the background"),
		VSpacer{},
		separatorLine(),
		Composite{
			Layout: HBox{MarginsZero: true, Spacing: 8},
			Children: []Widget{
				Label{Text: version.Summary(), TextColor: muted},
				HSpacer{},
				PushButton{
					Text: "Cancel",
					OnClicked: func() {
						dialog.Cancel()
					},
				},
				PushButton{
					Text: "Save",
					OnClicked: func() {
						apiKey := strings.TrimSpace(apiKeyEdit.Text())
						if apiKey != "" {
							if err := trayApp.SetAPIKey(apiKey); err != nil {
								showError(dialog, "Couldn't save settings", err)
								return
							}
						}
						if err := launch.SetEnabled(launchToggle.checked()); err != nil {
							showError(dialog, "Couldn't save settings", err)
							return
						}
						if err := trayApp.SetAutomaticUpdatesEnabled(updatesToggle.checked()); err != nil {
							showError(dialog, "Couldn't save settings", err)
							return
						}
						dialog.Accept()
					},
				},
			},
		},
	)

	// Create before Run: the DWM theme attributes must land on the window
	// before it is shown, or the title bar flashes light first.
	// Without an icon of its own a dialog shows the generic Windows one: the
	// main window's icon is not inherited, and it is hidden anyway.
	// A missing icon is cosmetic only, so fall back to the default rather
	// than refusing to open Settings.
	dialogIcon, iconErr := newAppIcon()
	if iconErr == nil {
		defer dialogIcon.Dispose()
	}

	err = Dialog{
		AssignTo:  &dialog,
		Title:     "Settings",
		Icon:      dialogIcon,
		MinSize:   Size{Width: 460, Height: 340},
		FixedSize: true,
		Font:      Font{Family: family, PointSize: pointSize},
		Layout: VBox{
			Margins: Margins{Left: 20, Top: 18, Right: 20, Bottom: 18},
			Spacing: 12,
		},
		Children: children,
	}.Create(owner)
	if err != nil {
		showError(owner, "Couldn't open settings", err)
		return
	}
	applyDialogTheme(dialog.Handle())
	dialog.Run()
	// walk re-shows a dialog's owner as the dialog closes. Here the owner is
	// the tray's hidden window, which must not surface.
	owner.SetVisible(false)
}

// runKeyVerification checks the entered key with the server off the UI
// thread, then reports the answer next to the button. Three outcomes, as on
// macOS: valid, invalid, or "the server could not say" — which is not a
// verdict on the key.
func runKeyVerification(dialog *walk.Dialog, button *walk.PushButton, status *walk.Label, key string) {
	if key == "" {
		return
	}
	button.SetEnabled(false)
	_ = status.SetText("Verifying…")
	go func() {
		valid, err := apikey.NewVerifier(agentlib.BaseURL()).Verify(context.Background(), key)
		dialog.Synchronize(func() {
			if dialog.IsDisposed() {
				return
			}
			button.SetEnabled(true)
			switch {
			case err != nil:
				_ = status.SetText("⚠ Couldn't verify the key. Try again.")
			case valid:
				_ = status.SetText("✓ Key is valid.")
			default:
				_ = status.SetText("✗ Key is invalid or has been revoked.")
			}
		})
	}()
}

// openDashboard resolves the signed login link off the UI thread — the tray
// must stay responsive while the server is asked — then opens the browser
// back on it, as walk requires.
func openDashboard(owner *walk.MainWindow, trayApp *coreapp.App) {
	go func() {
		target := trayApp.DashboardTarget(context.Background())
		owner.Synchronize(func() {
			verb := syscall.StringToUTF16Ptr("open")
			if !win.ShellExecute(owner.Handle(), verb, syscall.StringToUTF16Ptr(target), nil, nil, win.SW_SHOWNORMAL) {
				showError(owner, "Couldn't open the dashboard", fmt.Errorf("open %s failed", target))
			}
		})
	}()
}

func showError(owner walk.Form, title string, err error) {
	if err == nil {
		return
	}
	taskDialogError(owner, title, err.Error())
}

func truncateStatus(message string) string {
	message = strings.TrimSpace(message)
	if len(message) <= 160 {
		return message
	}
	return message[:157] + "..."
}
