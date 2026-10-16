class Pubport < Formula
  desc "Publish a local port to a stable URL on your own domain via Cloudflare tunnels"
  homepage "https://github.com/reallongnguyen/pubport"
  url "https://github.com/reallongnguyen/pubport/archive/refs/tags/v0.1.0.tar.gz"
  sha256 "f3f59d9c0a3101c1376b5487f3f7fa18d110446a1e68ddfe3d422f642f32144e"
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
