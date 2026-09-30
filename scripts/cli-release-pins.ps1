# Reviewed CLI release bundled into CI and production Windows builds.
# The pin and both digests are reviewed together; changing the CLI an app
# release bundles must be a visible repository change through main.
$TokitokiCliTag = "v0.1.10"
$TokitokiCliSha256 = @{
    amd64 = "b3750334ba03c483bd48df6ffec606023dba14bcbad5e12c77820baa39d49bad"
    arm64 = "76b2561cae0260d15d9630f35c80c76d3963a4ccfced7aa26121b919ff0a992d"
}
