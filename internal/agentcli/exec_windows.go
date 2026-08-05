//go:build windows

package agentcli

import (
	"os/exec"
	"syscall"
)

const binaryName = "tokitoki.exe"

// createNoWindow keeps the child from opening a console. The CLI is a console
// program and the tray app is not; without this every sync would flash a
// black window.
const createNoWindow = 0x08000000

func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}
