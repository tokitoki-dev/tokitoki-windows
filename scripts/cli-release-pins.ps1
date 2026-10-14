# Reviewed CLI release bundled into CI and production Windows builds.
# The pin and both digests are reviewed together; changing the CLI an app
# release bundles must be a visible repository change through main.
$TokitokiCliTag = "v0.1.11"
$TokitokiCliSha256 = @{
    amd64 = "e702c0e95c215c8019072d58a1c7ab62f3633ce4a977a61bad28384ee2e741fe"
    arm64 = "47090c962ca20fd23e79e0999160942220ad27cfbad8ea52bb22fbc5c36c5989"
}
