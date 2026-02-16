package main

import (
	"errors"
	"fmt"
	"os"
)

type tempParentDirState struct {
	info   os.FileInfo
	handle *os.File
}

func (s *tempParentDirState) close() {
	if s == nil || s.handle == nil {
		return
	}

	_ = s.handle.Close()
}

func createTempDirWithFallback(parentDirs []string) (string, func(), error) {
	if len(parentDirs) == 0 {
		parentDirs = []string{""}
	}

	attemptErrs := make([]error, 0, len(parentDirs))

	for _, parentDir := range parentDirs {
		tmpDir, cleanup, err := createTempDir(parentDir)
		if err == nil {
			return tmpDir, cleanup, nil
		}
		attemptErrs = append(attemptErrs, err)
	}

	return "", func() {}, fmt.Errorf(
		"failed to create temporary directory: %w",
		errors.Join(attemptErrs...),
	)
}

func createTempDir(parentDir string) (string, func(), error) {
	if parentDir == "" {
		tmpDir, err := os.MkdirTemp("", "gh-mcp-server-*")
		if err != nil {
			return "", func() {}, fmt.Errorf(
				"failed to create temporary directory in system temp: %w",
				err,
			)
		}

		cleanup := func() {
			_ = os.RemoveAll(tmpDir)
		}
		return tmpDir, cleanup, nil
	}

	parentState, err := ensureSecureTempParentDir(parentDir)
	if err != nil {
		return "", func() {}, err
	}

	tmpDir, err := createTempDirInVerifiedParent(parentDir, parentState)
	if err != nil {
		parentState.close()
		return "", func() {}, err
	}

	cleanup := func() {
		defer parentState.close()

		// Avoid deleting attacker-controlled paths if the parent changed.
		if err := verifyTempParentDirUnchanged(parentDir, parentState); err != nil {
			return
		}
		_ = os.RemoveAll(tmpDir)
	}

	return tmpDir, cleanup, nil
}

func ensureSecureTempParentDir(parentDir string) (*tempParentDirState, error) {
	if err := os.MkdirAll(parentDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create parent directory %q: %w", parentDir, err)
	}

	info, err := os.Lstat(parentDir)
	if err != nil {
		return nil, fmt.Errorf("failed to stat parent directory %q: %w", parentDir, err)
	}
	if err := validateTempParentDirInfo(parentDir, info); err != nil {
		return nil, err
	}

	state := &tempParentDirState{info: info}

	handle, err := openTempParentDir(parentDir)
	if err != nil {
		return nil, err
	}

	handleInfo, err := handle.Stat()
	if err != nil {
		_ = handle.Close()
		return nil, fmt.Errorf(
			"failed to stat opened parent directory %q: %w",
			parentDir,
			err,
		)
	}
	if err := validateTempParentDirInfo(parentDir, handleInfo); err != nil {
		_ = handle.Close()
		return nil, err
	}
	if !os.SameFile(info, handleInfo) {
		_ = handle.Close()
		return nil, fmt.Errorf(
			"%w: parent directory %q changed while preparing temporary directory",
			errBundledTempParentInsecure,
			parentDir,
		)
	}

	tightenedInfo, err := ensureSecureUnixTempParentDir(parentDir, handle, handleInfo)
	if err != nil {
		_ = handle.Close()
		return nil, err
	}

	latestPathInfo, err := os.Lstat(parentDir)
	if err != nil {
		_ = handle.Close()
		return nil, fmt.Errorf(
			"failed to stat parent directory %q after permissions check: %w",
			parentDir,
			err,
		)
	}
	if err := validateTempParentDirInfo(parentDir, latestPathInfo); err != nil {
		_ = handle.Close()
		return nil, err
	}
	if !os.SameFile(tightenedInfo, latestPathInfo) {
		_ = handle.Close()
		return nil, fmt.Errorf(
			"%w: parent directory %q changed while preparing temporary directory",
			errBundledTempParentInsecure,
			parentDir,
		)
	}

	state.info = tightenedInfo
	state.handle = handle

	return state, nil
}

func validateTempParentDirInfo(parentDir string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf(
			"%w: parent directory %q must not be a symbolic link",
			errBundledTempParentInsecure,
			parentDir,
		)
	}
	if !info.IsDir() {
		return fmt.Errorf(
			"%w: parent path %q is not a directory",
			errBundledTempParentInsecure,
			parentDir,
		)
	}

	return nil
}

func verifyTempParentDirUnchanged(parentDir string, expected *tempParentDirState) error {
	if expected == nil || expected.info == nil {
		return fmt.Errorf(
			"%w: missing expected parent directory state for %q",
			errBundledTempParentStateInvalid,
			parentDir,
		)
	}

	current, err := os.Lstat(parentDir)
	if err != nil {
		return fmt.Errorf("failed to re-check parent directory %q: %w", parentDir, err)
	}
	if err := validateTempParentDirInfo(parentDir, current); err != nil {
		return err
	}

	baseline := expected.info
	if expected.handle != nil {
		baseline, err = expected.handle.Stat()
		if err != nil {
			return fmt.Errorf(
				"failed to stat opened parent directory %q during verification: %w",
				parentDir,
				err,
			)
		}
	}

	if !os.SameFile(baseline, current) {
		return fmt.Errorf(
			"%w: parent directory %q changed while creating temporary directory",
			errBundledTempParentInsecure,
			parentDir,
		)
	}

	return nil
}
