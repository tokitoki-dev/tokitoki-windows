#include "test.h"

int g_test_failures = 0;
int g_test_checks = 0;

static void run(const char *name, void (*fn)(void)) {
    int before = g_test_failures;
    fn();
    printf("%-12s %s\n", name, g_test_failures == before ? "ok" : "FAILED");
}

int main(void) {
    run("buf", test_buf);
    run("wstr", test_wstr);
    run("json", test_json);
    run("sha256", test_sha256);
    run("inflate", test_inflate);
    run("version", test_version);
    run("settings", test_settings);
    run("data_dirs", test_data_dirs);
    run("agent_cli", test_agent_cli);
    run("app_update", test_app_update);
    run("logo", test_logo);

    printf("\n%d checks, %d failures\n", g_test_checks, g_test_failures);
    return g_test_failures == 0 ? 0 : 1;
}
