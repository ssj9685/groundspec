# Getting started

GroundSpec turns source evidence into reviewed specifications, implementation work, and current verification evidence. It never approves AI output or publishes repository changes automatically.

## 1. Install

### macOS or Linux

Copy and paste the complete block into Bash or Zsh:

```shell
go install github.com/ssj9685/groundspec/cmd/groundspec@latest

groundspec_bin_dir="$(go env GOBIN)"
if [ -z "$groundspec_bin_dir" ]; then
  groundspec_bin_dir="$(go env GOPATH)/bin"
fi
export PATH="$groundspec_bin_dir:$PATH"

groundspec --version
groundspec --help
```

Go installs commands into `GOBIN`, or into `GOPATH/bin` when `GOBIN` is empty. The block adds that directory to the current terminal before invoking GroundSpec. A successful installation prints `groundspec v0.1.0` or a newer version.

To keep the command available in new Zsh terminals, run this block once:

```shell
groundspec_bin_dir="$(go env GOBIN)"
if [ -z "$groundspec_bin_dir" ]; then
  groundspec_bin_dir="$(go env GOPATH)/bin"
fi
printf '\nexport PATH="%s:$PATH"\n' "$groundspec_bin_dir" >> ~/.zshrc
source ~/.zshrc
```

### Windows PowerShell

Copy and paste the complete block into PowerShell:

```powershell
go install github.com/ssj9685/groundspec/cmd/groundspec@latest

$groundspecBinDir = go env GOBIN
if (-not $groundspecBinDir) {
  $groundspecBinDir = Join-Path (go env GOPATH) "bin"
}
$env:Path = "$groundspecBinDir;$env:Path"

groundspec --version
groundspec --help
```

The PATH update applies to the current PowerShell session.

### Connect an agent

After `groundspec --help` succeeds:

```shell
codex login
groundspec adapters
```

Use a prebuilt archive from GitHub Releases when a Go toolchain is unavailable. A release binary is self-contained; only text-bearing PDF intake needs a separate `pdftotext` executable.

## 2. Initialize a project

For an existing repository:

```shell
cd your-project
groundspec init ./requirements.md --adapter codex-cli
```

`init` performs all reserved-path checks before writing. It preserves existing files and creates the source copy, proposal, and review record under `.groundspec/`. It refuses to continue when `.groundspec`, `SPEC.md`, `TEST_SPEC.md`, `DESIGN.md`, or `IMPLEMENTATION_PLAN.md` already exists.

For a new empty workspace:

```shell
groundspec start ./requirements.html --output ./workspace --adapter codex-cli
cd ./workspace
```

## 3. Review requirements

```shell
groundspec status .groundspec/proposal.json
groundspec review .groundspec/proposal.json --accept PB-01 --note "Confirmed from source"
```

Repeat for every candidate and resolve every question. GroundSpec retains decisions for unchanged candidates and blocks new or changed candidates.

## 4. Preserve an existing architecture

Before materialization, an existing project may add `.groundspec/project-context.json`:

```json
{
  "version": 1,
  "project": "your-project",
  "language": "TypeScript",
  "framework": "React",
  "sourceDirectory": ".",
  "constraints": [
    "Preserve the current package manager and test framework.",
    "Do not replace the existing application architecture."
  ]
}
```

The validator rejects a generated plan that silently changes these reviewed invariants.

## 5. Materialize, implement, and verify

```shell
groundspec materialize .groundspec/proposal.json --adapter codex-cli
groundspec implement .groundspec/development-plan.json --adapter codex-cli
groundspec verify .groundspec/development-plan.json
groundspec lifecycle
```

`lifecycle` reports `complete` only when proposal, review, plan, implementation, required verification commands, and declared evidence are current. A targeted test or AI completion message alone is never terminal evidence.
