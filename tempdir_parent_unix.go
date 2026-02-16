//go:build !windows

package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const tempDirNameAttempts = 256

var errBundledTempDirNameAttemptsExhausted = errors.New(
	"bundled temp directory name attempts exhausted",
)

func createTempDirInVerifiedParent(
	parentDir string,
	parentState *tempParentDirState,
) (string, error) {
	if parentState == nil || parentState.handle == nil {
		return "", fmt.Errorf(
			"%w: missing opened parent directory for %q",
			errBundledTempParentStateInvalid,
			parentDir,
		)
	}

	parentFD := int(parentState.handle.Fd())

	for range tempDirNameAttempts {
		name, err := randomTempDirName()
		if err != nil {
			return "", fmt.Errorf(
				"failed to generate temp directory name in %q: %w",
				parentDir,
				err,
			)
		}

		err = unix.Mkdirat(parentFD, name, 0o700)
		if err == nil {
			if err := verifyTempParentDirUnchanged(parentDir, parentState); err != nil {
				_ = unix.Unlinkat(parentFD, name, unix.AT_REMOVEDIR)
				return "", err
			}

			return filepath.Join(parentDir, name), nil
		}

		if errors.Is(err, unix.EEXIST) {
			continue
		}

		return "", fmt.Errorf("failed to create temporary directory in %q: %w", parentDir, err)
	}

	return "", fmt.Errorf(
		"%w: failed to create temporary directory in %q",
		errBundledTempDirNameAttemptsExhausted,
		parentDir,
	)
}

func randomTempDirName() (string, error) {
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", fmt.Errorf("failed to read random bytes for temp directory name: %w", err)
	}

	return "gh-mcp-server-" + hex.EncodeToString(suffix[:]), nil
}
