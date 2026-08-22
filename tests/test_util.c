/* Tests for buf, wstr, json, sha256. */
#include "test.h"

#include "util/buf.h"
#include "util/json.h"
#include "util/sha256.h"
#include "util/wstr.h"

#include <stdlib.h>
#include <string.h>

void test_buf(void) {
    Buf b;
    buf_init(&b);
    TEST_CHECK(buf_push(&b, "abc", 3));
    TEST_CHECK(buf_push_byte(&b, 'd'));
    TEST_CHECK(b.len == 4);
    TEST_CHECK(memcmp(b.data, "abcd", 4) == 0);

    /* Growth across many pushes keeps content intact. */
    for (int i = 0; i < 1000; i++) {
        TEST_CHECK(buf_push_byte(&b, (uint8_t)(i & 0xFF)));
    }
    TEST_CHECK(b.len == 1004);
    TEST_CHECK(b.data[4] == 0 && b.data[1003] == (uint8_t)(999 & 0xFF));
    buf_free(&b);
    TEST_CHECK(b.data == NULL && b.len == 0);
}

void test_wstr(void) {
    /* Round-trip including non-ASCII. */
    wchar_t *wide = wstr_from_utf8("caf\xC3\xA9 \xE4\xB8\xAD\xE6\x96\x87", (size_t)-1);
    TEST_CHECK(wide && wcscmp(wide, L"caf\x00E9 \x4E2D\x6587") == 0);
    char *back = utf8_from_wstr(wide);
    TEST_CHECK_STR_EQ(back, "caf\xC3\xA9 \xE4\xB8\xAD\xE6\x96\x87");
    free(wide);
    free(back);

    /* Char-safe truncation: never splits a multibyte sequence. */
    char short_text[] = "hello";
    utf8_truncate_chars(short_text, 10);
    TEST_CHECK_STR_EQ(short_text, "hello");

    char long_ascii[] = "abcdefghij";
    utf8_truncate_chars(long_ascii, 8);
    TEST_CHECK_STR_EQ(long_ascii, "abcde...");

    /* 10 x U+4E2D (3 bytes each); limit 8 chars -> 5 chars + "...". */
    char cjk[31];
    for (int i = 0; i < 10; i++) {
        memcpy(cjk + i * 3, "\xE4\xB8\xAD", 3);
    }
    cjk[30] = '\0';
    utf8_truncate_chars(cjk, 8);
    TEST_CHECK(strlen(cjk) == 5 * 3 + 3);
    TEST_CHECK(strcmp(cjk + 15, "...") == 0);

    char trimmed[] = "  \t value \r\n";
    TEST_CHECK_STR_EQ(utf8_trim(trimmed), "value");
}

void test_json(void) {
    JsonSlice v;

    /* The CLI ok contract. */
    TEST_CHECK(json_bool_is_true("{\"ok\":true}", "ok"));
    TEST_CHECK(!json_bool_is_true("{\"ok\":false}", "ok"));
    TEST_CHECK(!json_bool_is_true("not json", "ok"));
    TEST_CHECK(!json_bool_is_true("{}", "ok"));

    /* Update-check shape: mixed types, key order independent. */
    const char *check =
        "{\"available\": true, \"version\": \"1.2.3\", "
        "\"url\": \"/dl/a\\/b.exe\", \"size\": 4200000, \"sha256\": \"abc\"}";
    TEST_CHECK(json_obj_get(check, "version", &v));
    char *version = json_as_string(v);
    TEST_CHECK_STR_EQ(version, "1.2.3");
    free(version);
    TEST_CHECK(json_obj_get(check, "url", &v));
    char *url = json_as_string(v);
    TEST_CHECK_STR_EQ(url, "/dl/a/b.exe");
    free(url);
    unsigned long long size = 0;
    TEST_CHECK(json_obj_get(check, "size", &v) && json_as_u64(v, &size));
    TEST_CHECK(size == 4200000ULL);

    /* Legacy settings: nested array under an unknown key must be skipped. */
    const char *legacy =
        "{\"enabled_providers\":[\"claude\",{\"x\":[1,2]}],"
        "\"tracking_disabled\":true}";
    TEST_CHECK(json_bool_is_true(legacy, "tracking_disabled"));

    /* Escapes incl. \uXXXX and a surrogate pair. */
    TEST_CHECK(json_obj_get("{\"s\":\"a\\n\\t\\\"\\u00e9\\ud83d\\ude00\"}", "s", &v));
    char *escaped = json_as_string(v);
    TEST_CHECK_STR_EQ(escaped, "a\n\t\"\xC3\xA9\xF0\x9F\x98\x80");
    free(escaped);

    /* Absent key and malformed docs refuse politely. */
    TEST_CHECK(!json_obj_get("{\"a\":1}", "b", &v));
    TEST_CHECK(!json_obj_get("[1,2]", "a", &v));
    TEST_CHECK(!json_obj_get("{\"a\":", "a", &v));
}

void test_sha256(void) {
    char hex[65];
    /* Known vector: sha256("abc"). */
    TEST_CHECK(sha256_hex_of("abc", 3, hex));
    TEST_CHECK_STR_EQ(
        hex, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad");

    /* Streaming across chunks equals one-shot. */
    Sha256 ctx;
    TEST_CHECK(sha256_init(&ctx));
    TEST_CHECK(sha256_update(&ctx, "a", 1));
    TEST_CHECK(sha256_update(&ctx, "bc", 2));
    char streamed[65];
    TEST_CHECK(sha256_final_hex(&ctx, streamed));
    TEST_CHECK_STR_EQ(streamed, hex);
}
