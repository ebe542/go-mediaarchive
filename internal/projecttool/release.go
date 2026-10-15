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
  check     Check a tag and built assets before publication.
  publish   Publish checked assets to a GitHub release.

Run "projectctl release COMMAND --help" for command-specific options.
`

const validateReleaseUsage = `Usage: projectctl release validate --version vMAJOR.MINOR.PATCH
`

const buildReleaseUsage = `Usage: projectctl release build --version vMAJOR.MINOR.PATCH [--output-directory PATH]
`

const checkReleaseUsage = `Usage: projectctl release check --version vMAJOR.MINOR.PATCH --repository OWNER/REPOSITORY [--asset-directory PATH]
`

const publishReleaseUsage = `Usage: projectctl release publish --version vMAJOR.MINOR.PATCH --repository OWNER/REPOSITORY [--asset-directory PATH]
`

var releaseVersionPattern = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

var releaseRepositoryPattern = regexp.MustCompile(
	`^[A-Za-z0-9](?:[A-Za-z0-9_.-]*[A-Za-z0-9])?/[A-Za-z0-9](?:[A-Za-z0-9_.-]*[A-Za-z0-9])?$`,
)

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

func (app application) runReleaseCommand(arguments []string) error {
	if len(arguments) == 0 {
		fmt.Fprint(app.stderr, releaseUsage)

		return errors.New("release command is required")
	}
	if arguments[0] == "--help" || arguments[0] == "-h" {
		fmt.Fprint(app.stdout, releaseUsage)

		return nil
	}

	switch arguments[0] {
	case "validate":
		return app.validateRelease(arguments[1:])
	case "build":
		return app.buildRelease(arguments[1:])
	case "check":
		return app.checkRelease(arguments[1:])
	case "publish":
		return app.publishRelease(arguments[1:])
	default:
		fmt.Fprint(app.stderr, releaseUsage)

		return fmt.Errorf("unknown release command %q", arguments[0])
	}
}

func (app application) validateRelease(arguments []string) error {
	if hasHelpArgument(arguments) {
		fmt.Fprint(app.stdout, validateReleaseUsage)

		return nil
	}
	version, err := parseVersionArguments(arguments, validateReleaseUsage, app.stderr)
	if err != nil {
		return err
	}

	return app.validateReleaseVersion(version)
}

func (app application) validateReleaseVersion(version string) error {
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

func (app application) checkRelease(arguments []string) error {
	if hasHelpArgument(arguments) {
		fmt.Fprint(app.stdout, checkReleaseUsage)

		return nil
	}
	options, err := parseReleaseAssetArguments(arguments, checkReleaseUsage, app.stderr)
	if err != nil {
		return err
	}
	if err := app.validateReleaseVersion(options.version); err != nil {
		return err
	}
	if err := app.requireCommand("gh", "GitHub CLI"); err != nil {
		return err
	}
	options.assetDirectory = resolveReleaseDirectory(app.root, options.assetDirectory)
	if err := checkReleaseAssets(options.version, options.assetDirectory); err != nil {
		return err
	}

	fmt.Fprintf(app.stdout, "Release %s assets are ready for %s.\n", options.version, options.repository)

	return nil
}

func (app application) publishRelease(arguments []string) error {
	if hasHelpArgument(arguments) {
		fmt.Fprint(app.stdout, publishReleaseUsage)

		return nil
	}
	options, err := parseReleaseAssetArguments(arguments, publishReleaseUsage, app.stderr)
	if err != nil {
		return err
	}
	if err := app.requireCommand("gh", "GitHub CLI"); err != nil {
		return err
	}
	options.assetDirectory = resolveReleaseDirectory(app.root, options.assetDirectory)
	if err := checkReleaseAssets(options.version, options.assetDirectory); err != nil {
		return err
	}

	arguments = []string{"release", "create", options.version}
	for _, name := range expectedReleaseAssetNames(options.version) {
		arguments = append(arguments, filepath.Join(options.assetDirectory, name))
	}
	arguments = append(
		arguments,
		"--verify-tag",
		"--generate-notes",
		"--repo", options.repository,
		"--title", options.version,
	)
	if err := app.runner.Run(Command{Name: "gh", Args: arguments}); err != nil {
		return fmt.Errorf("publish GitHub release %s: %w", options.version, err)
	}
	fmt.Fprintf(app.stdout, "Published release %s to %s.\n", options.version, options.repository)

	return nil
}

func (app application) buildRelease(arguments []string) error {
	if hasHelpArgument(arguments) {
		fmt.Fprint(app.stdout, buildReleaseUsage)

		return nil
	}
	version, outputDirectory, err := parseBuildArguments(arguments, app.stderr)
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

func hasHelpArgument(arguments []string) bool {
	for _, argument := range arguments {
		if argument == "--help" || argument == "-h" {
			return true
		}
	}

	return false
}

func parseVersionArguments(
	arguments []string,
	usage string,
	stderr io.Writer,
) (string, error) {
	version := ""
	for index := 0; index < len(arguments); index++ {
		switch arguments[index] {
		case "--version":
			if index+1 >= len(arguments) {
				fmt.Fprint(stderr, usage)

				return "", errors.New("version value is required")
			}
			index++
			version = arguments[index]
		default:
			fmt.Fprint(stderr, usage)

			return "", fmt.Errorf("unknown release argument %q", arguments[index])
		}
	}
	if !releaseVersionPattern.MatchString(version) {
		return "", fmt.Errorf("version must use vMAJOR.MINOR.PATCH: %q", version)
	}

	return version, nil
}

func parseBuildArguments(arguments []string, stderr io.Writer) (string, string, error) {
	versionArguments := make([]string, 0, 2)
	outputDirectory := "dist"
	for index := 0; index < len(arguments); index++ {
		switch arguments[index] {
		case "--version":
			if index+1 >= len(arguments) {
				fmt.Fprint(stderr, buildReleaseUsage)

				return "", "", errors.New("version value is required")
			}
			versionArguments = append(versionArguments, "--version", arguments[index+1])
			index++
		case "--output-directory":
			if index+1 >= len(arguments) {
				fmt.Fprint(stderr, buildReleaseUsage)

				return "", "", errors.New("output directory value is required")
			}
			outputDirectory = arguments[index+1]
			index++
		default:
			fmt.Fprint(stderr, buildReleaseUsage)

			return "", "", fmt.Errorf("unknown release build argument %q", arguments[index])
		}
	}
	version, err := parseVersionArguments(versionArguments, buildReleaseUsage, stderr)

	return version, outputDirectory, err
}

type releaseAssetOptions struct {
	version        string
	repository     string
	assetDirectory string
}

func parseReleaseAssetArguments(
	arguments []string,
	usage string,
	stderr io.Writer,
) (releaseAssetOptions, error) {
	options := releaseAssetOptions{assetDirectory: "dist"}
	for index := 0; index < len(arguments); index++ {
		if index+1 >= len(arguments) {
			fmt.Fprint(stderr, usage)

			return releaseAssetOptions{}, fmt.Errorf("value is required for %s", arguments[index])
		}
		value := arguments[index+1]
		switch arguments[index] {
		case "--version":
			options.version = value
		case "--repository":
			options.repository = value
		case "--asset-directory":
			options.assetDirectory = value
		default:
			fmt.Fprint(stderr, usage)

			return releaseAssetOptions{}, fmt.Errorf("unknown release argument %q", arguments[index])
		}
		index++
	}
	if !releaseVersionPattern.MatchString(options.version) {
		return releaseAssetOptions{}, fmt.Errorf(
			"version must use vMAJOR.MINOR.PATCH: %q",
			options.version,
		)
	}
	if !releaseRepositoryPattern.MatchString(options.repository) {
		return releaseAssetOptions{}, fmt.Errorf(
			"repository must use OWNER/REPOSITORY: %q",
			options.repository,
		)
	}
	if strings.TrimSpace(options.assetDirectory) == "" {
		return releaseAssetOptions{}, errors.New("asset directory is required")
	}

	return options, nil
}

func resolveReleaseDirectory(root string, directory string) string {
	if filepath.IsAbs(directory) {
		return filepath.Clean(directory)
	}

	return filepath.Join(root, directory)
}

func expectedReleaseArchiveNames(version string) []string {
	releaseVersion := strings.TrimPrefix(version, "v")
	names := make([]string, 0, len(releaseTargets))
	for _, target := range releaseTargets {
		extension := ".tar.gz"
		if target.operatingSystem == "windows" {
			extension = ".zip"
		}
		names = append(names, fmt.Sprintf(
			"go-mediaarchive_%s_%s_%s%s",
			releaseVersion,
			target.operatingSystem,
			target.architecture,
			extension,
		))
	}

	return names
}

func expectedReleaseAssetNames(version string) []string {
	names := expectedReleaseArchiveNames(version)

	return append(names, "SHA256SUMS")
}

func checkReleaseAssets(version string, directory string) error {
	expectedNames := expectedReleaseAssetNames(version)
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("read release asset directory: %w", err)
	}
	actualNames := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			return fmt.Errorf("unexpected release asset directory %q", entry.Name())
		}
		actualNames[entry.Name()] = struct{}{}
	}
	for _, name := range expectedNames {
		if _, exists := actualNames[name]; !exists {
			return fmt.Errorf("required release asset %q is missing", name)
		}
		delete(actualNames, name)
	}
	if len(actualNames) != 0 {
		for name := range actualNames {
			return fmt.Errorf("unexpected release asset %q", name)
		}
	}

	checksumData, err := os.ReadFile(filepath.Join(directory, "SHA256SUMS"))
	if err != nil {
		return fmt.Errorf("read release checksums: %w", err)
	}
	checksums := make(map[string]string, len(releaseTargets))
	for lineNumber, line := range strings.Split(strings.TrimSpace(string(checksumData)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || len(fields[0]) != sha256.Size*2 {
			return fmt.Errorf("invalid checksum line %d", lineNumber+1)
		}
		if _, err := hex.DecodeString(fields[0]); err != nil {
			return fmt.Errorf("invalid checksum line %d: %w", lineNumber+1, err)
		}
		if _, duplicate := checksums[fields[1]]; duplicate {
			return fmt.Errorf("duplicate checksum for %q", fields[1])
		}
		checksums[fields[1]] = strings.ToLower(fields[0])
	}
	for _, name := range expectedReleaseArchiveNames(version) {
		expected, exists := checksums[name]
		if !exists {
			return fmt.Errorf("checksum for %q is missing", name)
		}
		actual, err := fileSHA256(filepath.Join(directory, name))
		if err != nil {
			return fmt.Errorf("hash release asset %q: %w", name, err)
		}
		if actual != expected {
			return fmt.Errorf("checksum for %q does not match", name)
		}
		delete(checksums, name)
	}
	if len(checksums) != 0 {
		for name := range checksums {
			return fmt.Errorf("unexpected checksum for %q", name)
		}
	}

	return nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

func copyFile(sourcePath string, destinationPath string, mode fs.FileMode) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(destination, source); err != nil {
		destination.Close()

		return err
	}

	return destination.Close()
}

func writeZIPArchive(path string, root string, packageName string) error {
	archive, err := os.Create(path)
	if err != nil {
		return err
	}
	writer := zip.NewWriter(archive)
	walkErr := filepath.WalkDir(packageName, func(path string, entry fs.DirEntry, walkErr error) error {
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
		relative, err := filepath.Rel(root, path)
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

func writeTarGZIPArchive(path string, root string, packageName string) error {
	archive, err := os.Create(path)
	if err != nil {
		return err
	}
	gzipWriter := gzip.NewWriter(archive)
	gzipWriter.Header.ModTime = releaseTimestamp
	tarWriter := tar.NewWriter(gzipWriter)
	walkErr := filepath.Walk(packageName, func(path string, info fs.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
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

func writeChecksums(directory string, names []string) error {
	checksumPath := filepath.Join(directory, "SHA256SUMS")
	checksumFile, err := os.Create(checksumPath)
	if err != nil {
		return fmt.Errorf("create checksum file: %w", err)
	}
	for _, name := range names {
		archive, err := os.Open(filepath.Join(directory, name))
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

func replaceFile(source string, destination string) error {
	if err := os.Remove(destination); err != nil && !os.IsNotExist(err) {
		return err
	}

	return os.Rename(source, destination)
}
