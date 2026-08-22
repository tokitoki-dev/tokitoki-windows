/* Growable byte buffer. Every function that can allocate reports failure;
 * on failure the buffer stays valid (and freeable). */
#ifndef TOKITOKI_BUF_H
#define TOKITOKI_BUF_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

typedef struct Buf {
    uint8_t *data; /* malloc'd, may be NULL when len == cap == 0 */
    size_t len;
    size_t cap;
} Buf;

void buf_init(Buf *b);
bool buf_reserve(Buf *b, size_t additional);
bool buf_push(Buf *b, const void *bytes, size_t n);
bool buf_push_byte(Buf *b, uint8_t byte);
void buf_free(Buf *b);

#endif
