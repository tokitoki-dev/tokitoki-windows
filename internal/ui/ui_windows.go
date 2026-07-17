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

	"github.com/tokitoki-dev/tokitoki-cli/pkg/agentlib"
	coreapp "github.com/tokitoki-dev/tokitoki-windows/internal/app"
	"github.com/tokitoki-dev/tokitoki-windows/internal/appupdate"
	"github.com/tokitoki-dev/tokitoki-windows/internal/launch"
	"github.com/tokitoki-dev/tokitoki-windows/internal/version"
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
)

// Run starts the Windows message loop.
func Run(ctx context.Context, trayApp *coreapp.App, logger *slog.Logger) error {
	enableProcessDPIAwareness()

	mainWindow, err := walk.NewMainWindow()
	if err != nil {
		return err
	}
	mainWindow.SetTitle("TokiToki")
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

	if err := notifyIcon.SetIcon(icon); err != nil {
		return err
	}
	if err := notifyIcon.SetToolTip("TokiToki"); err != nil {
		return err
	}

	if err := buildMenu(mainWindow, notifyIcon, trayApp, logger); err != nil {
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

	if err := notifyIcon.SetVisible(true); err != nil {
		return err
	}
	showStartupUI(mainWindow, notifyIcon, trayApp, logger)

	mainWindow.Run()
	return nil
}

func showStartupUI(owner *walk.MainWindow, notifyIcon *walk.NotifyIcon, trayApp *coreapp.App, logger *slog.Logger) {
	if _, err := trayApp.APIKey(); errors.Is(err, agentlib.ErrMissingAPIKey) {
		_ = notifyIcon.ShowInfo("TokiToki setup required", "Paste your API key to start syncing.")
		showSettings(owner, trayApp)
		return
	} else if err != nil {
		logger.Warn("check api key", "error", err)
		_ = notifyIcon.ShowWarning("TokiToki setup check failed", truncateStatus(err.Error()))
	}
}

func buildMenu(owner *walk.MainWindow, notifyIcon *walk.NotifyIcon, trayApp *coreapp.App, logger *slog.Logger) error {
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
			showError(owner, "Tracking", err)
		}
		_ = trackingAction.SetChecked(trayApp.TrackingEnabled())
	})
	if err := menu.Actions().Add(trackingAction); err != nil {
		return err
	}

	if err := addAction(menu, "Dashboard", func() { openDashboard(owner, trayApp) }); err != nil {
		return err
	}
	if err := addAction(menu, "Settings", func() { showSettings(owner, trayApp) }); err != nil {
		return err
	}
	if err := menu.Actions().Add(walk.NewSeparatorAction()); err != nil {
		return err
	}
	return addAction(menu, "Quit", func() {
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

func showSettings(owner walk.Form, trayApp *coreapp.App) {
	apiKey, err := trayApp.APIKey()
	if err != nil && !errors.Is(err, agentlib.ErrMissingAPIKey) {
		showError(owner, "Settings", err)
	}

	var dialog *walk.Dialog
	var apiKeyEdit *walk.LineEdit
	var launchAtLogin *walk.CheckBox

	var checkUpdatesButton *walk.PushButton
	children := []Widget{
		Label{
			Text: "API Key",
			Font: Font{Family: "Segoe UI", PointSize: 9, Bold: true},
		},
		LineEdit{
			AssignTo:  &apiKeyEdit,
			Text:      apiKey,
			CueBanner: "Paste your API key",
		},
	}
	children = append(children,
		Label{
			Text: "General",
			Font: Font{Family: "Segoe UI", PointSize: 9, Bold: true},
		},
		CheckBox{
			AssignTo: &launchAtLogin,
			Text:     "Launch at login",
			Checked:  launch.IsEnabled(),
		},
		Label{
			Text: "Updates",
			Font: Font{Family: "Segoe UI", PointSize: 9, Bold: true},
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
						runUpdateCheck(dialog, checkUpdatesButton)
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
								showError(dialog, "Settings", err)
								return
							}
						}
						if err := launch.SetEnabled(launchAtLogin.Checked()); err != nil {
							showError(dialog, "Settings", err)
							return
						}
						dialog.Accept()
					},
				},
			},
		},
	)

	_, err = Dialog{
		AssignTo:  &dialog,
		Title:     "Settings",
		MinSize:   Size{Width: 460, Height: 320},
		FixedSize: true,
		Font:      Font{Family: "Segoe UI", PointSize: 9},
		Layout: VBox{
			Margins: Margins{Left: 20, Top: 18, Right: 20, Bottom: 18},
			Spacing: 12,
		},
		Children: children,
	}.Run(owner)
	if err != nil {
		showError(owner, "Settings", err)
	}
}

// runUpdateCheck asks the server for a newer build off the UI thread, then
// reports back on it. A "yes" on the offer opens the download in the browser —
// the app never replaces its own running executable.
func runUpdateCheck(owner *walk.Dialog, button *walk.PushButton) {
	button.SetEnabled(false)
	go func() {
		update, err := appupdate.Check(context.Background(), agentlib.BaseURL(), version.Version)
		owner.Synchronize(func() {
			button.SetEnabled(true)
			switch {
			case errors.Is(err, appupdate.ErrDevBuild):
				walk.MsgBox(owner, "Updates",
					"This is a development build; update checks are disabled.",
					walk.MsgBoxIconInformation|walk.MsgBoxOK)
			case err != nil:
				showError(owner, "Updates", err)
			case update == nil:
				walk.MsgBox(owner, "Updates",
					fmt.Sprintf("You're up to date (%s).", version.Summary()),
					walk.MsgBoxIconInformation|walk.MsgBoxOK)
			default:
				answer := walk.MsgBox(owner, "Updates",
					fmt.Sprintf("Version %s is available. Download it now?", update.Version),
					walk.MsgBoxIconQuestion|walk.MsgBoxYesNo)
				if answer == walk.DlgCmdYes {
					verb := syscall.StringToUTF16Ptr("open")
					target := syscall.StringToUTF16Ptr(update.DownloadURL)
					if !win.ShellExecute(owner.Handle(), verb, target, nil, nil, win.SW_SHOWNORMAL) {
						showError(owner, "Updates", fmt.Errorf("open %s failed", update.DownloadURL))
					}
				}
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
				showError(owner, "Dashboard", fmt.Errorf("open %s failed", target))
			}
		})
	}()
}

func showError(owner walk.Form, title string, err error) {
	if err == nil {
		return
	}
	walk.MsgBox(owner, title, err.Error(), walk.MsgBoxIconError|walk.MsgBoxOK)
}

func truncateStatus(message string) string {
	message = strings.TrimSpace(message)
	if len(message) <= 160 {
		return message
	}
	return message[:157] + "..."
}
