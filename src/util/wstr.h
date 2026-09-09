/* UTF-8 <-> UTF-16 conversion and string helpers.
 *
 * Internally the app speaks UTF-16 (native Win32 W APIs); UTF-8 appears only
 * at the CLI-stdout/JSON boundary. All returned strings are malloc'd and
 * NUL-terminated; the caller frees. */
#ifndef TOKITOKI_WSTR_H
#define TOKITOKI_WSTR_H

#include <stdbool.h>
#include <stddef.h>
#include <wchar.h>

/* UTF-8 -> UTF-16. `len` in bytes, or (size_t)-1 for NUL-terminated. */
wchar_t *wstr_from_utf8(const char *utf8, size_t len);

/* UTF-16 -> UTF-8. */
char *utf8_from_wstr(const wchar_t *wide);

wchar_t *wstr_dup(const wchar_t *s);

/* Truncates in place to at most `max_chars` code points, appending "..."
 * (inside the limit) when something was cut. Never splits a UTF-8 sequence —
 * the byte-truncation panic bug from the Rust port is the reason this helper
 * exists. */
void utf8_truncate_chars(char *s, size_t max_chars);

/* Trims ASCII whitespace in place (both ends). Returns `s`. */
char *utf8_trim(char *s);

#endif
