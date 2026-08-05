//go:build windows

package ui

import (
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

// mutedTextColor is the secondary text color for row descriptions and the
// version line, in whichever theme the app is currently drawing.
func mutedTextColor() walk.Color {
	if appsUseLightTheme() {
		return walk.RGB(0x6B, 0x72, 0x80)
	}
	return walk.RGB(0xA1, 0xA1, 0xAA)
}

// separatorColor is the hairline that divides the dialog's sections.
func separatorColor() walk.Color {
	if appsUseLightTheme() {
		return walk.RGB(0xE5, 0xE7, 0xEB)
	}
	return walk.RGB(0x3F, 0x3F, 0x46)
}

// separatorLine is a one-pixel rule. walk's own HSeparator draws the etched
// system divider, which all but disappears against a dark dialog, so the line
// is a filled strip instead and stays visible in either theme.
func separatorLine() Widget {
	return Composite{
		Layout:     HBox{MarginsZero: true},
		Background: SolidColorBrush{Color: separatorColor()},
		MinSize:    Size{Height: 1},
		MaxSize:    Size{Height: 1},
	}
}

// settingRow lays out one preference the way the product design does: a bold
// title over a muted description on the left, the on/off control pinned
// right. The control is the native Win32 checkbox — the standard control for
// an enabled/disabled state — and onToggled applies the change the moment it
// is made; there is no Save step.
func settingRow(titleFont Font, muted walk.Color, box **walk.CheckBox, checked bool, title, description string, onToggled walk.EventHandler) Widget {
	return Composite{
		Layout: HBox{MarginsZero: true, Spacing: 12},
		Children: []Widget{
			Composite{
				Layout: VBox{MarginsZero: true, Spacing: 2},
				Children: []Widget{
					Label{Text: title, Font: titleFont},
					Label{Text: description, TextColor: muted},
				},
			},
			HSpacer{},
			CheckBox{
				AssignTo:         box,
				Checked:          checked,
				OnCheckedChanged: onToggled,
			},
		},
	}
}
