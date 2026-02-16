//go:build windows

package main

import (
	"fmt"
	"os"
)

func createTempDirInVerifiedParent(
	parentDir string,
	parentState *tempParentDirState,
) (string, error) {
	tmpDir, err := os.MkdirTemp(parentDir, "gh-mcp-server-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary directory in %q: %w", parentDir, err)
	}

	if err := verifyTempParentDirUnchanged(parentDir, parentState); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", err
	}

	return tmpDir, nil
}
