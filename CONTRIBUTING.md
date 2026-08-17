# Contributing to Go Media Archive

Thank you for helping improve Go Media Archive. This repository is both a
production-oriented application and a learning project. Contributions should be
small, understandable, tested, and documented.

## Code of conduct

Be respectful, constructive, and patient. Explain the reasoning behind a change,
assume good intent, and help keep discussions welcoming to contributors with
different levels of experience.

## Before contributing

- Search existing issues and pull requests before starting substantial work.
- Open an issue before making a large feature or architecture change.
- Keep each contribution focused on one problem.
- Never add licensed media, personal data, credentials, certificates, private
  keys, production databases, or other sensitive material.
- Use only self-created, synthetic, or appropriately licensed test fixtures.

## Development environment

The project currently uses:

- Go 1.26.6 or newer within the Go 1.26 release line;
- Git;
- Git Bash for documented shell commands;
- LF line endings for all text files;
- VS Code with the official Go extension as the recommended editor setup.

Clone the repository and enter its directory:

```bash
git clone https://github.com/ebe542/go-mediaarchive.git && \
  cd go-mediaarchive
```

Check the installed tools:

```bash
go version && git version
```

## Development workflow

The project follows red-green-refactor development:

1. Add or change a test that describes the intended behavior.
2. Run the test and confirm that it fails for the expected reason.
3. Add the smallest implementation that makes the test pass.
4. Refactor while keeping the tests green.
5. Update the relevant English documentation.
6. Run the milestone checks before committing.

Run the standard quality checks with:

```bash
./scripts/check_milestone.sh
```

Run the additional race detector when the local toolchain supports it:

```bash
./scripts/check_milestone.sh --race
```

The standard check verifies formatting, module files, static analysis, tests,
coverage execution, builds, and Git whitespace.

Before changing a GitHub Actions workflow, validate its syntax and reproduce its
project checks locally:

```bash
./scripts/check_ci.sh
```

This requires `actionlint`. Install it with:

```bash
go install github.com/rhysd/actionlint/cmd/actionlint@latest
```

To emulate the complete GitHub Actions `verify` job, install Docker and `act`,
then run:

```bash
./scripts/check_ci.sh --act
```

Local emulation reduces CI feedback cycles but does not guarantee that GitHub's
hosted environment will behave identically.

## Code guidelines

- Follow idiomatic Go and use `gofmt` formatting.
- Prefer the Go standard library when it provides a clear solution.
- Add third-party dependencies only when their benefit and maintenance cost are
  understood.
- Keep packages focused and use descriptive English names.
- Accept configuration primarily through explicit CLI flags. Environment
  variables may provide defaults, followed by safe built-in defaults.
- Return and wrap errors with useful context.
- Pass `context.Context` through operations that perform I/O or may be canceled.
- Add comments that explain intent, constraints, or non-obvious decisions. Avoid
  comments that merely repeat the code.
- Add GoDoc comments to exported identifiers.
- Do not log secrets, credentials, tokens, private file paths, or licensed media
  content.

## Tests

- Place tests close to the code they verify.
- Prefer testing observable behavior over implementation details.
- Use table-driven tests when several cases share the same behavior.
- Keep tests deterministic and independent of execution order.
- Use temporary directories and isolated databases for integration tests.
- Do not require external services unless the milestone explicitly introduces
  and documents them.
- Cover authorization failures as carefully as successful operations.

Tests should use names that describe behavior, for example:

```go
func TestHealthEndpoint(t *testing.T) {
    // Test implementation.
}
```

## API changes

The public REST API is versioned under `/api/v1`.

- Keep success and error responses consistent JSON unless an endpoint explicitly
  transfers media content.
- Document new endpoints and response fields.
- Add handler or end-to-end tests for externally visible behavior.
- Preserve backward compatibility within an API version.
- Discuss breaking changes before implementation.

## Security and licensed content

The application is intended to manage content that may not be distributed
publicly. Treat metadata and download permissions as security-sensitive.

- Metadata access does not automatically grant content download access.
- New media must remain private by default unless a documented policy says
  otherwise.
- Authentication does not replace per-resource authorization.
- mTLS identifies trusted clients or devices; it does not replace user identity
  or user authorization.
- Do not include real media libraries or production-derived fixtures in tests,
  issues, logs, or pull requests.

Do not disclose a suspected vulnerability in a public issue. Until a dedicated
security reporting process is published, contact the repository owner privately
through their GitHub profile and provide only the minimum information needed to
establish a secure communication channel.

## Branches and commits

Create a short-lived branch from `main`:

```bash
git switch main && git pull --ff-only && \
  git switch -c feature/short-description
```

Use [Conventional Commits](https://www.conventionalcommits.org/) with concise,
imperative subjects. A commit should represent one coherent change and include
its tests and documentation where applicable.

Common types used by this project are:

| Type | Purpose |
| --- | --- |
| `feat` | Add or change user-visible functionality |
| `fix` | Correct defective behavior |
| `test` | Add or improve tests without changing behavior |
| `docs` | Change documentation only |
| `refactor` | Restructure code without changing behavior |
| `chore` | Maintain tooling, dependencies, or repository configuration |
| `ci` | Change continuous-integration workflows or scripts |

An optional scope may identify the affected area, for example `api`, `auth`,
`storage`, `cli`, or `ci`. Add `!` before the colon and a `BREAKING CHANGE:`
footer when a commit intentionally introduces an incompatible change.

Examples:

```text
feat(api): add versioned health endpoint
docs: document milestone verification
fix(auth): reject unauthorized media downloads
ci: validate milestone checks on pull requests
```

Before committing, review the staged changes and run the checks:

```bash
git diff --check && git diff --staged && \
  ./scripts/check_milestone.sh
```

## Pull requests

A pull request should:

- explain the problem and the chosen solution;
- identify the relevant milestone or issue;
- list behavior and security implications;
- include tests for changed behavior;
- update documentation when contracts or workflows change;
- remain small enough to review carefully;
- pass all automated checks.

Review feedback is part of the learning process. Resolve discussions with code,
tests, documentation, or a clear explanation of why no change is needed.

## Documentation

Documentation, code identifiers, filenames, comments, API fields, log messages,
and error messages are written in English. Commands in project documentation are
written for Git Bash and should be combined with `&&` when later commands depend
on earlier commands succeeding.

Update the relevant milestone document whenever its scope, acceptance criteria,
or completion status changes.
