package projecttool

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const releaseUsage = `Usage: projectctl release COMMAND [OPTIONS]

Commands:
  validate  Validate a release tag and changelog entry.
  build     Build cross-platform release archives and checksums.

Run "projectctl release COMMAND --help" for command-specific options.
`

const validateReleaseUsage = `Usage: projectctl release validate --version vMAJOR.MINOR.PATCH
`

const buildReleaseUsage = `Usage: projectctl release build --version vMAJOR.MINOR.PATCH [--output-directory PATH]
`

var releaseVersionPattern = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

var releaseTimestamp = time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)

type releaseTarget struct {
	operatingSystem string
	architecture    string
}

var releaseTargets = []releaseTarget{
	{"linux", "amd64"},
	{"linux", "arm64"},
	{"windows", "amd64"},
	{"darwin", "amd64"},
	{"darwin", "arm64"},
}

var releaseCommands = []string{"server", "admin", "client"}

func (app application) runReleaseCommand(argArguments []string) error {
	if len(argArguments) == 0 {
		fmt.Fprint(app.stderr, releaseUsage)

		return errors.New("release command is required")
	}
	if argArguments[0] == "--help" || argArguments[0] == "-h" {
		fmt.Fprint(app.stdout, releaseUsage)

		return nil
	}

	switch argArguments[0] {
	case "validate":
		return app.validateRelease(argArguments[1:])
	case "build":
		return app.buildRelease(argArguments[1:])
	default:
		fmt.Fprint(app.stderr, releaseUsage)

		return fmt.Errorf("unknown release command %q", argArguments[0])
	}
}

func (app application) validateRelease(argArguments []string) error {
	if hasHelpArgument(argArguments) {
		fmt.Fprint(app.stdout, validateReleaseUsage)

		return nil
	}
	version, err := parseVersionArguments(argArguments, validateReleaseUsage, app.stderr)
	if err != nil {
		return err
	}

	tagType, err := app.runner.Output(Command{
		Name: "git", Args: []string{"cat-file", "-t", version}, Dir: app.root,
	})
	if err != nil {
		return fmt.Errorf("inspect release tag: %w", err)
	}
	if strings.TrimSpace(tagType) != "tag" {
		return fmt.Errorf("release tag %q must be annotated", version)
	}

	commit, err := app.runner.Output(Command{
		Name: "git", Args: []string{"rev-list", "-n", "1", version}, Dir: app.root,
	})
	if err != nil {
		return fmt.Errorf("resolve release tag: %w", err)
	}
	commit = strings.TrimSpace(commit)
	if commit == "" {
		return errors.New("release tag did not resolve to a commit")
	}
	if err := app.runner.Run(Command{
		Name: "git",
		Args: []string{"merge-base", "--is-ancestor", commit, "origin/main"},
		Dir:  app.root,
	}); err != nil {
		return fmt.Errorf("release tag %q is not based on origin/main: %w", version, err)
	}

	changelog, err := os.ReadFile(filepath.Join(app.root, "CHANGELOG.md"))
	if err != nil {
		return fmt.Errorf("read changelog: %w", err)
	}
	releaseVersion := strings.TrimPrefix(version, "v")
	entryPattern := regexp.MustCompile(
		`(?m)^## \[` + regexp.QuoteMeta(releaseVersion) + `\] - [0-9]{4}-[0-9]{2}-[0-9]{2}$`,
	)
	if !entryPattern.Match(changelog) {
		return fmt.Errorf("changelog has no dated section for %q", version)
	}

	fmt.Fprintf(app.stdout, "Release %s is valid.\n", version)

	return nil
}

