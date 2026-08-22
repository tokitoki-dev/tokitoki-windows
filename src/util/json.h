/* Minimal JSON reader for the narrow shapes this app consumes:
 * one top-level object with string/number/bool values, where unknown members
 * (including nested arrays/objects, e.g. the legacy `enabled_providers`)
 * must be skipped correctly.
 *
 * Not a general-purpose parser on purpose: the update-check, verify, CLI-ok,
 * and settings payloads are all flat objects. */
#ifndef TOKITOKI_JSON_H
#define TOKITOKI_JSON_H

#include <stdbool.h>
#include <stddef.h>

typedef struct JsonSlice {
    const char *start; /* raw value text inside the document */
    size_t len;
} JsonSlice;

/* Finds `key` in the top-level object of `json`. Returns false when the
 * document is not an object or the key is absent/malformed. */
bool json_obj_get(const char *json, const char *key, JsonSlice *out);

bool json_as_bool(JsonSlice v, bool *out);
bool json_as_u64(JsonSlice v, unsigned long long *out);
/* Unescapes into a malloc'd UTF-8 string (handles \", \\, \/, \b, \f, \n,
 * \r, \t, \uXXXX incl. surrogate pairs). Caller frees. */
char *json_as_string(JsonSlice v);

/* Convenience: true only when the document parses and `key` is `true`. */
bool json_bool_is_true(const char *json, const char *key);

#endif
