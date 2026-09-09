/* gzip/DEFLATE decoder tests against real GZipStream output.
 *
 * The three fixtures deliberately exercise all block types: hello.gz (tiny,
 * fixed Huffman), lorem.gz (repetitive, dynamic Huffman + long matches), and
 * random.gz (incompressible, stored blocks). */
#include "test.h"

#include "util/buf.h"
#include "util/inflate.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static uint8_t *read_file(const char *path, size_t *out_len) {
    FILE *f = fopen(path, "rb");
    if (!f) {
        return NULL;
    }
    fseek(f, 0, SEEK_END);
    long size = ftell(f);
    fseek(f, 0, SEEK_SET);
    uint8_t *data = malloc(size > 0 ? (size_t)size : 1);
    if (data && size > 0 && fread(data, 1, (size_t)size, f) != (size_t)size) {
        free(data);
        data = NULL;
    }
    fclose(f);
    if (data) {
        *out_len = (size_t)size;
    }
    return data;
}

static void check_roundtrip(const char *gz_path, const char *raw_path) {
    size_t gz_len = 0;
    size_t raw_len = 0;
    uint8_t *gz = read_file(gz_path, &gz_len);
    uint8_t *raw = read_file(raw_path, &raw_len);
    TEST_CHECK(gz != NULL);
    TEST_CHECK(raw != NULL);
    if (!gz || !raw) {
        free(gz);
        free(raw);
        return;
    }
    Buf out;
    buf_init(&out);
    TEST_CHECK(gzip_inflate(gz, gz_len, &out));
    TEST_CHECK(out.len == raw_len);
    TEST_CHECK(out.len == raw_len && memcmp(out.data, raw, raw_len) == 0);
    buf_free(&out);
    free(gz);
    free(raw);
}

void test_inflate(void) {
    check_roundtrip("tests/fixtures/hello.gz", "tests/fixtures/hello.txt");
    check_roundtrip("tests/fixtures/lorem.gz", "tests/fixtures/lorem.txt");
    check_roundtrip("tests/fixtures/random.gz", "tests/fixtures/random.bin");

    /* Garbage and truncated input must fail, not crash. */
    Buf out;
    buf_init(&out);
    TEST_CHECK(!gzip_inflate((const uint8_t *)"not gzip data", 13, &out));
    size_t gz_len = 0;
    uint8_t *gz = read_file("tests/fixtures/lorem.gz", &gz_len);
    TEST_CHECK(gz != NULL);
    if (gz) {
        buf_free(&out);
        buf_init(&out);
        TEST_CHECK(!gzip_inflate(gz, gz_len / 2, &out));
        free(gz);
    }
    buf_free(&out);
}
