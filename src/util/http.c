#include "util/http.h"

#include <windows.h>

#include <winhttp.h>

#include <stdlib.h>
#include <string.h>

#pragma comment(lib, "winhttp.lib")

typedef struct Request {
    HINTERNET session;
    HINTERNET connection;
    HINTERNET request;
} Request;

static void request_close(Request *r) {
    if (r->request) {
        WinHttpCloseHandle(r->request);
    }
    if (r->connection) {
        WinHttpCloseHandle(r->connection);
    }
    if (r->session) {
        WinHttpCloseHandle(r->session);
    }
    memset(r, 0, sizeof(*r));
}

/* Opens session/connection/request for `url`. Returns false on bad URL or
 * handle failure. */
static bool request_open(Request *r, const wchar_t *method, const wchar_t *url,
                         unsigned timeout_ms, const wchar_t *user_agent) {
    memset(r, 0, sizeof(*r));

    URL_COMPONENTS parts;
    memset(&parts, 0, sizeof(parts));
    parts.dwStructSize = sizeof(parts);
    wchar_t host[256];
    wchar_t path[2048];
    wchar_t extra[2048];
    parts.lpszHostName = host;
    parts.dwHostNameLength = ARRAYSIZE(host);
    parts.lpszUrlPath = path;
    parts.dwUrlPathLength = ARRAYSIZE(path);
    /* The query string is "extra info": components not requested are NOT
     * returned, so omitting this would silently strip ?query=... from every
     * request. */
    parts.lpszExtraInfo = extra;
    parts.dwExtraInfoLength = ARRAYSIZE(extra);
    if (!WinHttpCrackUrl(url, 0, 0, &parts)) {
        return false;
    }
    bool secure = parts.nScheme == INTERNET_SCHEME_HTTPS;
    wchar_t full_path[4096];
    _snwprintf_s(full_path, ARRAYSIZE(full_path), _TRUNCATE, L"%s%s", path, extra);

    r->session = WinHttpOpen(user_agent, WINHTTP_ACCESS_TYPE_AUTOMATIC_PROXY,
                             WINHTTP_NO_PROXY_NAME, WINHTTP_NO_PROXY_BYPASS, 0);
    if (!r->session) {
        return false;
    }
    int timeout = (int)timeout_ms;
    WinHttpSetTimeouts(r->session, timeout, timeout, timeout, timeout);

    r->connection = WinHttpConnect(r->session, host, parts.nPort, 0);
    if (!r->connection) {
        request_close(r);
        return false;
    }
    r->request = WinHttpOpenRequest(r->connection, method, full_path, NULL,
                                    WINHTTP_NO_REFERER,
                                    WINHTTP_DEFAULT_ACCEPT_TYPES,
                                    secure ? WINHTTP_FLAG_SECURE : 0);
    if (!r->request) {
        request_close(r);
        return false;
    }
    return true;
}

static bool request_send(Request *r, const HttpHeader *headers, size_t header_count) {
    for (size_t i = 0; i < header_count; i++) {
        wchar_t line[1024];
        int written = _snwprintf_s(line, ARRAYSIZE(line), _TRUNCATE, L"%s: %s",
                                   headers[i].name, headers[i].value);
        if (written < 0 ||
            !WinHttpAddRequestHeaders(r->request, line, (DWORD)-1,
                                      WINHTTP_ADDREQ_FLAG_ADD)) {
            return false;
        }
    }
    if (!WinHttpSendRequest(r->request, WINHTTP_NO_ADDITIONAL_HEADERS, 0,
                            WINHTTP_NO_REQUEST_DATA, 0, 0, 0)) {
        return false;
    }
    return WinHttpReceiveResponse(r->request, NULL) != FALSE;
}

static unsigned request_status(Request *r) {
    DWORD status = 0;
    DWORD size = sizeof(status);
    if (!WinHttpQueryHeaders(r->request,
                             WINHTTP_QUERY_STATUS_CODE | WINHTTP_QUERY_FLAG_NUMBER,
                             WINHTTP_HEADER_NAME_BY_INDEX, &status, &size,
                             WINHTTP_NO_HEADER_INDEX)) {
        return 0;
    }
    return status;
}

static bool request_read(Request *r, HttpSink sink, void *sink_ctx) {
    uint8_t chunk[64 * 1024];
    for (;;) {
        DWORD read = 0;
        if (!WinHttpReadData(r->request, chunk, sizeof(chunk), &read)) {
            return false;
        }
        if (read == 0) {
            return true;
        }
        if (!sink(sink_ctx, chunk, read)) {
            return false;
        }
    }
}

static bool buf_sink(void *ctx, const uint8_t *chunk, size_t len) {
    return buf_push((Buf *)ctx, chunk, len);
}

bool http_request(const wchar_t *method, const wchar_t *url,
                  const HttpHeader *headers, size_t header_count,
                  unsigned timeout_ms, const wchar_t *user_agent,
                  HttpResponse *out) {
    out->status = 0;
    buf_init(&out->body);

    Request r;
    if (!request_open(&r, method, url, timeout_ms, user_agent)) {
        return false;
    }
    bool ok = request_send(&r, headers, header_count);
    if (ok) {
        out->status = request_status(&r);
        ok = request_read(&r, buf_sink, &out->body);
    }
    request_close(&r);
    if (ok) {
        /* NUL-terminate so the body doubles as a C string for JSON. */
        ok = buf_push_byte(&out->body, 0);
        if (ok) {
            out->body.len--; /* the NUL is not part of the payload */
        }
    }
    return ok;
}

bool http_download(const wchar_t *url, unsigned timeout_ms,
                   const wchar_t *user_agent, HttpSink sink, void *sink_ctx,
                   unsigned *out_status) {
    *out_status = 0;
    Request r;
    if (!request_open(&r, L"GET", url, timeout_ms, user_agent)) {
        return false;
    }
    bool ok = request_send(&r, NULL, 0);
    if (ok) {
        *out_status = request_status(&r);
        ok = (*out_status == 200) && request_read(&r, sink, sink_ctx);
    }
    request_close(&r);
    return ok;
}

void http_response_free(HttpResponse *r) {
    buf_free(&r->body);
    r->status = 0;
}
