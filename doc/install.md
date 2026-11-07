# Install

## macOS

On Apple Silicon or Intel, install the upstream Homebrew cask:

```sh
brew tap ObiWahn/jdiff https://github.com/ObiWahn/jdiff.git
brew install --cask obiwahn/jdiff/jdiff
```

To update it, run `brew update && brew upgrade --cask obiwahn/jdiff/jdiff`.
The cask installs a prebuilt binary; Go is not needed.

## Linux

Download the package for your distribution from the [upstream releases](https://github.com/ObiWahn/jdiff/releases) and install it; it pulls in git:

```sh
sudo apt install ./jdiff_<version>_amd64.deb               # Debian, Ubuntu
sudo dnf install ./jdiff-<version>-1.x86_64.rpm             # Fedora, RHEL
sudo apk add --allow-untrusted ./jdiff_<version>_x86_64.apk # Alpine
sudo pacman -U ./jdiff-<version>-1-x86_64.pkg.tar.zst       # Arch
```

## Windows

Install it with Scoop:

```sh
scoop bucket add jdiff https://github.com/ObiWahn/jdiff
scoop install jdiff
```

## Archives and bare binaries

The [upstream releases](https://github.com/ObiWahn/jdiff/releases) have an archive and a bare binary for Linux, macOS and Windows.
Extract the archive, or rename the binary, and put `jdiff` (or `jdiff.exe`) on your `PATH`.
On Linux and macOS, make a bare binary executable with `chmod +x`.

## From source

jdiff needs Go 1.24 or newer:

```sh
make build        # builds bin/jdiff
go install .      # or install it into $GOBIN
```
