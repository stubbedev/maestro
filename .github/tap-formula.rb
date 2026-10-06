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

  depends_on "php"

  def install
    bin.install asset => "maestro"
    chmod 0555, bin/"maestro"
  end

  def caveats
    <<~EOS
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
