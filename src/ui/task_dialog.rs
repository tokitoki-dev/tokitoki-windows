//! Thin wrappers over `TaskDialogIndirect`.
//!
//! Unlike the Go original — which had to hand-pack the `#pragma pack(1)`
//! `TASKDIALOGCONFIG` into a 160-byte buffer — `windows-rs` exposes the
//! packed struct directly, so this is ordinary (if unsafe) code.
//! The window title is always "Tokitoki".

use windows::{
    core::{w, PCWSTR},
    Win32::UI::Controls::{
        TaskDialogIndirect, TASKDIALOGCONFIG, TASKDIALOGCONFIG_0, TASKDIALOG_BUTTON,
        TDCBF_OK_BUTTON, TDF_ALLOW_DIALOG_CANCELLATION, TDF_POSITION_RELATIVE_TO_WINDOW,
        TDF_USE_COMMAND_LINKS,
    },
};

use super::imp::wide;

/// `TD_INFORMATION_ICON` / `TD_ERROR_ICON` resource ids.
const ICON_INFORMATION: PCWSTR = PCWSTR(0xFFFD as *const u16);
const ICON_ERROR: PCWSTR = PCWSTR(0xFFFE as *const u16);
/// Command-link button ids start here (0..N map to `LINK_ID_BASE + i`).
const LINK_ID_BASE: i32 = 100;

/// Error dialog with an OK button.
pub(super) fn error(instruction: &str, content: &str) {
    let _ = show(instruction, content, &[], ICON_ERROR);
}

/// Command-link chooser; returns the 0-based index of the clicked link, or
/// `None` when cancelled.
pub(super) fn choice(instruction: &str, content: &str, links: &[&str]) -> Option<usize> {
    show(instruction, content, links, ICON_INFORMATION)
}

fn show(instruction: &str, content: &str, links: &[&str], icon: PCWSTR) -> Option<usize> {
    let instruction_wide = wide(instruction);
    let content_wide = wide(content);
    let link_texts: Vec<Vec<u16>> = links.iter().map(|link| wide(link)).collect();
    let buttons: Vec<TASKDIALOG_BUTTON> = link_texts
        .iter()
        .enumerate()
        .map(|(index, text)| TASKDIALOG_BUTTON {
            nButtonID: LINK_ID_BASE + i32::try_from(index).unwrap_or(0),
            pszButtonText: PCWSTR(text.as_ptr()),
        })
        .collect();

    let mut flags = TDF_ALLOW_DIALOG_CANCELLATION | TDF_POSITION_RELATIVE_TO_WINDOW;
    if !buttons.is_empty() {
        flags |= TDF_USE_COMMAND_LINKS;
    }

    let mut config = TASKDIALOGCONFIG {
        cbSize: u32::try_from(std::mem::size_of::<TASKDIALOGCONFIG>()).unwrap_or(0),
        dwFlags: flags,
        pszWindowTitle: w!("Tokitoki"),
        Anonymous1: TASKDIALOGCONFIG_0 { pszMainIcon: icon },
        pszMainInstruction: PCWSTR(instruction_wide.as_ptr()),
        pszContent: PCWSTR(content_wide.as_ptr()),
        ..Default::default()
    };
    if buttons.is_empty() {
        config.dwCommonButtons = TDCBF_OK_BUTTON;
    } else {
        config.cButtons = u32::try_from(buttons.len()).unwrap_or(0);
        config.pButtons = buttons.as_ptr();
    }

    let mut pressed = 0i32;
    // SAFETY: config and every wide string it points at stay alive for the
    // duration of the call; TaskDialogIndirect pumps its own message loop.
    unsafe { TaskDialogIndirect(&raw const config, Some(&raw mut pressed), None, None) }.ok()?;

    usize::try_from(pressed - LINK_ID_BASE)
        .ok()
        .filter(|index| *index < links.len())
}
