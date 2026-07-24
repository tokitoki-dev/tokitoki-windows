//go:build windows

package ui

import (
	"encoding/binary"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
)

// Task dialogs (comctl32 v6, guaranteed by the manifest) are the documented
// replacement for MessageBox: real instruction/content typography, command
// links instead of Yes/No, and a progress bar when we need one. Everything
// here still falls back to walk.MsgBox if TaskDialogIndirect is unavailable,
// so a broken comctl32 costs looks, never functionality.

var (
	comctl32               = syscall.NewLazyDLL("comctl32.dll")
	procTaskDialogIndirect = comctl32.NewProc("TaskDialogIndirect")
)

const (
	tdErrorIcon       = 0xFFFE // MAKEINTRESOURCE(-2)
	tdInformationIcon = 0xFFFD // MAKEINTRESOURCE(-3)

	tdfAllowDialogCancellation  = 0x0008
	tdfUseCommandLinks          = 0x0010
	tdfShowMarqueeProgressBar   = 0x0400
	tdfPositionRelativeToWindow = 0x1000

	tdcbfCancel = 0x0008

	tdnCreated           = 0
	tdnButtonClicked     = 2
	tdnDialogConstructed = 7

	tdmClickButton           = win.WM_USER + 102
	tdmSetProgressBarMarquee = win.WM_USER + 106

	idCancel = 2

	// Command links get IDs of their own, clear of the ID* constants.
	linkIDBase = 100
)

// TASKDIALOGCONFIG is declared with #pragma pack(1), which Go structs cannot
// express, so the config is assembled into a byte buffer at these fixed
// offsets. Both release architectures (amd64, arm64) are 64-bit, so the
// layout is one layout.
const (
	tdcSize            = 160
	offCbSize          = 0
	offHwndParent      = 4
	offFlags           = 20
	offCommonButtons   = 24
	offWindowTitle     = 28
	offMainIcon        = 36
	offMainInstruction = 44
	offContent         = 52
	offCButtons        = 60
	offPButtons        = 64
	offCallback        = 140

	// TASKDIALOG_BUTTON, same pack(1): a 4-byte ID and an 8-byte text pointer.
	tdButtonSize  = 12
	offButtonID   = 0
	offButtonText = 4
)

// taskDialog is one dialog invocation. run displays it modally and reports
// the outcome; only one can be on screen at a time, which the UI thread
// already guarantees.
type taskDialog struct {
	owner       walk.Form
	instruction string
	content     string
	icon        uintptr  // td*Icon, or 0 for none
	links       []string // command links; empty means a plain OK dialog
	marquee     bool     // indeterminate progress bar, with a Cancel button
	onCancel    func()   // invoked when the Cancel button is clicked

	hwnd   win.HWND // set while on screen
	closed bool     // close was requested, possibly before construction
}

// activeTaskDialog is the dialog the shared callback delivers to. Written
// and read on the UI thread only.
var activeTaskDialog *taskDialog

var taskDialogCallback = syscall.NewCallback(func(hwnd, msg, wParam, lParam, refData uintptr) uintptr {
	d := activeTaskDialog
	if d == nil {
		return 0
	}
	switch uint32(msg) {
	case tdnDialogConstructed:
		d.hwnd = win.HWND(hwnd)
		applyDialogTheme(d.hwnd)
		// The result may have arrived before the window existed.
		if d.closed {
			win.SendMessage(d.hwnd, tdmClickButton, idCancel, 0)
		}
	case tdnCreated:
		if d.marquee {
			win.SendMessage(win.HWND(hwnd), tdmSetProgressBarMarquee, 1, 0)
		}
	case tdnButtonClicked:
		if wParam == idCancel && d.onCancel != nil {
			d.onCancel()
		}
	}
	return 0
})

