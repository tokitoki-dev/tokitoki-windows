//go:build !windows

package agentcli

import "os/exec"

const binaryName = "tokitoki"

func hideConsole(*exec.Cmd) {}
