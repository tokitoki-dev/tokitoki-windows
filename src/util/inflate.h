/* gzip (RFC 1952) decoder for the embedded CLI payload.
 *
 * DEFLATE decompression is delegated to the vendored miniz library
 * (third_party/miniz, pinned + digest-recorded); this module only parses the
 * gzip member header, feeds the raw DEFLATE stream to miniz, and verifies
 * the trailer's CRC32 and length against the decompressed output. */
#ifndef TOKITOKI_INFLATE_H
#define TOKITOKI_INFLATE_H

#include "util/buf.h"

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

/* Decompresses a whole gzip stream into `out` (appended). Fails on header,
 * stream, CRC32, or length mismatch. */
bool gzip_inflate(const uint8_t *src, size_t src_len, Buf *out);

#endif
