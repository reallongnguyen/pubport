class Pubport < Formula
  desc "Publish a local port to a stable URL on your own domain via Cloudflare tunnels"
  homepage "https://github.com/reallongnguyen/pubport"
  url "https://github.com/reallongnguyen/pubport/archive/refs/tags/v0.1.1.tar.gz"
  sha256 "b356c134a0432c453034bcc83f1e117ef9baa2944a7d5fe6ce6438b2a4584070"
  head "https://github.com/reallongnguyen/pubport.git", branch: "main"

  depends_on "go" => :build
  depends_on "cloudflared"

  def install
    ldflags = "-s -w -X github.com/reallongnguyen/pubport/internal/app.Version=#{version}"
    system "go", "build", *std_go_args(ldflags: ldflags), "./cmd/pubport"
  end

  test do
    assert_match "pubport #{version}", shell_output("#{bin}/pubport version")
  end
end
