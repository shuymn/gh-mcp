package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"strings"
)

const maxBundledExecutableBytes int64 = 64 << 20 // 64 MiB

func verifyBundledArchiveChecksum(archive []byte, expectedSHA256 string) error {
	sum := sha256.Sum256(archive)
	actual := hex.EncodeToString(sum[:])

	if !strings.EqualFold(actual, expectedSHA256) {
		return fmt.Errorf(
			"%w: archive=%s expected=%s actual=%s",
			errBundledChecksumMismatch,
			bundledMCPArchiveName,
			expectedSHA256,
			actual,
		)
	}

	return nil
}

func extractTarGzExecutable(archive []byte, executableName, outputPath string) error {
	gzipReader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return fmt.Errorf("failed to read bundled tar.gz: %w", err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read bundled tar entry: %w", err)
		}

		if !header.FileInfo().Mode().IsRegular() {
			continue
		}
		// Archive entry names use "/" separators regardless of host OS.
		if path.Base(header.Name) != executableName {
			continue
		}
		if err := validateBundledExecutableSize(header.Size, executableName); err != nil {
			return err
		}

		file, err := os.OpenFile(outputPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
		if err != nil {
			return fmt.Errorf("failed to create extracted binary: %w", err)
		}

		if err := copyBundledExecutableWithLimit(file, tarReader, executableName); err != nil {
			_ = file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("failed to close extracted binary: %w", err)
		}

		return nil
	}

	return fmt.Errorf(
		"%w: executable=%q archive=%s",
		errBundledExecutableNotFound,
		executableName,
		bundledMCPArchiveName,
	)
}

func extractZipExecutable(archive []byte, executableName, outputPath string) error {
	zipReader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return fmt.Errorf("failed to read bundled zip: %w", err)
	}

	for _, fileInArchive := range zipReader.File {
		// Archive entry names use "/" separators regardless of host OS.
		if path.Base(fileInArchive.Name) != executableName {
			continue
		}
		if err := validateZipExecutableSize(
			fileInArchive.UncompressedSize64,
			executableName,
		); err != nil {
			return err
		}

		readCloser, err := fileInArchive.Open()
		if err != nil {
			return fmt.Errorf("failed to open bundled executable: %w", err)
		}

		file, err := os.OpenFile(outputPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
		if err != nil {
			_ = readCloser.Close()
			return fmt.Errorf("failed to create extracted binary: %w", err)
		}

		if err := copyBundledExecutableWithLimit(file, readCloser, executableName); err != nil {
			_ = file.Close()
			_ = readCloser.Close()
			return err
		}

		if err := file.Close(); err != nil {
			_ = readCloser.Close()
			return fmt.Errorf("failed to close extracted binary: %w", err)
		}
		if err := readCloser.Close(); err != nil {
			return fmt.Errorf("failed to close bundled executable stream: %w", err)
		}

		return nil
	}

	return fmt.Errorf(
		"%w: executable=%q archive=%s",
		errBundledExecutableNotFound,
		executableName,
		bundledMCPArchiveName,
	)
}

func copyBundledExecutableWithLimit(dst io.Writer, src io.Reader, executableName string) error {
	copied, err := io.Copy(dst, io.LimitReader(src, maxBundledExecutableBytes+1))
	if err != nil {
		return fmt.Errorf("failed to extract bundled binary: %w", err)
	}
	if copied > maxBundledExecutableBytes {
		return fmt.Errorf(
			"%w: executable=%q limit=%d archive=%s",
			errBundledExecutableTooLarge,
			executableName,
			maxBundledExecutableBytes,
			bundledMCPArchiveName,
		)
	}

	return nil
}

func validateBundledExecutableSize(size int64, executableName string) error {
	if size < 0 || size > maxBundledExecutableBytes {
		return fmt.Errorf(
			"%w: executable=%q size=%d archive=%s allowed=0-%d",
			errBundledExecutableInvalidSize,
			executableName,
			size,
			bundledMCPArchiveName,
			maxBundledExecutableBytes,
		)
	}

	return nil
}

func validateZipExecutableSize(size uint64, executableName string) error {
	if size > math.MaxInt64 {
		return fmt.Errorf(
			"%w: executable=%q size=%d archive=%s allowed=0-%d",
			errBundledExecutableInvalidSize,
			executableName,
			size,
			bundledMCPArchiveName,
			maxBundledExecutableBytes,
		)
	}

	return validateBundledExecutableSize(int64(size), executableName)
}
