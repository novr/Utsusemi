# Local development formula (source build).
# Distribution: brew tap novr/taps && brew install utsusemi
class Utsusemi < Formula
  desc "Ephemeral self-hosted GitHub Actions runners on Apple Silicon Macs"
  homepage "https://github.com/novr/utsusemi"
  version "0.1.0"
  license "MIT"

  depends_on "go" => :build
  depends_on "tart"

  def install
    system "go", "build", "-o", bin/"utsusemi", "./cmd/utsusemi"
    (etc/"utsusemi").mkpath
  end

  def post_install
    config = etc/"utsusemi/config.yaml"
    return if config.exist?

    config.write <<~YAML
      target:
        repo: owner/repo
      labels: [self-hosted, macOS, tart, arm64]
      registration:
        mode: github_pat
      provider: tart
      base_image: ghcr.io/cirruslabs/macos-sequoia-base:latest
      runner_version: "2.336.0"
      pool_size: 1
    YAML
  end

  service do
    run [opt_bin/"utsusemi", "run", "--config", etc/"utsusemi/config.yaml"]
    keep_alive true
    log_path var/"log/utsusemi.log"
    error_log_path var/"log/utsusemi.error.log"
    environment_variables PATH: std_service_path_env
  end

  test do
    assert_match "Ephemeral", shell_output("#{bin}/utsusemi --help")
  end
end
