//go:build !windows

package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

const tempDirNameAttempts = 256

var errBundledTempDirNameAttemptsExhausted = errors.New(
	"bundled temp directory name attempts exhausted",
)

func ensureSecureUnixTempParentDir(
	parentDir string,
	handle *os.File,
	info os.FileInfo,
) (os.FileInfo, error) {
	if uid, ok := fileInfoUID(info); ok && uid != os.Geteuid() {
		return nil, fmt.Errorf(
			"%w: parent directory %q must be owned by the current user",
			errBundledTempParentInsecure,
			parentDir,
		)
	}

	if perms := info.Mode().Perm(); perms&0o077 == 0 {
		return info, nil
	}

	if err := handle.Chmod(0o700); err != nil {
		return nil, fmt.Errorf(
			"failed to tighten parent directory %q permissions: %w",
			parentDir,
			err,
		)
	}

	tightenedInfo, err := handle.Stat()
	if err != nil {
		return nil, fmt.Errorf(
			"failed to stat opened parent directory %q after chmod: %w",
			parentDir,
			err,
		)
	}
	if err := validateTempParentDirInfo(parentDir, tightenedInfo); err != nil {
		return nil, err
	}
	if perms := tightenedInfo.Mode().Perm(); perms&0o077 != 0 {
		return nil, fmt.Errorf(
			"%w: parent directory %q permissions are too broad (%#o)",
			errBundledTempParentInsecure,
			parentDir,
			perms,
		)
	}

	return tightenedInfo, nil
}

func fileInfoUID(info os.FileInfo) (int, bool) {
	switch stat := info.Sys().(type) {
	case *syscall.Stat_t:
		if stat == nil {
			return 0, false
		}

		uid := uint64(stat.Uid)
		if uid > uint64(math.MaxInt) {
			return 0, false
		}

		return int(uid), true
	case syscall.Stat_t:
		uid := uint64(stat.Uid)
		if uid > uint64(math.MaxInt) {
			return 0, false
		}

		return int(uid), true
	default:
		return 0, false
	}
}

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

	// #nosec G115 -- file descriptors are small non-negative integers on Unix
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

func openTempParentDir(parentDir string) (*os.File, error) {
	handle, err := os.Open(parentDir)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to open parent directory %q for verification: %w",
			parentDir,
			err,
		)
	}

	return handle, nil
}
