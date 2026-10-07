class Maestro < Formula
  desc "Composer, natively: a drop-in replacement for the composer command"
  homepage "https://github.com/stubbedev/maestro"
  version "@@VERSION@@"
  license "MIT"

  on_macos do
    if Hardware::CPU.intel?
      url "@@BASE_URL@@/maestro_darwin_amd64"
      sha256 "@@SHA_DARWIN_AMD64@@"
    else
      url "@@BASE_URL@@/maestro_darwin_arm64"
      sha256 "@@SHA_DARWIN_ARM64@@"
    end
  end

  on_linux do
    if Hardware::CPU.intel?
      url "@@BASE_URL@@/maestro_linux_amd64"
      sha256 "@@SHA_LINUX_AMD64@@"
    else
      url "@@BASE_URL@@/maestro_linux_arm64"
      sha256 "@@SHA_LINUX_ARM64@@"
    end
  end

  # maestro runs PHP code (platform detection, plugins, scripts) with the
  # php first on PATH, whichever installed it, so Homebrew's php is only
  # an option: forcing it builds php's whole tree where no bottle fits
  # (an Intel brew on Apple Silicon) for users who already have a php.
  depends_on "php" => :optional

  def install
    bin.install asset => "maestro"
    chmod 0555, bin/"maestro"
  end

  def caveats
    <<~EOS
      maestro runs PHP code with the php first on your PATH. If you have
      none, install one (`brew install php`, or reinstall maestro with
      `--with-php`).

      To use maestro as composer:
        ln -s "#{opt_bin}/maestro" "$(brew --prefix)/bin/composer"
    EOS
  end

  def test
    assert_match "maestro version #{version}", shell_output("#{bin}/maestro --version 2>&1")
  end

  def asset
    os = OS.mac? ? "darwin" : "linux"
    arch = Hardware::CPU.intel? ? "amd64" : "arm64"
    "maestro_#{os}_#{arch}"
  end
end
