//go:build windows

package main

import (
	"fmt"
	"os"
	"syscall"
)

func ensureSecureUnixTempParentDir(_ string, _ *os.File, info os.FileInfo) (os.FileInfo, error) {
	return info, nil
}

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

func openTempParentDir(parentDir string) (*os.File, error) {
	pathUTF16, err := syscall.UTF16PtrFromString(parentDir)
	if err != nil {
		return nil, fmt.Errorf("failed to encode parent directory %q: %w", parentDir, err)
	}

	handle, err := syscall.CreateFile(
		pathUTF16,
		0,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_BACKUP_SEMANTICS|syscall.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to open parent directory %q for verification: %w",
			parentDir,
			err,
		)
	}

	handleFile := os.NewFile(uintptr(handle), parentDir)
	if handleFile == nil {
		_ = syscall.CloseHandle(handle)
		return nil, fmt.Errorf("failed to wrap parent directory handle %q", parentDir)
	}

	return handleFile, nil
}
