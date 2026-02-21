package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cli/go-gh/v2"
)

const (
	upstreamRepository           = "github/github-mcp-server"
	bundledDirName               = "bundled"
	mcpVersionFileName           = "mcp_version.go"
	defaultDownloadRetryCount    = 3
	defaultDownloadRetryDelaySec = 2
	checksumLineFieldCount       = 2
	bundleFileGlobPattern        = "bundle_*.go"
)

var (
	errUsage             = errors.New("usage: prepare [-check] [version]")
	errGHNotInstalled    = errors.New("gh CLI is required but not installed")
	errInvalidRetryCount = errors.New("DOWNLOAD_RETRY_COUNT must be a positive integer")
	errInvalidRetryDelay = errors.New(
		"DOWNLOAD_RETRY_DELAY_SECONDS must be a positive integer",
	)
	errParseMCPVersion        = errors.New("failed to parse mcpServerVersion")
	errGHAuthRequired         = errors.New("gh authentication is required")
	errContextDone            = errors.New("prepare interrupted")
	errGHCommandFailed        = errors.New("gh command failed")
	errChecksumNotFound       = errors.New("asset checksum not found")
	errChecksumMismatch       = errors.New("asset checksum mismatch")
	errPinnedChecksumNotFound = errors.New("pinned checksum not found")
	errPinnedChecksumMismatch = errors.New("pinned checksum mismatch")
	errBundleMetadataNotFound = errors.New("bundle metadata not found")
	errBundleMetadataInvalid  = errors.New("bundle metadata is invalid")
	errStringConstNotFound    = errors.New("string const not found")
	errStringConstInvalid     = errors.New("string const is invalid")
	assets                    = []string{
		"github-mcp-server_Darwin_arm64.tar.gz",
		"github-mcp-server_Darwin_x86_64.tar.gz",
		"github-mcp-server_Linux_i386.tar.gz",
		"github-mcp-server_Linux_arm64.tar.gz",
		"github-mcp-server_Linux_x86_64.tar.gz",
		"github-mcp-server_Windows_arm64.zip",
		"github-mcp-server_Windows_i386.zip",
		"github-mcp-server_Windows_x86_64.zip",
	}
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	options, err := parseArgs(args)
	if err != nil {
		return err
	}

	version, err := resolveVersion(options.version)
	if err != nil {
		return err
	}

	if options.checkOnly {
		return checkBundledAssets(version)
	}

	retryCount, err := positiveIntFromEnv(
		"DOWNLOAD_RETRY_COUNT",
		defaultDownloadRetryCount,
		errInvalidRetryCount,
	)
	if err != nil {
		return err
	}
	retryDelaySec, err := positiveIntFromEnv(
		"DOWNLOAD_RETRY_DELAY_SECONDS",
		defaultDownloadRetryDelaySec,
		errInvalidRetryDelay,
	)
	if err != nil {
		return err
	}
	retryDelay := time.Duration(retryDelaySec) * time.Second

	if _, err := gh.Path(); err != nil {
		return fmt.Errorf("%w: %w", errGHNotInstalled, err)
	}

	if err := ensureGHAuth(ctx); err != nil {
		return err
	}

	if err := os.MkdirAll(bundledDirName, 0o755); err != nil {
		return fmt.Errorf("failed to create bundled directory %q: %w", bundledDirName, err)
	}

	stagingDir, err := os.MkdirTemp(bundledDirName, ".prepare-bundled-mcp-server.")
	if err != nil {
		return fmt.Errorf("failed to create staging directory: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(stagingDir)
	}()

	checksumsAsset := checksumsAssetName(version)
	checksumsFile := filepath.Join(stagingDir, checksumsAsset)

	fmt.Printf("Downloading checksums for %s...\n", version)
	if err := runWithRetry(
		ctx,
		retryCount,
		retryDelay,
		"download "+checksumsAsset,
		func() error {
			return downloadReleaseAsset(ctx, version, checksumsAsset, stagingDir)
		},
	); err != nil {
		return err
	}

	fmt.Printf("Verifying attestation for %s...\n", checksumsAsset)
	if err := runWithRetry(
		ctx,
		retryCount,
		retryDelay,
		"verify attestation for "+checksumsAsset,
		func() error {
			return verifyReleaseAssetAttestation(ctx, version, checksumsFile)
		},
	); err != nil {
		return err
	}

	checksums, err := loadChecksums(checksumsFile)
	if err != nil {
		return err
	}

	for _, asset := range assets {
		fmt.Printf("Downloading %s...\n", asset)
		if err := runWithRetry(
			ctx,
			retryCount,
			retryDelay,
			"download "+asset,
			func() error {
				return downloadReleaseAsset(ctx, version, asset, stagingDir)
			},
		); err != nil {
			return err
		}

		filePath := filepath.Join(stagingDir, asset)
		if err := verifyAssetChecksum(checksums, asset, filePath, checksumsFile); err != nil {
			return err
		}
	}

	for _, asset := range assets {
		if err := promoteStagedAsset(stagingDir, bundledDirName, asset); err != nil {
			return err
		}
	}
	if err := promoteStagedAsset(stagingDir, bundledDirName, checksumsAsset); err != nil {
		return err
	}

	fmt.Printf("Bundled assets prepared for %s.\n", version)

	return nil
}

type prepareOptions struct {
	checkOnly bool
	version   string
}

func parseArgs(args []string) (*prepareOptions, error) {
	flagSet := flag.NewFlagSet("prepare", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)

	checkOnly := flagSet.Bool("check", false, "validate bundled assets without downloading")
	if err := flagSet.Parse(args); err != nil {
		return nil, errUsage
	}

	remaining := flagSet.Args()
	if len(remaining) > 1 {
		return nil, errUsage
	}

	options := &prepareOptions{
		checkOnly: *checkOnly,
	}
	if len(remaining) == 1 {
		options.version = remaining[0]
	}

	return options, nil
}

func resolveVersion(version string) (string, error) {
	if version != "" {
		return version, nil
	}

	content, err := os.ReadFile(mcpVersionFileName)
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", mcpVersionFileName, err)
	}

	return parseMCPServerVersion(content)
}

