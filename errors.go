package main

import "errors"

var (
	// errServerNonZeroExit is returned when github-mcp-server exits with non-zero status.
	errServerNonZeroExit = errors.New("server exited with non-zero status")
	// errNoBundledServerForPlatform is returned when no bundled archive exists for the current platform.
	errNoBundledServerForPlatform = errors.New("no bundled github-mcp-server for platform")
	// errUnsupportedBundledArchiveFormat is returned when the bundled archive format is unknown.
	errUnsupportedBundledArchiveFormat = errors.New("unsupported bundled archive format")
	// errBundledChecksumMismatch is returned when bundled archive checksum validation fails.
	errBundledChecksumMismatch = errors.New("bundled github-mcp-server checksum mismatch")
	// errBundledExecutableNotFound is returned when the bundled executable is missing from the archive.
	errBundledExecutableNotFound = errors.New("bundled executable was not found in archive")
	// errBundledExecutableTooLarge is returned when extracted bytes exceed the configured limit.
	errBundledExecutableTooLarge = errors.New("bundled executable exceeds extraction size limit")
	// errBundledExecutableInvalidSize is returned when archive metadata reports an invalid executable size.
	errBundledExecutableInvalidSize = errors.New("bundled executable has invalid size")
	// errBundledTempParentInsecure is returned when the temp parent directory fails safety checks.
	errBundledTempParentInsecure     = errors.New("bundled temp parent directory is insecure")
	errBundledTempParentStateInvalid = errors.New("bundled temp parent directory state is invalid")
)
