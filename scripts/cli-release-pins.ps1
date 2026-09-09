# Reviewed CLI release bundled into CI and production Windows builds.
# The pin and both digests are reviewed together; changing the CLI an app
# release bundles must be a visible repository change through main.
$TokitokiCliTag = "v0.1.8"
$TokitokiCliSha256 = @{
    amd64 = "4a2f81195e60fd14584494757892dab7a973a1ca134bed86678f5c37cefc11c3"
    arm64 = "564299fdc87b43440c4dd42f12922917b4bccadcd92ce1a061ca374a84a6a604"
}
