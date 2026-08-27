# Releases

Go Media Archive distinguishes development milestones from installable product
releases.

## Tag types

| Tag | Purpose | Creates a GitHub release |
| --- | --- | --- |
| `milestone-NNN` | Immutable learning and development checkpoint | No |
| `vMAJOR.MINOR.PATCH` | Semantic product version | Yes |

A release may contain multiple completed milestones. Completing a milestone does
not require publishing a product release.

Release tags must:

- use the exact stable-version format `vMAJOR.MINOR.PATCH`;
- be annotated Git tags;
- point to a commit contained in `main`;
- pass the complete quality gate on the tagged commit.

Prerelease suffixes such as `-alpha.1` or `-rc.1` are not supported initially.

## Published programs

Each release contains these commands:

- `go-mediaarchive-server`
- `go-mediaarchive-admin`
- `go-mediaarchive-client`

Archives are built with `CGO_ENABLED=0` and `-trimpath` for:

- Linux AMD64;
- Linux ARM64;
- Windows AMD64;
- macOS AMD64;
- macOS ARM64.

Every archive also contains `LICENSE` and `README.md`. `SHA256SUMS` records the
SHA-256 checksum of every published archive. Runtime databases, media files,
credentials, certificates, private keys, and local configuration are never
included.

## Local release build

Build all release archives from Git Bash:

```bash
./scripts/build_release.sh \
  --version v0.1.0 \
  --output-directory dist
```

Run the equivalent build from Windows PowerShell:

```powershell
.\scripts\build_release.ps1 `
  -Version v0.1.0 `
  -OutputDirectory dist
```

The `dist` directory is ignored by Git. Local builds validate packaging but do
not publish a GitHub release.

## Publishing a release

First ensure the selected commit is on `main` and its Quality Gate workflow is
green. Then create and push an annotated tag from Git Bash:

```bash
git tag -a v0.1.0 -m "Release v0.1.0" && \
git push origin v0.1.0
```

Pushing the tag starts the Release workflow. It validates the tag, repeats the
quality gate, builds the archives, calculates checksums, and creates the GitHub
release with generated release notes.

Release tags and published releases are immutable. Corrections require a new
semantic version rather than moving or replacing an existing tag.