func (app application) buildRelease(argArguments []string) error {
	if hasHelpArgument(argArguments) {
		fmt.Fprint(app.stdout, buildReleaseUsage)

		return nil
	}
	version, outputDirectory, err := parseBuildArguments(argArguments, app.stderr)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(outputDirectory) {
		outputDirectory = filepath.Join(app.root, outputDirectory)
	}
	if err := os.MkdirAll(outputDirectory, 0o755); err != nil {
		return fmt.Errorf("create release output directory: %w", err)
	}

	stagingDirectory, err := os.MkdirTemp(outputDirectory, ".release-staging-")
	if err != nil {
		return fmt.Errorf("create release staging directory: %w", err)
	}
	defer os.RemoveAll(stagingDirectory)

	packageRoot := filepath.Join(stagingDirectory, "packages")
	archiveRoot := filepath.Join(stagingDirectory, "archives")
	if err := os.MkdirAll(archiveRoot, 0o755); err != nil {
		return fmt.Errorf("create archive staging directory: %w", err)
	}

	releaseVersion := strings.TrimPrefix(version, "v")
	archiveNames := make([]string, 0, len(releaseTargets))
	for _, target := range releaseTargets {
		archiveBase := fmt.Sprintf(
			"go-mediaarchive_%s_%s_%s",
			releaseVersion,
			target.operatingSystem,
			target.architecture,
		)
		packageDirectory := filepath.Join(packageRoot, archiveBase)
		if err := os.MkdirAll(packageDirectory, 0o755); err != nil {
			return fmt.Errorf("create release package directory: %w", err)
		}

		for _, commandName := range releaseCommands {
			binaryName := "go-mediaarchive-" + commandName
			if target.operatingSystem == "windows" {
				binaryName += ".exe"
			}
			fmt.Fprintf(
				app.stdout,
				"Building %s for %s/%s\n",
				commandName,
				target.operatingSystem,
				target.architecture,
			)
			if err := app.runner.Run(Command{
				Name: "go",
				Args: []string{
					"build", "-trimpath", "-o", filepath.Join(packageDirectory, binaryName),
					"./cmd/" + commandName,
				},
				Dir: app.root,
				Env: []string{
					"CGO_ENABLED=0",
					"GOOS=" + target.operatingSystem,
					"GOARCH=" + target.architecture,
				},
			}); err != nil {
				return fmt.Errorf(
					"build %s for %s/%s: %w",
					commandName,
					target.operatingSystem,
					target.architecture,
					err,
				)
			}
			if err := os.Chmod(filepath.Join(packageDirectory, binaryName), 0o755); err != nil {
				return fmt.Errorf("set executable mode for %s: %w", binaryName, err)
			}
		}

		for _, name := range []string{"CHANGELOG.md", "LICENSE", "README.md"} {
			if err := copyFile(
				filepath.Join(app.root, name),
				filepath.Join(packageDirectory, name),
				0o644,
			); err != nil {
				return fmt.Errorf("copy release document %s: %w", name, err)
			}
		}

		archiveName := archiveBase + ".tar.gz"
		archivePath := filepath.Join(archiveRoot, archiveName)
		if target.operatingSystem == "windows" {
			archiveName = archiveBase + ".zip"
			archivePath = filepath.Join(archiveRoot, archiveName)
			err = writeZIPArchive(archivePath, packageRoot, packageDirectory)
		} else {
			err = writeTarGZIPArchive(archivePath, packageRoot, packageDirectory)
		}
		if err != nil {
			return fmt.Errorf("create archive for %s/%s: %w", target.operatingSystem, target.architecture, err)
		}
		archiveNames = append(archiveNames, archiveName)
	}

	if err := writeChecksums(archiveRoot, archiveNames); err != nil {
		return err
	}
	for _, name := range append(archiveNames, "SHA256SUMS") {
		if err := replaceFile(filepath.Join(archiveRoot, name), filepath.Join(outputDirectory, name)); err != nil {
			return fmt.Errorf("publish release file %s: %w", name, err)
		}
	}

	fmt.Fprintf(
		app.stdout,
		"Created %d release archives and %s\n",
		len(archiveNames),
		filepath.Join(outputDirectory, "SHA256SUMS"),
	)

	return nil
}

func hasHelpArgument(argArguments []string) bool {
	for _, argument := range argArguments {
		if argument == "--help" || argument == "-h" {
			return true
		}
	}

	return false
}

func parseVersionArguments(
	argArguments []string,
	argUsage string,
	argStderr io.Writer,
) (string, error) {
	version := ""
	for index := 0; index < len(argArguments); index++ {
		switch argArguments[index] {
		case "--version":
			if index+1 >= len(argArguments) {
				fmt.Fprint(argStderr, argUsage)

				return "", errors.New("version value is required")
			}
			index++
			version = argArguments[index]
		default:
			fmt.Fprint(argStderr, argUsage)

			return "", fmt.Errorf("unknown release argument %q", argArguments[index])
		}
	}
	if !releaseVersionPattern.MatchString(version) {
		return "", fmt.Errorf("version must use vMAJOR.MINOR.PATCH: %q", version)
	}

	return version, nil
}

