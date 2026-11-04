# Installation

[Documentation](README.md) · [Quick start](../README.md#quick-start)

One `workledger` binary provides the agent-friendly CLI and an optional TUI. Install it once for direct commands, agent workflows, or interactive use.

## Homebrew

```sh
brew install solitus0/tap/workledger
workledger version
```

To update:

```sh
brew update
brew upgrade workledger
```

## Release archives

Find available platform archives and checksums on [GitHub Releases](https://github.com/solitus0/workledger/releases). Extract the archive for your operating system and architecture, then put `workledger` on your `PATH`.

The release configuration targets macOS, Linux, and Windows on amd64 and arm64. Check the assets of the release you choose for availability.

## Build from source

Use the Go version declared in [go.mod](../go.mod), currently Go 1.26 or newer. Follow the [development setup](development.md#build-and-run) for a checkout, or install the latest module version:

```sh
go install github.com/solitus0/workledger/cmd/workledger@latest
```

Ensure your Go binary directory is on `PATH`.

## Terminal requirements

These display requirements apply to the optional TUI. CLI commands and agent workflows can run without opening it, including with piped JSON input and output where supported.

`workledger tui` needs interactive stdin and stdout and a terminal of at least **100 columns × 28 rows**. A font with Unicode box-drawing characters and a terminal with true color give the clearest presentation.

The TUI chooses its light or dark palette from the terminal background. `NO_COLOR=1 workledger tui` retains the same layout and non-color state indicators. Run the TUI directly in your terminal; use ordinary CLI commands with `--output json` in pipes and automation.

The screenshots use the Ledger W accents and Catppuccin-derived semantic palette. See [capture details](media.md) for their font, size, fixture, and rendering conditions.

## Shell completion

Completion is supported for Bash, Zsh, and Fish. PowerShell completion is unsupported. Generate scripts without initializing storage, and regenerate installed scripts after upgrades that change commands or flags.

<details>
<summary>Zsh</summary>

```zsh
mkdir -p ~/.zfunc
workledger completion zsh > ~/.zfunc/_workledger
```

Add these lines to `~/.zshrc` before any existing `compinit` call:

```zsh
fpath=(~/.zfunc $fpath)
autoload -Uz compinit
compinit
```

</details>

<details>
<summary>Bash</summary>

Load completion for the current session:

```bash
source <(workledger completion bash)
```

Add that line to your Bash startup file to load it in future sessions.

</details>

<details>
<summary>Fish</summary>

```fish
mkdir -p ~/.config/fish/completions
workledger completion fish > ~/.config/fish/completions/workledger.fish
```

</details>

## First launch

Continue with the [quick start](../README.md#quick-start). `workledger init` prepares the local config and database; `workledger status` checks readiness. Configure optional adapters using the [configuration guide](configuration.md), then complete the [initial pull](synchronization.md#first-import) before pushing.
