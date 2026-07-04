//go:build windows

package ui

import "github.com/lxn/walk"

const appIconResourceID = 2

func newAppIcon() (*walk.Icon, error) {
	return walk.NewIconFromResourceId(appIconResourceID)
}
