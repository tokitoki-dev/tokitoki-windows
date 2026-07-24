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
	"github.com/tokitoki-dev/tokitoki-windows/internal/appupdate"
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

	up := newUpdater(mainWindow, notifyIcon, logger)
	if err := buildMenu(mainWindow, notifyIcon, trayApp, up, logger); err != nil {
		return err
	}
	notifyIcon.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton {
			openDashboard(mainWindow, trayApp)
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

func showSettings(owner walk.Form, trayApp *coreapp.App, up *updater) {
	apiKey, err := trayApp.APIKey()
	if err != nil && !errors.Is(err, agentlib.ErrMissingAPIKey) {
		showError(owner, "Couldn't load settings", err)
	}

	var dialog *walk.Dialog
	var apiKeyEdit *walk.LineEdit
	var launchAtLogin *walk.CheckBox
	var verifyButton *walk.PushButton
	var verifyStatus *walk.Label

	family, pointSize := messageFont()
	headerFont := Font{Family: family, PointSize: pointSize, Bold: true}

	var checkUpdatesButton *walk.PushButton
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
	}
	children = append(children,
		Label{
			Text: "General",
			Font: headerFont,
		},
		CheckBox{
			AssignTo: &launchAtLogin,
			Text:     "Launch at login",
			Checked:  launch.IsEnabled(),
		},
		Label{
			Text: "Updates",
			Font: headerFont,
		},
		Composite{
			Layout: HBox{MarginsZero: true, Spacing: 8},
			Children: []Widget{
				Label{Text: version.Summary()},
				HSpacer{},
				PushButton{
					AssignTo: &checkUpdatesButton,
					Text:     "Check for updates",
					OnClicked: func() {
						runUpdateCheck(dialog, checkUpdatesButton, up)
					},
				},
			},
		},
		Composite{
			Layout: HBox{MarginsZero: true, Spacing: 8},
			Children: []Widget{
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
						if err := launch.SetEnabled(launchAtLogin.Checked()); err != nil {
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
	err = Dialog{
		AssignTo:  &dialog,
		Title:     "Settings",
		MinSize:   Size{Width: 460, Height: 320},
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

// runUpdateCheck asks the server for a newer build off the UI thread, then
// reports back on it. The user asked, so every outcome gets an answer —
// unlike the background check, which only speaks up for news. A "yes" on the
// offer hands over to the shared install pipeline.
func runUpdateCheck(owner *walk.Dialog, button *walk.PushButton, up *updater) {
	button.SetEnabled(false)
	go func() {
		update, err := appupdate.Check(context.Background(), agentlib.BaseURL(), version.Version)
		owner.Synchronize(func() {
			if owner.IsDisposed() {
				return
			}
			button.SetEnabled(true)
			switch {
			case errors.Is(err, appupdate.ErrDevBuild):
				taskDialogInfo(owner, "Update checks are disabled",
					"This is a development build with no release version to compare.")
			case err != nil:
				showError(owner, "The update check failed", err)
			case update == nil:
				taskDialogInfo(owner, "You're up to date",
					fmt.Sprintf("%s is the newest published build.", version.Summary()))
			default:
				up.offerInstall(owner, update)
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
