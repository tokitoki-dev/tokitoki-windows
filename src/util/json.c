#include "util/json.h"

#include "util/buf.h"

#include <stdlib.h>
#include <string.h>

typedef struct Cursor {
    const char *p;
    const char *end;
} Cursor;

static void skip_ws(Cursor *c) {
    while (c->p < c->end &&
           (*c->p == ' ' || *c->p == '\t' || *c->p == '\n' || *c->p == '\r')) {
        c->p++;
    }
}

static bool skip_value(Cursor *c);

static bool skip_string(Cursor *c) {
    if (c->p >= c->end || *c->p != '"') {
        return false;
    }
    c->p++;
    while (c->p < c->end) {
        char ch = *c->p++;
        if (ch == '\\') {
            if (c->p >= c->end) {
                return false;
            }
            c->p++; /* the escaped char; \uXXXX hex is plain chars after */
        } else if (ch == '"') {
            return true;
        }
    }
    return false;
}

static bool skip_container(Cursor *c, char open, char close) {
    if (c->p >= c->end || *c->p != open) {
        return false;
    }
    c->p++;
    skip_ws(c);
    if (c->p < c->end && *c->p == close) {
        c->p++;
        return true;
    }
    for (;;) {
        skip_ws(c);
        if (open == '{') {
            if (!skip_string(c)) {
                return false;
            }
            skip_ws(c);
            if (c->p >= c->end || *c->p != ':') {
                return false;
            }
            c->p++;
        }
        if (!skip_value(c)) {
            return false;
        }
        skip_ws(c);
        if (c->p >= c->end) {
            return false;
        }
        if (*c->p == ',') {
            c->p++;
            continue;
        }
        if (*c->p == close) {
            c->p++;
            return true;
        }
        return false;
    }
}

static bool skip_literal(Cursor *c, const char *lit) {
    size_t len = strlen(lit);
    if ((size_t)(c->end - c->p) < len || memcmp(c->p, lit, len) != 0) {
        return false;
    }
    c->p += len;
    return true;
}

static bool skip_number(Cursor *c) {
    const char *start = c->p;
    if (c->p < c->end && *c->p == '-') {
        c->p++;
    }
    while (c->p < c->end &&
           ((*c->p >= '0' && *c->p <= '9') || *c->p == '.' || *c->p == 'e' ||
            *c->p == 'E' || *c->p == '+' || *c->p == '-')) {
        c->p++;
    }
    return c->p > start;
}

static bool skip_value(Cursor *c) {
    skip_ws(c);
    if (c->p >= c->end) {
        return false;
    }
    switch (*c->p) {
    case '"':
        return skip_string(c);
    case '{':
        return skip_container(c, '{', '}');
    case '[':
        return skip_container(c, '[', ']');
    case 't':
        return skip_literal(c, "true");
    case 'f':
        return skip_literal(c, "false");
    case 'n':
        return skip_literal(c, "null");
    default:
        return skip_number(c);
    }
}

/* Matches a raw (escaped) JSON string against a plain key. Keys in our
 * payloads are ASCII identifiers, so escape forms never match. */
static bool string_equals_key(const char *raw_start, const char *raw_end, const char *key) {
    size_t key_len = strlen(key);
    /* raw includes the surrounding quotes */
    if ((size_t)(raw_end - raw_start) != key_len + 2) {
        return false;
    }
    return memcmp(raw_start + 1, key, key_len) == 0;
}

bool json_obj_get(const char *json, const char *key, JsonSlice *out) {
    if (!json) {
        return false;
    }
    Cursor c = {json, json + strlen(json)};
    skip_ws(&c);
    if (c.p >= c.end || *c.p != '{') {
        return false;
    }
    c.p++;
    skip_ws(&c);
    if (c.p < c.end && *c.p == '}') {
        return false;
    }
    for (;;) {
        skip_ws(&c);
        const char *key_start = c.p;
        if (!skip_string(&c)) {
            return false;
        }
        const char *key_end = c.p;
        skip_ws(&c);
        if (c.p >= c.end || *c.p != ':') {
            return false;
        }
        c.p++;
        skip_ws(&c);
        const char *value_start = c.p;
        if (!skip_value(&c)) {
            return false;
        }
        if (string_equals_key(key_start, key_end, key)) {
            out->start = value_start;
            out->len = (size_t)(c.p - value_start);
            return true;
        }
        skip_ws(&c);
        if (c.p < c.end && *c.p == ',') {
            c.p++;
            continue;
        }
        return false; /* '}' or malformed: key not found */
    }
}

