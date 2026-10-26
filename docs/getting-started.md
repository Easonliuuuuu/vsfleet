# Getting started

Install vsfleet, try the sample estate, then connect your first vCenter.
Use a read-only vSphere account for live inventory.

## Release status

vsfleet is in alpha and has been used against real vCenter 8.0.3 labs. Its
read-only guarantee holds in every release. Until 1.0, minor releases may
change command flags, JSON output fields, export columns, and the snapshot
database schema. Breaking changes are called out in the
[changelog](https://github.com/Easonliuuuuu/vsfleet/blob/main/CHANGELOG.md);
pin a version for automation.

## Install

### Homebrew (macOS and Linux)

```sh
brew install easonliuuuuu/tap/vsfleet
```

### Windows (WinGet & Scoop)

```powershell
# WinGet (Windows 10/11)
winget install --id Easonliuuuuu.vsfleet --exact

# Scoop
scoop bucket add easonliuuuuu https://github.com/Easonliuuuuu/homebrew-tap
scoop install vsfleet
```

### Linux packages (Debian, Ubuntu, RHEL, Fedora)

Download native `.deb` or `.rpm` packages from
[GitHub Releases](https://github.com/Easonliuuuuu/vsfleet/releases), then run
the matching command in the directory containing the downloaded package.
These examples use `amd64` for Intel/AMD 64-bit CPUs; on ARM64, download the
`arm64` package and replace `amd64` in the command:

```sh
# Debian / Ubuntu
sudo dpkg -i vsfleet_*_linux_amd64.deb

# RHEL / CentOS / Fedora
sudo rpm -i vsfleet_*_linux_amd64.rpm
```

### Pre-built release binary

Download a release archive for your operating system and CPU architecture from
[GitHub Releases](https://github.com/Easonliuuuuu/vsfleet/releases).

```sh
# Example for Linux x86_64
release_url=$(curl -fsSL -o /dev/null -w '%{url_effective}' \
  https://github.com/Easonliuuuuu/vsfleet/releases/latest)
version=${release_url##*/v}
curl -fL -o vsfleet.tar.gz \
  "https://github.com/Easonliuuuuu/vsfleet/releases/download/v${version}/vsfleet_${version}_linux_amd64.tar.gz"
tar -xzf vsfleet.tar.gz vsfleet
sudo install -m 0755 vsfleet /usr/local/bin/
```

Archives are available for Linux, macOS (Apple Silicon and Intel), and Windows
(amd64 and arm64).

### Go installation

Requires Go 1.26 or newer:

```sh
go install github.com/easonliuuuuu/vsfleet/cmd/vsfleet@latest
```

### Container (automation)

The official GitHub Container Registry image supports Linux amd64 and arm64:

```sh
docker run --rm ghcr.io/easonliuuuuu/vsfleet:latest explain rvtools.vInfo -o json
```

Use a version tag such as <!-- x-release-please-start-version -->`v0.6.1`<!-- x-release-please-end --> for repeatable deployments, or pin the
image digest in production. The image runs as an unprivileged user for
unattended commands, assessments, and exports.

Install the native binary for the interactive terminal UI and workstation
handoffs. The image has no shell, browser, SSH client, OS keyring, or `exec:`
credential helper. See the [Containers guide](containers.md) for mounts and secrets.

### Build from source

Requires Go 1.26 or newer. On Linux or macOS:

```sh
git clone https://github.com/Easonliuuuuu/vsfleet.git
cd vsfleet
go build -o vsfleet ./cmd/vsfleet
sudo install -m 0755 vsfleet /usr/local/bin/
```

On Windows, build with `go build -o vsfleet.exe ./cmd/vsfleet`, then run
`.\vsfleet.exe` from the repository directory or put it in a directory on
your `PATH`.

## Verify installation

Open a terminal and check that the installed binary is on your `PATH`:

```sh
vsfleet --version
vsfleet --help
```

If the command is not found, see [installation and PATH fixes](troubleshooting.md#vsfleet-is-not-found).

## Look around first

Try the interface with sample data before configuring it:

```sh
vsfleet demo
```

Press `?` for help and `q` to quit. The demo is a terminal UI; CLI inventory
commands query your configured vCenters.

The synthetic demo has three vCenter sites: two healthy sites with different
routes, and a disaster-recovery site whose proxy refuses the connection.
The main site has about 1,000 VMs across six clusters, 36 datastores and
24 vApps. History has five dated assessments showing drift, capacity and
snapshot ageing.

The demo reads no configuration, opens no keyring, resolves no credentials,
and makes no network connections or writes. Unlike a live run, it does not
remember the last screen. Every screen is marked `DEMO · SAMPLE DATA`.

## First context

Running `vsfleet` opens the setup wizard if no contexts exist:

```sh
vsfleet
```

Press `Ctrl+C` to exit the setup form without adding a context.

You can also open the wizard with:

```sh
vsfleet config set-context
```

Name the context `prod` to follow the commands below, or replace `prod` with
your chosen name. Enter the vCenter URL and a read-only account. Choose
`direct` for a connection without a proxy; see [network routes](configuration.md#network-routes)
for proxy settings.

Use `system` for a certificate trusted by your OS. For an untrusted lab
certificate, choose `thumbprint` and verify its fingerprint before accepting
it; see [TLS policies](configuration.md#tls-policies). The wizard tests the
connection before saving. With an OS keyring, it prompts
for a password and stores it there.

Without a keyring, such as on a headless server or in an SSH session, the
wizard tells you and asks you to choose a password source: a prompt on every
run, an environment variable, a file, or a helper such as `vault`. See
[without an OS keyring](configuration.md#without-an-os-keyring) and
[unattended sources](configuration.md#unattended-sources).

## Explore inventory

```sh
vsfleet config get-contexts
vsfleet doctor prod
vsfleet cluster-info

vsfleet get vms
vsfleet get vms -n prod -o json
vsfleet get vapps
vsfleet get hosts
vsfleet get datastores
vsfleet describe vm web-01

vsfleet search ubuntu --all-contexts
```

Replace `prod` with your context name. Use `-A` (`--all-contexts`) to query
the estate or `-n NAME` (`--context NAME`) to select one context. See the [CLI reference](commands.md)
for filtering and output, or open the [terminal UI](tui.md) with `vsfleet`.

## Capture your first assessment

```sh
vsfleet assessment run --all-contexts
vsfleet assessment report latest
```

The capture is stored locally; the report reads it offline. Check coverage
before interpreting missing objects. Continue with [Assessments](assessments.md)
for comparisons, [Exports](exports.md) for XLSX/CSV, or
[Planning](planning.md) for migration and sizing.

## Shell completion

Generate completion for Bash, Zsh, Fish, or PowerShell:

```sh
# Bash
mkdir -p ~/.local/share/bash-completion/completions
vsfleet completion bash > ~/.local/share/bash-completion/completions/vsfleet
source ~/.local/share/bash-completion/completions/vsfleet

# Zsh
mkdir -p ~/.zfunc
vsfleet completion zsh > ~/.zfunc/_vsfleet

# Fish
mkdir -p ~/.config/fish/completions
vsfleet completion fish > ~/.config/fish/completions/vsfleet.fish
```

For Bash, the `source` command enables completion in the current session.
Install your distribution's `bash-completion` package for automatic loading
in new sessions.

For Zsh, add these lines to `~/.zshrc`, placing `fpath` before any existing
`compinit` call, then restart the shell:

```sh
fpath=("$HOME/.zfunc" $fpath)
autoload -Uz compinit
compinit
```

For the current PowerShell session:

```powershell
vsfleet completion powershell | Out-String | Invoke-Expression
```

Run `vsfleet completion --help` for command details.

## Upgrading

vsfleet checks for updates in the background at most once a day. When an
update is available, the next interface launch asks whether to upgrade.
Homebrew and `go install` users can upgrade and restart from that prompt.
Scoop, winget, `.deb`/`.rpm` and release archive users get a command or
download link to copy. Other commands print a notice on stderr.

Set `VSFLEET_NO_UPDATE_NOTIFIER=1` to disable the check. It never runs in CI
or the container image. See
[Terminal UI](tui.md#upgrade-prompt) and [SECURITY.md](https://github.com/Easonliuuuuu/vsfleet/blob/main/SECURITY.md)
for what the check sends.