// run shows the dialog and blocks (pumping messages) until it closes. It
// returns the zero-based index of the chosen command link, or -1 for OK,
// Cancel, or Escape.
func (d *taskDialog) run() int {
	if procTaskDialogIndirect.Find() != nil {
		return d.fallback()
	}

	title, err := syscall.UTF16PtrFromString("Tokitoki")
	if err != nil {
		return d.fallback()
	}
	instruction, err := syscall.UTF16PtrFromString(d.instruction)
	if err != nil {
		return d.fallback()
	}
	content, err := syscall.UTF16PtrFromString(d.content)
	if err != nil {
		return d.fallback()
	}

	flags := uint32(tdfPositionRelativeToWindow | tdfAllowDialogCancellation)
	commonButtons := uint32(0)
	if d.marquee {
		flags |= tdfShowMarqueeProgressBar
		flags &^= tdfAllowDialogCancellation
		commonButtons = tdcbfCancel
	}

	// keep pins every pointer written into the packed buffers until the
	// call returns.
	var keep []unsafe.Pointer
	var buttons []byte
	if len(d.links) > 0 {
		flags |= tdfUseCommandLinks
		buttons = make([]byte, tdButtonSize*len(d.links))
		for i, link := range d.links {
			text, err := syscall.UTF16PtrFromString(link)
			if err != nil {
				return d.fallback()
			}
			keep = append(keep, unsafe.Pointer(text))
			base := i * tdButtonSize
			binary.LittleEndian.PutUint32(buttons[base+offButtonID:], uint32(linkIDBase+i))
			binary.LittleEndian.PutUint64(buttons[base+offButtonText:], uint64(uintptr(unsafe.Pointer(text))))
		}
	}

	cfg := make([]byte, tdcSize)
	putU32 := func(off int, v uint32) { binary.LittleEndian.PutUint32(cfg[off:], v) }
	putPtr := func(off int, v uintptr) { binary.LittleEndian.PutUint64(cfg[off:], uint64(v)) }

	putU32(offCbSize, tdcSize)
	if d.owner != nil {
		putPtr(offHwndParent, uintptr(d.owner.Handle()))
	}
	putU32(offFlags, flags)
	putU32(offCommonButtons, commonButtons)
	putPtr(offWindowTitle, uintptr(unsafe.Pointer(title)))
	putPtr(offMainIcon, d.icon)
	putPtr(offMainInstruction, uintptr(unsafe.Pointer(instruction)))
	putPtr(offContent, uintptr(unsafe.Pointer(content)))
	if len(buttons) > 0 {
		putU32(offCButtons, uint32(len(d.links)))
		putPtr(offPButtons, uintptr(unsafe.Pointer(&buttons[0])))
	}
	putPtr(offCallback, taskDialogCallback)

	prev := activeTaskDialog
	activeTaskDialog = d
	defer func() {
		activeTaskDialog = prev
		d.hwnd = 0
	}()

	var pressed int32
	ret, _, _ := procTaskDialogIndirect.Call(
		uintptr(unsafe.Pointer(&cfg[0])),
		uintptr(unsafe.Pointer(&pressed)),
		0, 0,
	)
	runtime.KeepAlive(title)
	runtime.KeepAlive(instruction)
	runtime.KeepAlive(content)
	runtime.KeepAlive(keep)
	runtime.KeepAlive(buttons)

	if ret != 0 { // FAILED(hr)
		return d.fallback()
	}
	if int(pressed) >= linkIDBase {
		return int(pressed) - linkIDBase
	}
	return -1
}

// close dismisses the dialog programmatically, as if Cancel were clicked. A
// close before the window exists is honored on construction.
func (d *taskDialog) close() {
	d.closed = true
	if d.hwnd != 0 {
		win.SendMessage(d.hwnd, tdmClickButton, idCancel, 0)
	}
}

// fallback is the MessageBox rendering of the same question, for the day
// TaskDialogIndirect cannot be reached. Two command links map onto Yes/No.
func (d *taskDialog) fallback() int {
	text := d.instruction
	if d.content != "" {
		text += "\n\n" + d.content
	}
	if len(d.links) == 0 {
		style := walk.MsgBoxIconInformation
		if d.icon == tdErrorIcon {
			style = walk.MsgBoxIconError
		}
		walk.MsgBox(d.owner, "Tokitoki", text, style|walk.MsgBoxOK)
		return -1
	}
	if walk.MsgBox(d.owner, "Tokitoki", text, walk.MsgBoxIconQuestion|walk.MsgBoxYesNo) == walk.DlgCmdYes {
		return 0
	}
	return 1
}

// taskDialogInfo reports something the user asked about.
func taskDialogInfo(owner walk.Form, instruction, content string) {
	dialog := taskDialog{owner: owner, instruction: instruction, content: content, icon: tdInformationIcon}
	dialog.run()
}

// taskDialogError reports a failure.
func taskDialogError(owner walk.Form, instruction, content string) {
	dialog := taskDialog{owner: owner, instruction: instruction, content: content, icon: tdErrorIcon}
	dialog.run()
}

// taskDialogChoice puts a decision to the user as command links and returns
// the chosen zero-based index, or -1 when dismissed.
func taskDialogChoice(owner walk.Form, instruction, content string, links ...string) int {
	dialog := taskDialog{owner: owner, instruction: instruction, content: content, links: links}
	return dialog.run()
}
