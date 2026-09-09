/* Thin wrappers over TaskDialogIndirect (comctl32 v6, activated by the
 * manifest). The window title is always "Tokitoki". */
#include "ui/ui.h"

#include <commctrl.h>

#pragma comment(lib, "comctl32.lib")

#define LINK_ID_BASE 100

static int show_dialog(const wchar_t *instruction, const wchar_t *content,
                       const wchar_t *const *links, int link_count,
                       PCWSTR icon) {
    TASKDIALOG_BUTTON buttons[8];
    if (link_count > 8) {
        link_count = 8;
    }
    for (int i = 0; i < link_count; i++) {
        buttons[i].nButtonID = LINK_ID_BASE + i;
        buttons[i].pszButtonText = links[i];
    }

    TASKDIALOGCONFIG config;
    memset(&config, 0, sizeof(config));
    config.cbSize = sizeof(config);
    config.dwFlags = TDF_ALLOW_DIALOG_CANCELLATION | TDF_POSITION_RELATIVE_TO_WINDOW;
    config.pszWindowTitle = L"Tokitoki";
    config.pszMainIcon = icon;
    config.pszMainInstruction = instruction;
    config.pszContent = content;
    if (link_count > 0) {
        config.dwFlags |= TDF_USE_COMMAND_LINKS;
        config.cButtons = (UINT)link_count;
        config.pButtons = buttons;
    } else {
        config.dwCommonButtons = TDCBF_OK_BUTTON;
    }

    int pressed = 0;
    if (FAILED(TaskDialogIndirect(&config, &pressed, NULL, NULL))) {
        return -1;
    }
    if (pressed >= LINK_ID_BASE && pressed < LINK_ID_BASE + link_count) {
        return pressed - LINK_ID_BASE;
    }
    return -1;
}

void task_dialog_error(const wchar_t *instruction, const wchar_t *content) {
    show_dialog(instruction, content, NULL, 0, TD_ERROR_ICON);
}

int task_dialog_choice(const wchar_t *instruction, const wchar_t *content,
                       const wchar_t *const *links, int link_count) {
    return show_dialog(instruction, content, links, link_count, TD_INFORMATION_ICON);
}