func checksumsAssetName(version string) string {
	return fmt.Sprintf("github-mcp-server_%s_checksums.txt", strings.TrimPrefix(version, "v"))
}

func checkBundledAssets(version string) error {
	checksumsFile := filepath.Join(bundledDirName, checksumsAssetName(version))

	checksums, err := loadChecksums(checksumsFile)
	if err != nil {
		return err
	}
	pinnedChecksums, err := loadPinnedChecksums()
	if err != nil {
		return err
	}
	if err := verifyPinnedChecksums(checksums, pinnedChecksums); err != nil {
		return err
	}

	for _, asset := range assets {
		filePath := filepath.Join(bundledDirName, asset)
		if _, err := os.Stat(filePath); err != nil {
			return fmt.Errorf("failed to stat bundled asset %s: %w", filePath, err)
		}
		if err := verifyAssetChecksum(checksums, asset, filePath, checksumsFile); err != nil {
			return err
		}
	}

	return nil
}

func loadPinnedChecksums() (map[string]string, error) {
	bundleFiles, err := filepath.Glob(bundleFileGlobPattern)
	if err != nil {
		return nil, fmt.Errorf("failed to list bundle files: %w", err)
	}
	if len(bundleFiles) == 0 {
		return nil, fmt.Errorf("%w: pattern=%s", errBundleMetadataNotFound, bundleFileGlobPattern)
	}

	slices.Sort(bundleFiles)

	pinnedChecksums := make(map[string]string, len(bundleFiles))
	for _, bundleFile := range bundleFiles {
		metadata, err := parseBundleMetadata(bundleFile)
		if err != nil {
			return nil, err
		}
		if metadata.archiveName == "" && metadata.sha256 == "" {
			continue
		}
		if metadata.archiveName == "" || metadata.sha256 == "" {
			return nil, fmt.Errorf("%w: file=%s", errBundleMetadataInvalid, bundleFile)
		}

		pinnedChecksums[metadata.archiveName] = metadata.sha256
	}

	return pinnedChecksums, nil
}

type bundleMetadata struct {
	archiveName string
	sha256      string
}

