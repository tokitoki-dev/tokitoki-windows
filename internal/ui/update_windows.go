//go:build windows

package ui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
	"time"

	"github.com/lxn/walk"
	"github.com/tokitoki-dev/tokitoki-cli/pkg/agentlib"
	"github.com/tokitoki-dev/tokitoki-windows/internal/appupdate"
	"github.com/tokitoki-dev/tokitoki-windows/internal/version"
)

const (
	// firstCheckDelay keeps the update check out of the app's startup work.
	firstCheckDelay = time.Minute
	checkInterval   = 24 * time.Hour
)

// restartTarget is the executable main relaunches after the message loop
// ends. Written on the UI thread before Exit, read after Run returns.
var restartTarget string

// PendingRestart reports the executable to start again — set when the user
// accepted an installed update's restart offer.
func PendingRestart() (string, bool) {
	return restartTarget, restartTarget != ""
}

// updater funnels both update entry points — the Settings button and the
// background check — into one install pipeline: confirm, download and swap,
// offer a restart.
type updater struct {
	owner      *walk.MainWindow
	notifyIcon *walk.NotifyIcon
	logger     *slog.Logger
	installing atomic.Bool

	// UI-thread state.
	latest       *appupdate.Update
	updateAction *walk.Action
}

func newUpdater(owner *walk.MainWindow, notifyIcon *walk.NotifyIcon, logger *slog.Logger) *updater {
	u := &updater{owner: owner, notifyIcon: notifyIcon, logger: logger}
	notifyIcon.MessageClicked().Attach(func() {
		if u.latest != nil {
			u.offerInstall(u.owner, u.latest)
		}
	})
	return u
}

// run is the background schedule: one check shortly after launch, then one
// a day. It reports only news — errors and "already current" stay silent,
// because nobody asked.
func (u *updater) run(ctx context.Context) {
	timer := time.NewTimer(firstCheckDelay)
	defer timer.Stop()

	var offered string
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		timer.Reset(checkInterval)

		update, err := appupdate.Check(ctx, agentlib.BaseURL(), version.Version)
		switch {
		case errors.Is(err, appupdate.ErrDevBuild):
			return
		case err != nil:
			u.logger.Debug("background update check", "error", err)
		case update == nil:
			// Already current.
		case update.Version != offered:
			offered = update.Version
			u.announce(update)
		}
	}
}

// announce surfaces a fresh offer: a tray balloon, and a menu entry that
// stays after the balloon fades.
func (u *updater) announce(update *appupdate.Update) {
	u.owner.Synchronize(func() {
		u.latest = update
		u.ensureMenuAction()
		_ = u.updateAction.SetText(fmt.Sprintf("Install update %s…", update.Version))
		_ = u.notifyIcon.ShowInfo("Tokitoki update available",
			fmt.Sprintf("Version %s is ready to install.", update.Version))
	})
}

// ensureMenuAction inserts the install entry right below Settings, above
// the separator that guards Quit.
func (u *updater) ensureMenuAction() {
	if u.updateAction != nil {
		return
	}
	action := walk.NewAction()
	action.Triggered().Attach(func() {
		if u.latest != nil {
			u.offerInstall(u.owner, u.latest)
		}
	})
	actions := u.notifyIcon.ContextMenu().Actions()
	_ = actions.Insert(actions.Len()-2, action)
	u.updateAction = action
}

// clearOffer retires the menu entry once the update is on disk.
func (u *updater) clearOffer() {
	u.latest = nil
	if u.updateAction != nil {
		_ = u.notifyIcon.ContextMenu().Actions().Remove(u.updateAction)
		u.updateAction = nil
	}
}

// offerInstall asks, then installs. Runs on the UI thread.
func (u *updater) offerInstall(owner walk.Form, update *appupdate.Update) {
	if u.installing.Load() {
		return
	}
	answer := walk.MsgBox(owner, "Updates",
		fmt.Sprintf("Version %s is available. Install now?", update.Version),
		walk.MsgBoxIconQuestion|walk.MsgBoxYesNo)
	if answer == walk.DlgCmdYes {
		u.install(update)
	}
}

// install downloads and swaps the binary off the UI thread, then offers the
// restart on it. From here on the update cannot be lost: even a declined
// restart leaves the new build on disk for the next launch.
func (u *updater) install(update *appupdate.Update) {
	if !u.installing.CompareAndSwap(false, true) {
		return
	}
	go func() {
		err := appupdate.Install(context.Background(), update)
		u.owner.Synchronize(func() {
			u.installing.Store(false)
			if err != nil {
				showError(u.owner, "Updates", err)
				return
			}
			u.clearOffer()
			answer := walk.MsgBox(u.owner, "Updates",
				fmt.Sprintf("Version %s is installed. Restart Tokitoki now to start using it?", update.Version),
				walk.MsgBoxIconQuestion|walk.MsgBoxYesNo)
			if answer == walk.DlgCmdYes {
				u.requestRestart()
			}
		})
	}()
}

// requestRestart records the relaunch target and ends the message loop.
// The relaunch itself happens in main, after the instance lock is released.
func (u *updater) requestRestart() {
	executable, err := os.Executable()
	if err != nil {
		showError(u.owner, "Updates", err)
		return
	}
	restartTarget = executable
	walk.App().Exit(0)
}
