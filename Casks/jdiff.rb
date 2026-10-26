cask "jdiff" do
  arch arm: "arm64", intel: "amd64"

  version "0.0.2"
  sha256 arm:   "3e2e03dce8cc7ea736a11b99625a46d2a20270b49c1571423ce2beb22a5e0be7",
         intel: "611cb83610f02019f9a913e74199c36d536c643d2b246a2755a668ebee3e21a1"

  url "https://github.com/ObiWahn/jdiff/releases/download/v#{version}/jdiff-v#{version}-darwin-#{arch}"
  name "jdiff"
  desc "Interactive terminal diff viewer with manual line alignment"
  homepage "https://github.com/ObiWahn/jdiff"

  depends_on macos: :ventura
  container type: :naked
  binary "jdiff-v#{version}-darwin-#{arch}", target: "jdiff"

  # The existing release contains bare, unsigned binaries. GoReleaser replaces
  # this cask with archive downloads on the next upstream release.
  postflight_steps do
    run "/usr/bin/xattr", args: ["-dr", "com.apple.quarantine", "{{staged_path}}/jdiff-v#{version}-darwin-#{arch}"]
  end

  # No zap stanza required
end
