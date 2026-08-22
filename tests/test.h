/* Tiny assertion-counting test harness. Each test file exposes one
 * test_<area>(void) entry; test_main.c runs them and exits non-zero on any
 * failure. Descriptive check messages double as documentation. */
#ifndef TOKITOKI_TEST_H
#define TOKITOKI_TEST_H

#include <stdio.h>

extern int g_test_failures;
extern int g_test_checks;

#define TEST_CHECK(cond)                                                     \
    do {                                                                     \
        g_test_checks++;                                                     \
        if (!(cond)) {                                                       \
            g_test_failures++;                                               \
            printf("FAIL %s:%d: %s\n", __FILE__, __LINE__, #cond);           \
        }                                                                    \
    } while (0)

#define TEST_CHECK_STR_EQ(actual, expected)                                  \
    do {                                                                     \
        g_test_checks++;                                                     \
        const char *a_ = (actual);                                           \
        const char *e_ = (expected);                                         \
        if (!a_ || strcmp(a_, e_) != 0) {                                    \
            g_test_failures++;                                               \
            printf("FAIL %s:%d: got \"%s\", expected \"%s\"\n", __FILE__,    \
                   __LINE__, a_ ? a_ : "(null)", e_);                        \
        }                                                                    \
    } while (0)

void test_buf(void);
void test_wstr(void);
void test_json(void);
void test_inflate(void);
void test_sha256(void);
void test_version(void);
void test_settings(void);
void test_data_dirs(void);
void test_agent_cli(void);
void test_app_update(void);
void test_logo(void);

#endif
