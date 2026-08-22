#include "util/sha256.h"

#include <windows.h>

#include <bcrypt.h>

#include <string.h>

#pragma comment(lib, "bcrypt.lib")

bool sha256_init(Sha256 *ctx) {
    ctx->alg = NULL;
    ctx->hash = NULL;
    BCRYPT_ALG_HANDLE alg = NULL;
    if (!BCRYPT_SUCCESS(BCryptOpenAlgorithmProvider(&alg, BCRYPT_SHA256_ALGORITHM, NULL, 0))) {
        return false;
    }
    BCRYPT_HASH_HANDLE hash = NULL;
    if (!BCRYPT_SUCCESS(BCryptCreateHash(alg, &hash, NULL, 0, NULL, 0, 0))) {
        BCryptCloseAlgorithmProvider(alg, 0);
        return false;
    }
    ctx->alg = alg;
    ctx->hash = hash;
    return true;
}

bool sha256_update(Sha256 *ctx, const void *data, size_t len) {
    if (!ctx->hash) {
        return false;
    }
    /* BCryptHashData takes a ULONG; feed large inputs in chunks. */
    const unsigned char *p = data;
    while (len > 0) {
        ULONG chunk = (len > 1u << 30) ? (1u << 30) : (ULONG)len;
        if (!BCRYPT_SUCCESS(BCryptHashData(ctx->hash, (PUCHAR)p, chunk, 0))) {
            return false;
        }
        p += chunk;
        len -= chunk;
    }
    return true;
}

bool sha256_final_hex(Sha256 *ctx, char out_hex[65]) {
    unsigned char digest[32];
    bool ok = ctx->hash &&
              BCRYPT_SUCCESS(BCryptFinishHash(ctx->hash, digest, sizeof(digest), 0));
    sha256_destroy(ctx);
    if (!ok) {
        return false;
    }
    static const char hex[] = "0123456789abcdef";
    for (size_t i = 0; i < sizeof(digest); i++) {
        out_hex[i * 2] = hex[digest[i] >> 4];
        out_hex[i * 2 + 1] = hex[digest[i] & 0x0F];
    }
    out_hex[64] = '\0';
    return true;
}

void sha256_destroy(Sha256 *ctx) {
    if (ctx->hash) {
        BCryptDestroyHash(ctx->hash);
        ctx->hash = NULL;
    }
    if (ctx->alg) {
        BCryptCloseAlgorithmProvider(ctx->alg, 0);
        ctx->alg = NULL;
    }
}

bool sha256_hex_of(const void *data, size_t len, char out_hex[65]) {
    Sha256 ctx;
    if (!sha256_init(&ctx)) {
        return false;
    }
    if (!sha256_update(&ctx, data, len)) {
        sha256_destroy(&ctx);
        return false;
    }
    return sha256_final_hex(&ctx, out_hex);
}
