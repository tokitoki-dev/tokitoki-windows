# Reviewed CLI release bundled into CI and production Windows builds.
# The pin and both digests are reviewed together; changing the CLI an app
# release bundles must be a visible repository change through main.
$TokitokiCliTag = "v0.1.9"
$TokitokiCliSha256 = @{
    amd64 = "f180ca79f0390a9675acbb9ecbd7a2e8dafd6b19e9f3921a9fd354083c6d9820"
    arm64 = "655784938505d7ebd4e0378f0dae6b16e0a3035589a4d748be493972f9f56ef4"
}
