/* Thin WinHTTP wrapper: TLS comes from schannel, proxy settings from the
 * OS — nothing bundled. All requests are synchronous; callers run them on
 * worker threads. */
#ifndef TOKITOKI_HTTP_H
#define TOKITOKI_HTTP_H

#include "util/buf.h"

#include <stdbool.h>
#include <stdint.h>
#include <wchar.h>

typedef struct HttpHeader {
    const wchar_t *name;
    const wchar_t *value;
} HttpHeader;

typedef struct HttpResponse {
    unsigned status; /* HTTP status code, 0 on transport failure */
    Buf body;
} HttpResponse;

/* Streaming sink for large downloads; return false to abort. */
typedef bool (*HttpSink)(void *ctx, const uint8_t *chunk, size_t len);

/* GET/POST with an in-memory response body. `body`/`body_len` may be
 * NULL/0 (POSTs in this app send empty bodies). Returns false only on
 * transport-level failure; HTTP error statuses come back in `out->status`. */
bool http_request(const wchar_t *method, const wchar_t *url,
                  const HttpHeader *headers, size_t header_count,
                  unsigned timeout_ms, const wchar_t *user_agent,
                  HttpResponse *out);

/* GET streamed through `sink` (for multi-MB downloads). */
bool http_download(const wchar_t *url, unsigned timeout_ms,
                   const wchar_t *user_agent, HttpSink sink, void *sink_ctx,
                   unsigned *out_status);

void http_response_free(HttpResponse *r);

#endif