bool json_as_bool(JsonSlice v, bool *out) {
    if (v.len == 4 && memcmp(v.start, "true", 4) == 0) {
        *out = true;
        return true;
    }
    if (v.len == 5 && memcmp(v.start, "false", 5) == 0) {
        *out = false;
        return true;
    }
    return false;
}

bool json_as_u64(JsonSlice v, unsigned long long *out) {
    if (v.len == 0 || v.len > 20) {
        return false;
    }
    unsigned long long value = 0;
    for (size_t i = 0; i < v.len; i++) {
        char ch = v.start[i];
        if (ch < '0' || ch > '9') {
            return false;
        }
        if (value > (~0ULL - (unsigned long long)(ch - '0')) / 10) {
            return false;
        }
        value = value * 10 + (unsigned long long)(ch - '0');
    }
    *out = value;
    return true;
}

static bool push_utf8_codepoint(Buf *out, unsigned cp) {
    if (cp < 0x80) {
        return buf_push_byte(out, (uint8_t)cp);
    }
    if (cp < 0x800) {
        return buf_push_byte(out, (uint8_t)(0xC0 | (cp >> 6))) &&
               buf_push_byte(out, (uint8_t)(0x80 | (cp & 0x3F)));
    }
    if (cp < 0x10000) {
        return buf_push_byte(out, (uint8_t)(0xE0 | (cp >> 12))) &&
               buf_push_byte(out, (uint8_t)(0x80 | ((cp >> 6) & 0x3F))) &&
               buf_push_byte(out, (uint8_t)(0x80 | (cp & 0x3F)));
    }
    return buf_push_byte(out, (uint8_t)(0xF0 | (cp >> 18))) &&
           buf_push_byte(out, (uint8_t)(0x80 | ((cp >> 12) & 0x3F))) &&
           buf_push_byte(out, (uint8_t)(0x80 | ((cp >> 6) & 0x3F))) &&
           buf_push_byte(out, (uint8_t)(0x80 | (cp & 0x3F)));
}

static int hex_value(char ch) {
    if (ch >= '0' && ch <= '9') {
        return ch - '0';
    }
    if (ch >= 'a' && ch <= 'f') {
        return ch - 'a' + 10;
    }
    if (ch >= 'A' && ch <= 'F') {
        return ch - 'A' + 10;
    }
    return -1;
}

static bool parse_u16_hex(const char *p, unsigned *out) {
    unsigned value = 0;
    for (int i = 0; i < 4; i++) {
        int digit = hex_value(p[i]);
        if (digit < 0) {
            return false;
        }
        value = (value << 4) | (unsigned)digit;
    }
    *out = value;
    return true;
}

char *json_as_string(JsonSlice v) {
    if (v.len < 2 || v.start[0] != '"' || v.start[v.len - 1] != '"') {
        return NULL;
    }
    const char *p = v.start + 1;
    const char *end = v.start + v.len - 1;
    Buf out;
    buf_init(&out);
    bool ok = true;
    while (ok && p < end) {
        char ch = *p++;
        if (ch != '\\') {
            ok = buf_push_byte(&out, (uint8_t)ch);
            continue;
        }
        if (p >= end) {
            ok = false;
            break;
        }
        char esc = *p++;
        switch (esc) {
        case '"':
        case '\\':
        case '/':
            ok = buf_push_byte(&out, (uint8_t)esc);
            break;
        case 'b':
            ok = buf_push_byte(&out, '\b');
            break;
        case 'f':
            ok = buf_push_byte(&out, '\f');
            break;
        case 'n':
            ok = buf_push_byte(&out, '\n');
            break;
        case 'r':
            ok = buf_push_byte(&out, '\r');
            break;
        case 't':
            ok = buf_push_byte(&out, '\t');
            break;
        case 'u': {
            unsigned cp;
            if (end - p < 4 || !parse_u16_hex(p, &cp)) {
                ok = false;
                break;
            }
            p += 4;
            if (cp >= 0xD800 && cp <= 0xDBFF && end - p >= 6 && p[0] == '\\' &&
                p[1] == 'u') {
                unsigned low;
                if (parse_u16_hex(p + 2, &low) && low >= 0xDC00 && low <= 0xDFFF) {
                    cp = 0x10000 + ((cp - 0xD800) << 10) + (low - 0xDC00);
                    p += 6;
                }
            }
            ok = push_utf8_codepoint(&out, cp);
            break;
        }
        default:
            ok = false;
            break;
        }
    }
    if (!ok || !buf_push_byte(&out, 0)) {
        buf_free(&out);
        return NULL;
    }
    return (char *)out.data; /* ownership moves to the caller */
}

bool json_bool_is_true(const char *json, const char *key) {
    JsonSlice value;
    bool result = false;
    return json_obj_get(json, key, &value) && json_as_bool(value, &result) && result;
}
