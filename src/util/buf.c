#include "util/buf.h"

#include <stdlib.h>
#include <string.h>

void buf_init(Buf *b) {
    b->data = NULL;
    b->len = 0;
    b->cap = 0;
}

bool buf_reserve(Buf *b, size_t additional) {
    if (additional > SIZE_MAX - b->len) {
        return false;
    }
    size_t needed = b->len + additional;
    if (needed <= b->cap) {
        return true;
    }
    size_t cap = b->cap ? b->cap : 64;
    while (cap < needed) {
        if (cap > SIZE_MAX / 2) {
            cap = needed;
            break;
        }
        cap *= 2;
    }
    uint8_t *grown = realloc(b->data, cap);
    if (!grown) {
        return false;
    }
    b->data = grown;
    b->cap = cap;
    return true;
}

bool buf_push(Buf *b, const void *bytes, size_t n) {
    if (!buf_reserve(b, n)) {
        return false;
    }
    memcpy(b->data + b->len, bytes, n);
    b->len += n;
    return true;
}

bool buf_push_byte(Buf *b, uint8_t byte) {
    return buf_push(b, &byte, 1);
}

void buf_free(Buf *b) {
    free(b->data);
    buf_init(b);
}
