# releasebot

Releasebot is a Go CLI that automates release workflows: run justfile recipes, validate release tags, and generate or update `CHANGELOG.md` using an LLM (or a simple template), with data from GitHub PRs or the git commit log.

## Workflow

1. **Checkout** your repository (or run from inside it).
2. **Config**: releasebot looks for `.releasebot.yml` in the repo root (or `--config`).
3. **Validate** the previous release tag in the git repository (`--prev-tag` or `previous_release_tag` in config).
4. **Justfile** (optional): run configured `just` recipe targets in order. Requires the [just](https://just.systems/) binary on PATH when using this feature.
5. **Changelog**: generate a new section for the release using:
   - **GitHub** (if `github.enabled`): merged PRs between the previous tag and `--head` (default `HEAD`). Results are cached in `.releasebot/cache/` by ref range so repeated runs for the same range skip the API.
   - **Otherwise**: git commit log between the same refs.
   - **LLM** (if configured): OpenAI, Ollama, or Anthropic to format the changelog; otherwise a simple template is used.

## Build and Installation

### Using go install

```bash
go install github.com/johnewart/releasebot@latest
```

This installs the `releasebot` binary to `$GOPATH/bin` (typically `$HOME/go/bin`). Make sure this directory is in your `PATH`.

### Building from source

```bash
git clone https://github.com/johnewart/releasebot
cd releasebot
go build -o releasebot .
```

This creates a `./releasebot` binary in the current directory. You can move it to a directory in your `PATH` or run it directly with `./releasebot`.

## Release Workflows

Releasebot supports two release workflows: a **step-by-step manual approach** for fine-grained control, and an **all-in-one automated command** for a streamlined release process.

### Step-by-Step (Manual)

For maximum control, you can execute each release step individually:

#### 1. Create and push tags

```bash
# Get the next tag (patch version by default)
releasebot tag next

# Create the tag locally
releasebot tag next --create

# Push branch and tag to remote
git push origin main
git push origin <tag>
```

Use `--release` for minor version bump, `--release --major` for major version, `--rc` for release candidates, or `--alpha` for alpha releases.

#### 2. Generate changelog

```bash
# Generate changelog between tags
releasebot changelog --prev-tag <previous-tag> --head <new-tag>

# Commit and push the changelog
git add CHANGELOG.md
git commit -m "changelog: release <tag>"
git push origin main
```

#### 3. Watch for GitHub Actions

```bash
# Watch until all workflows complete
releasebot actions watch --tag <tag>

# Or check status at any time
releasebot actions status --tag <tag>

# Or list all workflow runs
releasebot actions list --tag <tag>
```

#### 4. Watch for PyPI package (if applicable)

```bash
# Check if package is available on PyPI
releasebot pypi check --package <name> --version <version>

# Or watch until available (with timeout)
releasebot pypi watch --package <name> --version <version>
```

#### 5. Watch for Docker Hub image (if applicable)

```bash
# Check if image is available on Docker Hub
releasebot dockerhub check --image <org/name:tag>

# Or watch until available (with timeout)
releasebot dockerhub watch --image <org/name:tag>
```

### All-in-One (Automated)

The `release` command automates the entire release workflow in a single step:

```bash
# Full release (patch version by default)
releasebot release

# Specify version type
releasebot release --release           # minor bump (e.g., v1.2.0 → v1.3.0)
releasebot release --release --major   # major bump (e.g., v1.2.0 → v2.0.0)
releasebot release --rc                # release candidate (e.g., v1.2.0 → v1.2.1rc0)
releasebot release --alpha             # alpha release (e.g., v1.2.0 → v1.2.1a0)

# Optional flags
releasebot release --prev-tag v1.2.3   # specify previous tag explicitly
releasebot release --branch main       # specify branch to release from
releasebot release --confirm           # prompt before each step
releasebot release --no-tui            # disable TUI, use plain output
releasebot release --dry-run           # preview what would be done
```

The `release` command automatically executes these steps:

1. Runs justfile targets (if configured in `.releasebot.yml`)
2. Generates changelog from PRs or commits between tags
3. Commits the changelog
4. Creates and pushes the release tag
5. Pushes the branch to remote
6. Watches for GitHub Actions workflows to complete
7. Watches for PyPI package availability (if `release.pypi_package` is configured)
8. Watches for Docker Hub image availability (if `release.docker_image` is configured)

The command uses an interactive TUI by default when run in a terminal. Use `--no-tui` for plain text output, or `--confirm` to pause and prompt before each step.

## Usage

The `run` command generates or updates the changelog without creating tags or pushing to remote:

```bash
# From inside the repo (uses .releasebot.yml)
releasebot run --prev-tag v1.0.0

# Custom repo path and head ref
releasebot run --repo /path/to/repo --prev-tag v1.0.0 --head main

# Config can set previous_release_tag so you can omit --prev-tag
releasebot run
```

### GitHub Actions (list, status, watch)

List, watch for, or show status of workflow runs triggered for a specific tag (e.g. after pushing a release tag). Requires `GITHUB_TOKEN` and a repo with remote origin (or `github` config).

```bash
# List all workflow runs for a tag
releasebot actions list --tag v1.0.0

# One-line status summary (e.g. "3 runs: 2 success, 1 in progress")
releasebot actions status --tag v1.0.0

# Watch until all runs complete (exit 0 if all success, 1 if any failed or timeout)
releasebot actions watch --tag v1.0.0
releasebot actions watch --tag v1.0.0 --timeout 1h --poll-interval 30s
```

### Environment

- **`OPENAI_API_KEY`** – Required when using OpenAI as the LLM provider. When set and no `llm` config is present, releasebot uses OpenAI with default model `gpt-4o-mini`.
- **`OPENAI_BASE_URL`** – Optional; override API base URL for OpenAI.
- **`ANTHROPIC_API_KEY`** – Required when using Anthropic as the LLM provider.
- **`RELEASEBOT_LLM_PROVIDER`** – Override LLM provider: `openai`, `ollama`, or `anthropic`.
- **`OLLAMA_HOST`** – When using Ollama, optional host (e.g. `localhost:11434`); default is `http://localhost:11434/v1`.
- **`GITHUB_TOKEN`** – Used when `github.enabled` is true (for listing PRs) and for the `actions` command (list/watch/status). Can also be set in `.releasebot.yml` as `github.token`.

## Configuration (`.releasebot.yml`)

| Key | Description |
|-----|-------------|
| `previous_release_tag` | Default previous tag (overridden by `--prev-tag`) |
| `justfile.targets` | List of just recipe names to run in order |
| `justfile.working_dir` | Directory containing the justfile (default: repo root) |
| `changelog.output` | Output file path (default: `CHANGELOG.md`) |
| `changelog.format` | Format string for each entry (hint for LLM or simple mode) |
| `changelog.format_file` | Path to a file containing the format template |
| `changelog.llm.provider` | `openai`, `ollama`, or `anthropic` to use an LLM for the changelog section |
| `changelog.llm.model` | Model name (e.g. `gpt-4o-mini`, `llama3.2`, `claude-sonnet-4-5-20250929`) |
| `changelog.llm.base_url` | Optional API base URL (Ollama default: `http://localhost:11434/v1`) |
| `changelog.llm.summarize_per_pr` | If true, analyze each PR independently (LLM→JSON per PR), then build changelog from JSON; if false, feed LLM all PRs at once (more context, may be slower) |
| `changelog.llm.include_diff` | When `summarize_per_pr` is true, pass the PR diff to the LLM (metadata only vs metadata+diff) |
| `changelog.llm.cache_llm_summaries` | When `summarize_per_pr` is true, cache each PR's JSON in `.releasebot/cache/llm_pr/` (default: true) |
| `changelog.template` | Go text/template for the final changelog section when using `summarize_per_pr` (multiline YAML with `\|`) |
| `changelog.template_file` | Path to a file containing the changelog writer template (overrides `template`) |
| `github.enabled` | If true, use GitHub API for merged PRs between tags |
| `github.token` | GitHub token (or use `GITHUB_TOKEN`) |
| `github.owner` / `github.repo` | Override repo (default: from `git remote origin`) |
| `release.remote` | Git remote to push to for the `release` command (default: `origin`) |
| `release.pypi_package` | PyPI package name; if set, `release` command watches for package availability on PyPI |
| `release.docker_image` | Docker image name (e.g., `myorg/myimage`); if set, `release` command watches for image availability on Docker Hub |

See [.releasebot.yml.example](.releasebot.yml.example) for a full example.

## LLM providers (OpenAI, Ollama, Anthropic)

Changelog sections can be generated by an LLM using one of the following providers:

- **OpenAI** – Set `OPENAI_API_KEY` or configure `changelog.llm.provider: openai` in `.releasebot.yml`. Uses the OpenAI API (or another provider via `OPENAI_BASE_URL`).
- **Ollama** – Configure `changelog.llm.provider: ollama` in `.releasebot.yml`, or set `RELEASEBOT_LLM_PROVIDER=ollama`. Uses the [official Ollama Go SDK](https://pkg.go.dev/github.com/ollama/ollama/api) and the `/api/generate` endpoint. No API key needed; default host is `http://localhost:11434` (override with `OLLAMA_HOST`). Example model: `llama3.2`.
- **Anthropic** – Set `ANTHROPIC_API_KEY` and configure `changelog.llm.provider: anthropic` in `.releasebot.yml`, or set `RELEASEBOT_LLM_PROVIDER=anthropic`. Uses the [Anthropic Messages API](https://docs.anthropic.com/en/api/messages). Example model: `claude-sonnet-4-5-20250929`. Optional `changelog.llm.base_url` for a custom endpoint.

If no LLM is configured and `OPENAI_API_KEY` is not set, releasebot uses a simple template instead of an LLM.

**Per-PR summarization** (`changelog.llm.summarize_per_pr: true`): each PR is analyzed independently (one LLM call per PR) to produce **JSON** (change_type, description, pr_id), which is cached to a file. Then the LLM is called once with these summarized records (not raw PRs or diffs) to generate the final changelog. When `summarize_per_pr` is false, the LLM receives all raw PRs in a single call to generate the changelog (more context, possibly slower). Per-PR JSON format:
```json
{"change_type": "Added|Changed|Developer Experience|Deprecated|Docs|Removed|Fixed|Security", "description": "Description of the change", "pr_id": 12345}
```
Results are cached under `.releasebot/cache/llm_pr/`. You can pass only PR metadata or also the PR diff (`include_diff: true`).

**Changelog structure/template**: When using per-PR summarization, the final changelog is generated by the LLM from the summarized records. The **template** (from `changelog.template_file` or `changelog.template`) is passed to the LLM as the desired structure/format for the output (e.g. version heading, sections by change type, entry format with description and PR link).

### Example Changelog Template

When using an LLM to generate changelogs, the template provides instructions and examples for the desired output format. Here's a practical example:

**Template file (`.releasebot/changelog-template.txt`):**

```
<instructions>
The changelog should look like this - for each type of change and the list of changes of that type, there should be a Markdown section like this:

### Type of change
- Description of change [#PR number](PR URL)

### Other type of change
- Description of change [#PR number](PR URL)

Use the below as an EXAMPLE of the output, do not include it in your output.
</instructions>

### Added
- Monitor scoped activity tab for datastore monitors in the action center [#7162](https://github.com/ethyca/fides/pull/7162)
- Added a monitor type filter to the root action center [#7186](https://github.com/ethyca/fides/pull/7186)

### Changed
- Updated FE copy for our bulk ignore modal, the schema explorer empty state, and the failed action message/toast [#7185](https://github.com/ethyca/fides/pull/7185)

### Developer Experience
- Migrated consent reporting tables from Fides V2 table to Ant Design table components [#7187](https://github.com/ethyca/fides/pull/7187)
```

The LLM uses these instructions and examples to generate changelogs with consistent formatting. The template can include:
- Instructions in `<instructions>` tags explaining the desired format
- Example sections showing the markdown structure
- Example entries showing how to format descriptions and PR links

Configure the template in `.releasebot.yml`:

```yaml
changelog:
  llm:
    provider: ollama  # or openai, anthropic
    model: qwen3:4b
    summarize_per_pr: true
  template_file: .releasebot/changelog-template.txt
```

Alternatively, for more advanced use cases with `summarize_per_pr: true`, you can use a Go `text/template` to structure the LLM's per-PR summaries. See `changelog-template.example.tmpl` for an example with variables like `{{.Version}}` and `{{.Sections}}`.

## Justfile integration

Releasebot does not embed a justfile parser. When you configure `justfile.targets`, it runs the [just](https://github.com/casey/just) binary (e.g. `just test`, `just build`) in the repo. So **`just` must be installed and on PATH** when using that feature. All other behavior (git, GitHub API, changelog generation) is self-contained.

## Known Limitations

### Docker Hub Private Registries

Currently, watching for images in private Docker Hub registries is not fully supported. The `release` command and `dockerhub watch/check` commands work correctly with public Docker Hub repositories. Support for private registries (requiring authentication) is planned for a future release.

## License

MIT