func parseBundleMetadata(bundleFile string) (*bundleMetadata, error) {
	content, err := os.ReadFile(bundleFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", bundleFile, err)
	}

	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, bundleFile, content, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", bundleFile, err)
	}

	archiveName, err := extractBundleConst(file, "bundledMCPArchiveName")
	if err != nil {
		return nil, fmt.Errorf(
			"%w: file=%s field=bundledMCPArchiveName",
			classifyBundleMetadataConstErr(err),
			bundleFile,
		)
	}
	sha256, err := extractBundleConst(file, "bundledMCPArchiveSHA256")
	if err != nil {
		return nil, fmt.Errorf(
			"%w: file=%s field=bundledMCPArchiveSHA256",
			classifyBundleMetadataConstErr(err),
			bundleFile,
		)
	}

	return &bundleMetadata{
		archiveName: archiveName,
		sha256:      sha256,
	}, nil
}

func classifyBundleMetadataConstErr(err error) error {
	if errors.Is(err, errStringConstNotFound) {
		return errBundleMetadataNotFound
	}

	return errBundleMetadataInvalid
}

func extractBundleConst(file *ast.File, constName string) (string, error) {
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.CONST {
			continue
		}

		for _, spec := range genDecl.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}

			for index, name := range valueSpec.Names {
				if name.Name != constName {
					continue
				}

				valueExpr := valueSpecExprAt(valueSpec, index)
				lit, ok := valueExpr.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return "", fmt.Errorf("%w: const=%s", errStringConstInvalid, constName)
				}

				value, err := strconv.Unquote(lit.Value)
				if err != nil {
					return "", fmt.Errorf("%w: const=%s", errStringConstInvalid, constName)
				}

				return value, nil
			}
		}
	}

	return "", fmt.Errorf("%w: const=%s", errStringConstNotFound, constName)
}

func verifyPinnedChecksums(checksums map[string]string, pinnedChecksums map[string]string) error {
	for _, asset := range assets {
		expectedChecksum, ok := checksums[asset]
		if !ok {
			return fmt.Errorf("%w: asset=%s", errChecksumNotFound, asset)
		}

		pinnedChecksum, ok := pinnedChecksums[asset]
		if !ok {
			return fmt.Errorf("%w: asset=%s", errPinnedChecksumNotFound, asset)
		}
		if pinnedChecksum != expectedChecksum {
			return fmt.Errorf(
				"%w: asset=%s expected=%s pinned=%s",
				errPinnedChecksumMismatch,
				asset,
				expectedChecksum,
				pinnedChecksum,
			)
		}
	}

	return nil
}

func parseMCPServerVersion(content []byte) (string, error) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, mcpVersionFileName, content, 0)
	if err != nil {
		return "", fmt.Errorf("failed to parse %s: %w", mcpVersionFileName, err)
	}

	version, err := extractBundleConst(file, "mcpServerVersion")
	if err != nil || !strings.HasPrefix(version, "v") || len(version) <= 1 {
		return "", fmt.Errorf("%w from %s", errParseMCPVersion, mcpVersionFileName)
	}

	return version, nil
}

func valueSpecExprAt(valueSpec *ast.ValueSpec, index int) ast.Expr {
	if valueSpec == nil || len(valueSpec.Values) == 0 {
		return nil
	}
	if len(valueSpec.Values) == len(valueSpec.Names) {
		return valueSpec.Values[index]
	}
	if len(valueSpec.Values) == 1 {
		return valueSpec.Values[0]
	}

	return nil
}

func positiveIntFromEnv(key string, defaultValue int, invalidValueErr error) (int, error) {
	rawValue := strings.TrimSpace(os.Getenv(key))
	if rawValue == "" {
		return defaultValue, nil
	}

	value, err := strconv.Atoi(rawValue)
	if err != nil || value <= 0 {
		return 0, invalidValueErr
	}

	return value, nil
}

func ensureGHAuth(ctx context.Context) error {
	if os.Getenv("GH_TOKEN") != "" || os.Getenv("GITHUB_TOKEN") != "" {
		return nil
	}

	if err := runGHCommand(ctx, io.Discard, io.Discard, "auth", "status"); err == nil {
		return nil
	}

	return fmt.Errorf(
		"%w: run 'gh auth login' locally, or set GH_TOKEN (or GITHUB_TOKEN) in CI",
		errGHAuthRequired,
	)
}

