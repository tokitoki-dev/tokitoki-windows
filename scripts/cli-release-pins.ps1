# Reviewed CLI release bundled into CI and production Windows builds.
# The pin and both digests are reviewed together; changing the CLI an app
# release bundles must be a visible repository change through main.
$TokitokiCliTag = "v0.1.6"
$TokitokiCliSha256 = @{
    amd64 = "245e24f3e016c46f621858c912fe8b13b53d6f1aa16af6e054487c8fa8da10ab"
    arm64 = "72bf62d1dd6eba236ce7f5c050d008f33f2abdbd9e21511a6fa40d8d513ce5f9"
}
