/* SHA-256 via Windows CNG (bcrypt) — no vendored crypto. */
#ifndef TOKITOKI_SHA256_H
#define TOKITOKI_SHA256_H

#include <stdbool.h>
#include <stddef.h>

typedef struct Sha256 {
    void *alg;  /* BCRYPT_ALG_HANDLE */
    void *hash; /* BCRYPT_HASH_HANDLE */
} Sha256;

bool sha256_init(Sha256 *ctx);
bool sha256_update(Sha256 *ctx, const void *data, size_t len);
/* Writes 64 lowercase hex chars + NUL and destroys the context. */
bool sha256_final_hex(Sha256 *ctx, char out_hex[65]);
void sha256_destroy(Sha256 *ctx);

/* One-shot convenience. */
bool sha256_hex_of(const void *data, size_t len, char out_hex[65]);

#endif
