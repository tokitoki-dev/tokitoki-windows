/* Procedural clock-mark renderer (mirrors Go internal/logo): a ring plus one
 * hand pointing to ten o'clock, rendered from signed distance fields with
 * 3x3 supersampling and a 1 px AA ramp. No bitmap assets; the tray glyph is
 * generated at runtime at the taskbar's DPI. */
#ifndef TOKITOKI_LOGO_H
#define TOKITOKI_LOGO_H

#include <stdbool.h>
#include <stdint.h>

/* Fills `rgba` (size*size*4 bytes, straight alpha, row-major) with the mark
 * in the given color. `white` selects the white glyph (dark taskbars);
 * otherwise the dark #18181B glyph (light taskbars). */
void logo_mark(uint32_t size, bool white, uint8_t *rgba);

#endif
