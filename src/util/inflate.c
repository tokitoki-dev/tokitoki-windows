#include "util/inflate.h"

/* Vendored miniz does the DEFLATE heavy lifting (see third_party/miniz).
 * Unity include: archive/stdio/time features compiled out, third-party
 * warnings silenced locally — /W4 /WX stays authoritative for OUR code. */
#define MINIZ_NO_STDIO
#define MINIZ_NO_TIME
#define MINIZ_NO_ARCHIVE_APIS
#define MINIZ_NO_ARCHIVE_WRITING_APIS
#define MINIZ_NO_ZLIB_COMPATIBLE_NAMES
#pragma warning(push, 0)
#include "../../third_party/miniz/miniz.c"
#pragma warning(pop)

/* Parses the gzip member header (RFC 1952); returns the DEFLATE offset.
 * All fields are bounded against len - 8: the header must end before the
 * trailer, or the later deflate-length subtraction would underflow. */
static bool gzip_header(const uint8_t *src, size_t len, size_t *offset) {
    if (len < 18 || src[0] != 0x1F || src[1] != 0x8B || src[2] != 8) {
        return false; /* 18 = minimal header (10) + trailer (8) */
    }
    size_t limit = len - 8;
    uint8_t flags = src[3];
    size_t pos = 10;
    if (flags & 0x04) { /* FEXTRA */
        if (limit - pos < 2) {
            return false;
        }
        size_t xlen = (size_t)src[pos] | ((size_t)src[pos + 1] << 8);
        pos += 2;
        if (limit - pos < xlen) {
            return false;
        }
        pos += xlen;
    }
    for (int i = 0; i < 2; i++) { /* FNAME then FCOMMENT: NUL-terminated */
        if (flags & (i == 0 ? 0x08 : 0x10)) {
            while (pos < limit && src[pos] != 0) {
                pos++;
            }
            if (pos >= limit) {
                return false;
            }
            pos++;
        }
    }
    if (flags & 0x02) { /* FHCRC */
        if (limit - pos < 2) {
            return false;
        }
        pos += 2;
    }
    if (pos >= limit) {
        return false; /* no room left for any DEFLATE data */
    }
    *offset = pos;
    return true;
}

static uint32_t read_le32(const uint8_t *p) {
    return (uint32_t)p[0] | ((uint32_t)p[1] << 8) | ((uint32_t)p[2] << 16) |
           ((uint32_t)p[3] << 24);
}

bool gzip_inflate(const uint8_t *src, size_t src_len, Buf *out) {
    size_t offset;
    if (!gzip_header(src, src_len, &offset)) {
        return false;
    }
    size_t deflate_len = src_len - 8 - offset; /* trailer: CRC32 + ISIZE */

    size_t decompressed_len = 0;
    void *decompressed = tinfl_decompress_mem_to_heap(
        src + offset, deflate_len, &decompressed_len, 0 /* raw DEFLATE */);
    if (!decompressed) {
        return false;
    }

    /* The gzip trailer ties the bytes to what was compressed: CRC32 plus
     * the length mod 2^32. */
    const uint8_t *trailer = src + src_len - 8;
    bool ok = read_le32(trailer) ==
                  (uint32_t)mz_crc32(MZ_CRC32_INIT, decompressed, decompressed_len) &&
              read_le32(trailer + 4) == (uint32_t)decompressed_len;
    if (ok) {
        ok = buf_push(out, decompressed, decompressed_len);
    }
    mz_free(decompressed);
    return ok;
}
