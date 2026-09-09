#include "util/wstr.h"

#include <windows.h>

#include <stdlib.h>
#include <string.h>

wchar_t *wstr_from_utf8(const char *utf8, size_t len) {
    if (!utf8) {
        return NULL;
    }
    int in_len = (len == (size_t)-1) ? -1 : (int)len;
    int needed = MultiByteToWideChar(CP_UTF8, 0, utf8, in_len, NULL, 0);
    if (needed <= 0) {
        /* Empty input still deserves an empty string. */
        return wstr_dup(L"");
    }
    /* +1: with an explicit length, the converted text is not NUL-terminated. */
    wchar_t *wide = malloc(((size_t)needed + 1) * sizeof(wchar_t));
    if (!wide) {
        return NULL;
    }
    MultiByteToWideChar(CP_UTF8, 0, utf8, in_len, wide, needed);
    wide[needed] = L'\0';
    if (in_len == -1) {
        /* Converted length included the NUL already. */
        wide[needed - 1] = L'\0';
    }
    return wide;
}

char *utf8_from_wstr(const wchar_t *wide) {
    if (!wide) {
        return NULL;
    }
    int needed = WideCharToMultiByte(CP_UTF8, 0, wide, -1, NULL, 0, NULL, NULL);
    if (needed <= 0) {
        return NULL;
    }
    char *utf8 = malloc((size_t)needed);
    if (!utf8) {
        return NULL;
    }
    WideCharToMultiByte(CP_UTF8, 0, wide, -1, utf8, needed, NULL, NULL);
    return utf8;
}

wchar_t *wstr_dup(const wchar_t *s) {
    if (!s) {
        return NULL;
    }
    size_t bytes = (wcslen(s) + 1) * sizeof(wchar_t);
    wchar_t *copy = malloc(bytes);
    if (copy) {
        memcpy(copy, s, bytes);
    }
    return copy;
}

/* A byte starts a UTF-8 code point unless it is a continuation (10xxxxxx). */
static bool is_utf8_start(unsigned char byte) {
    return (byte & 0xC0) != 0x80;
}

void utf8_truncate_chars(char *s, size_t max_chars) {
    if (!s || max_chars < 4) {
        return;
    }
    size_t chars = 0;
    size_t i = 0;
    size_t cut_at = 0; /* byte offset of the (max_chars - 3)th char */
    while (s[i]) {
        if (is_utf8_start((unsigned char)s[i])) {
            if (chars == max_chars - 3) {
                cut_at = i;
            }
            if (chars == max_chars) {
                /* Over the limit: cut three chars early, append ellipsis. */
                s[cut_at] = '.';
                s[cut_at + 1] = '.';
                s[cut_at + 2] = '.';
                s[cut_at + 3] = '\0';
                return;
            }
            chars++;
        }
        i++;
    }
}

char *utf8_trim(char *s) {
    if (!s) {
        return s;
    }
    size_t len = strlen(s);
    size_t start = 0;
    while (start < len && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r' || s[start] == '\n')) {
        start++;
    }
    size_t end = len;
    while (end > start && (s[end - 1] == ' ' || s[end - 1] == '\t' || s[end - 1] == '\r' || s[end - 1] == '\n')) {
        end--;
    }
    if (start > 0) {
        memmove(s, s + start, end - start);
    }
    s[end - start] = '\0';
    return s;
}