func runWithRetry(
	ctx context.Context,
	retryCount int,
	retryDelay time.Duration,
	description string,
	fn func() error,
) error {
	var lastErr error

	for attempt := range retryCount {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%w: %w", errContextDone, err)
		}

		err := fn()
		if err == nil {
			return nil
		}
		lastErr = err

		if attempt+1 >= retryCount {
			break
		}

		fmt.Printf(
			"Retrying: %s (%d/%d) in %ds...\n",
			description,
			attempt+1,
			retryCount,
			int(retryDelay.Seconds()),
		)

		timer := time.NewTimer(retryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("%w: %w", errContextDone, ctx.Err())
		case <-timer.C:
		}
	}

	return fmt.Errorf("failed: %s (attempts=%d): %w", description, retryCount, lastErr)
}

func downloadReleaseAsset(
	ctx context.Context,
	version string,
	assetName string,
	outputDir string,
) error {
	return runGHCommand(
		ctx,
		os.Stdout,
		os.Stderr,
		"release",
		"download",
		version,
		"--repo",
		upstreamRepository,
		"--pattern",
		assetName,
		"--dir",
		outputDir,
		"--clobber",
	)
}

func verifyReleaseAssetAttestation(ctx context.Context, version, checksumsFile string) error {
	return runGHCommand(
		ctx,
		io.Discard,
		os.Stderr,
		"release",
		"verify-asset",
		version,
		checksumsFile,
		"--repo",
		upstreamRepository,
	)
}

func runGHCommand(ctx context.Context, stdout io.Writer, stderr io.Writer, args ...string) error {
	stdoutBuffer, stderrBuffer, err := gh.ExecContext(ctx, args...)

	if _, writeErr := stdout.Write(stdoutBuffer.Bytes()); writeErr != nil {
		return fmt.Errorf("failed to write gh stdout: %w", writeErr)
	}
	if _, writeErr := stderr.Write(stderrBuffer.Bytes()); writeErr != nil {
		return fmt.Errorf("failed to write gh stderr: %w", writeErr)
	}

	if err != nil {
		return fmt.Errorf("%w: gh %s: %w", errGHCommandFailed, strings.Join(args, " "), err)
	}

	return nil
}

func loadChecksums(checksumsFile string) (map[string]string, error) {
	// #nosec G703 -- paths are constructed from controlled constants via filepath.Join
	file, err := os.Open(checksumsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read checksums file %q: %w", checksumsFile, err)
	}
	defer file.Close()

	checksums := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < checksumLineFieldCount {
			continue
		}
		checksums[fields[1]] = fields[0]
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed reading checksums file %q: %w", checksumsFile, err)
	}

	return checksums, nil
}

func verifyAssetChecksum(
	checksums map[string]string,
	assetName string,
	filePath string,
	checksumsFile string,
) error {
	expected, ok := checksums[assetName]
	if !ok {
		return fmt.Errorf(
			"%w: asset=%s checksums=%s",
			errChecksumNotFound,
			assetName,
			checksumsFile,
		)
	}

	actual, err := sha256File(filePath)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf(
			"%w: asset=%s expected=%s actual=%s",
			errChecksumMismatch,
			assetName,
			expected,
			actual,
		)
	}

	return nil
}

func sha256File(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open %s: %w", filePath, err)
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("failed to hash %s: %w", filePath, err)
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func promoteStagedAsset(stagingDir string, bundledDir string, assetName string) error {
	src := filepath.Join(stagingDir, assetName)
	dst := filepath.Join(bundledDir, assetName)

	// Keep explicit remove for Windows where Rename does not replace existing files.
	// #nosec G703 -- paths are constructed from controlled constants via filepath.Join
	if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to remove existing asset %s: %w", dst, err)
	}

	// #nosec G703 -- paths are constructed from controlled constants via filepath.Join
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("failed to promote %s to %s: %w", src, dst, err)
	}

	return nil
}
