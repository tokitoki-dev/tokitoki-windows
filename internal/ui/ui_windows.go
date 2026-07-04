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

	"github.com/labx/tokitoki-agent/pkg/agentlib"
	coreapp "github.com/labx/tracklm-windows/internal/app"
	"github.com/labx/tracklm-windows/internal/launch"
	"github.com/labx/tracklm-windows/internal/version"
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
			openDashboard(mainWindow)
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
	mainWindow.Run()
	return nil
}

func buildMenu(owner *walk.MainWindow, notifyIcon *walk.NotifyIcon, trayApp *coreapp.App, logger *slog.Logger) error {
	menu := notifyIcon.ContextMenu()
	statusAction := walk.NewAction()
	if err := statusAction.SetText(statusText(trayApp.Status())); err != nil {
		return err
	}
	if err := statusAction.SetEnabled(false); err != nil {
		return err
	}
	if err := menu.Actions().Add(statusAction); err != nil {
		return err
	}
	if err := menu.Actions().Add(walk.NewSeparatorAction()); err != nil {
		return err
	}

	if err := addAction(menu, "Dashboard", func() { openDashboard(owner) }); err != nil {
		return err
	}
	if err := addAction(menu, "Sync now", trayApp.SyncNow); err != nil {
		return err
	}
	if err := addAction(menu, "Agents", func() { showSettings(owner, trayApp) }); err != nil {
		return err
	}
	if err := addAction(menu, "Settings", func() { showSettings(owner, trayApp) }); err != nil {
		return err
	}
	if err := menu.Actions().Add(walk.NewSeparatorAction()); err != nil {
		return err
	}
	if err := addAction(menu, "Quit", func() {
		logger.Info("quit requested")
		walk.App().Exit(0)
	}); err != nil {
		return err
	}

	lastWarning := trayApp.Status().LastError
	trayApp.OnStatusChanged(func(status coreapp.Status) {
		owner.Synchronize(func() {
			_ = statusAction.SetText(statusText(status))
			if status.LastError != "" && status.LastError != lastWarning && notifyIcon.Visible() {
				_ = notifyIcon.ShowWarning("TokiToki sync failed", truncateStatus(status.LastError))
			}
			lastWarning = status.LastError
		})
	})
	return nil
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
	enabled := providerSet(trayApp.EnabledProviders())

	var dialog *walk.Dialog
	var apiKeyEdit *walk.LineEdit
	var launchAtLogin *walk.CheckBox
	var claudeBox *walk.CheckBox
	var codexBox *walk.CheckBox

	_, err = Dialog{
		AssignTo:  &dialog,
		Title:     "Settings",
		MinSize:   Size{Width: 540, Height: 332},
		FixedSize: true,
		Font:      Font{Family: "Segoe UI", PointSize: 9},
		Layout: VBox{
			Margins: Margins{Left: 20, Top: 18, Right: 20, Bottom: 18},
			Spacing: 12,
		},
		Children: []Widget{
			Label{
				Text: "API Key",
				Font: Font{Family: "Segoe UI", PointSize: 9, Bold: true},
			},
			LineEdit{
				AssignTo:  &apiKeyEdit,
				Text:      apiKey,
				CueBanner: "Paste your API key",
			},
			Label{
				Text: "Agents",
				Font: Font{Family: "Segoe UI", PointSize: 9, Bold: true},
			},
			CheckBox{
				AssignTo: &claudeBox,
				Text:     "Claude Code",
				Checked:  enabled["claude"],
			},
			CheckBox{
				AssignTo: &codexBox,
				Text:     "Codex",
				Checked:  enabled["codex"],
			},
			Label{
				Text: "General",
				Font: Font{Family: "Segoe UI", PointSize: 9, Bold: true},
			},
			CheckBox{
				AssignTo: &launchAtLogin,
				Text:     "Launch at login",
				Checked:  launch.IsEnabled(),
			},
			Label{Text: version.Summary()},
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
							var providers []string
							if claudeBox.Checked() {
								providers = append(providers, "claude")
							}
							if codexBox.Checked() {
								providers = append(providers, "codex")
							}
							if len(providers) == 0 {
								showError(dialog, "Settings", errors.New("select at least one agent"))
								return
							}
							if err := trayApp.SetEnabledProviders(providers); err != nil {
								showError(dialog, "Settings", err)
								return
							}
							dialog.Accept()
						},
					},
				},
			},
		},
	}.Run(owner)
	if err != nil {
		showError(owner, "Settings", err)
	}
}

func openDashboard(owner *walk.MainWindow) {
	verb := syscall.StringToUTF16Ptr("open")
	target := syscall.StringToUTF16Ptr(coreapp.DashboardURL)
	if !win.ShellExecute(owner.Handle(), verb, target, nil, nil, win.SW_SHOWNORMAL) {
		showError(owner, "Dashboard", fmt.Errorf("open %s failed", coreapp.DashboardURL))
	}
}

func showError(owner walk.Form, title string, err error) {
	if err == nil {
		return
	}
	walk.MsgBox(owner, title, err.Error(), walk.MsgBoxIconError|walk.MsgBoxOK)
}

func providerSet(providers []string) map[string]bool {
	set := make(map[string]bool, len(providers))
	for _, provider := range providers {
		set[provider] = true
	}
	return set
}

func statusText(status coreapp.Status) string {
	switch {
	case status.Syncing:
		return "Status: syncing"
	case status.LastError != "":
		return "Status: failed"
	case !status.LastSyncAt.IsZero():
		return "Last sync: " + status.LastSyncAt.Local().Format("15:04")
	default:
		return "Status: idle"
	}
}

func truncateStatus(message string) string {
	message = strings.TrimSpace(message)
	if len(message) <= 160 {
		return message
	}
	return message[:157] + "..."
}