func parseBuildArguments(argArguments []string, argStderr io.Writer) (string, string, error) {
	versionArguments := make([]string, 0, 2)
	outputDirectory := "dist"
	for index := 0; index < len(argArguments); index++ {
		switch argArguments[index] {
		case "--version":
			if index+1 >= len(argArguments) {
				fmt.Fprint(argStderr, buildReleaseUsage)

				return "", "", errors.New("version value is required")
			}
			versionArguments = append(versionArguments, "--version", argArguments[index+1])
			index++
		case "--output-directory":
			if index+1 >= len(argArguments) {
				fmt.Fprint(argStderr, buildReleaseUsage)

				return "", "", errors.New("output directory value is required")
			}
			outputDirectory = argArguments[index+1]
			index++
		default:
			fmt.Fprint(argStderr, buildReleaseUsage)

			return "", "", fmt.Errorf("unknown release build argument %q", argArguments[index])
		}
	}
	version, err := parseVersionArguments(versionArguments, buildReleaseUsage, argStderr)

	return version, outputDirectory, err
}

func copyFile(argSource string, argDestination string, argMode fs.FileMode) error {
	source, err := os.Open(argSource)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.OpenFile(argDestination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, argMode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(destination, source); err != nil {
		destination.Close()

		return err
	}

	return destination.Close()
}

func writeZIPArchive(argPath string, argRoot string, argPackage string) error {
	archive, err := os.Create(argPath)
	if err != nil {
		return err
	}
	writer := zip.NewWriter(archive)
	walkErr := filepath.WalkDir(argPackage, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(argRoot, path)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relative)
		header.Method = zip.Deflate
		header.Modified = releaseTimestamp
		destination, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		source, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(destination, source)
		closeErr := source.Close()
		if copyErr != nil {
			return copyErr
		}

		return closeErr
	})
	closeWriterErr := writer.Close()
	closeArchiveErr := archive.Close()
	if walkErr != nil {
		return walkErr
	}
	if closeWriterErr != nil {
		return closeWriterErr
	}

	return closeArchiveErr
}

func writeTarGZIPArchive(argPath string, argRoot string, argPackage string) error {
	archive, err := os.Create(argPath)
	if err != nil {
		return err
	}
	gzipWriter := gzip.NewWriter(archive)
	gzipWriter.Header.ModTime = releaseTimestamp
	tarWriter := tar.NewWriter(gzipWriter)
	walkErr := filepath.Walk(argPackage, func(path string, info fs.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(argRoot, path)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relative)
		header.ModTime = releaseTimestamp
		header.AccessTime = time.Time{}
		header.ChangeTime = time.Time{}
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		source, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tarWriter, source)
		closeErr := source.Close()
		if copyErr != nil {
			return copyErr
		}

		return closeErr
	})
	closeTarErr := tarWriter.Close()
	closeGZIPErr := gzipWriter.Close()
	closeArchiveErr := archive.Close()
	for _, closeErr := range []error{walkErr, closeTarErr, closeGZIPErr, closeArchiveErr} {
		if closeErr != nil {
			return closeErr
		}
	}

	return nil
}

func writeChecksums(argDirectory string, argNames []string) error {
	checksumPath := filepath.Join(argDirectory, "SHA256SUMS")
	checksumFile, err := os.Create(checksumPath)
	if err != nil {
		return fmt.Errorf("create checksum file: %w", err)
	}
	for _, name := range argNames {
		archive, err := os.Open(filepath.Join(argDirectory, name))
		if err != nil {
			checksumFile.Close()

			return fmt.Errorf("open archive for checksum: %w", err)
		}
		hash := sha256.New()
		_, copyErr := io.Copy(hash, archive)
		closeErr := archive.Close()
		if copyErr != nil {
			checksumFile.Close()

			return fmt.Errorf("hash release archive: %w", copyErr)
		}
		if closeErr != nil {
			checksumFile.Close()

			return fmt.Errorf("close release archive: %w", closeErr)
		}
		if _, err := fmt.Fprintf(checksumFile, "%s  %s\n", hex.EncodeToString(hash.Sum(nil)), name); err != nil {
			checksumFile.Close()

			return fmt.Errorf("write archive checksum: %w", err)
		}
	}
	if err := checksumFile.Close(); err != nil {
		return fmt.Errorf("close checksum file: %w", err)
	}

	return nil
}

func replaceFile(argSource string, argDestination string) error {
	if err := os.Remove(argDestination); err != nil && !os.IsNotExist(err) {
		return err
	}

	return os.Rename(argSource, argDestination)
}
