package artifact

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
	"path"
	"strings"
)

// MaxExecutableBytes bounds how much an archive entry may expand to.
const MaxExecutableBytes int64 = 64 << 20

var (
	// ErrExecutableNotFound is returned when the archive lacks the server executable.
	ErrExecutableNotFound = errors.New("executable was not found in archive")
	// ErrExecutableTooLarge is returned when the executable exceeds MaxExecutableBytes.
	ErrExecutableTooLarge = errors.New("executable exceeds extraction size limit")
	// ErrUnsupportedArchive is returned for archive formats other than tar.gz and zip.
	ErrUnsupportedArchive = errors.New("unsupported archive format")
)

// ExtractExecutable copies the entry named executableName from archive into dst
// and returns the hex SHA256 of the copied bytes. assetName selects the format.
func ExtractExecutable(
	archive []byte,
	assetName, executableName string,
	dst io.Writer,
) (string, error) {
	switch {
	case strings.HasSuffix(assetName, ".tar.gz"):
		return extractTarGz(archive, executableName, dst)
	case strings.HasSuffix(assetName, ".zip"):
		return extractZip(archive, executableName, dst)
	default:
		return "", fmt.Errorf("%w: %s", ErrUnsupportedArchive, assetName)
	}
}

func extractTarGz(archive []byte, executableName string, dst io.Writer) (string, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return "", fmt.Errorf("failed to read tar.gz: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("failed to read tar entry: %w", err)
		}
		// Archive entry names use "/" separators regardless of host OS.
		if header.Typeflag != tar.TypeReg || path.Base(header.Name) != executableName {
			continue
		}

		return copyHashed(dst, tr)
	}

	return "", fmt.Errorf("%w: %s", ErrExecutableNotFound, executableName)
}

func extractZip(archive []byte, executableName string, dst io.Writer) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return "", fmt.Errorf("failed to read zip: %w", err)
	}

	for _, file := range zr.File {
		if !file.Mode().IsRegular() || path.Base(file.Name) != executableName {
			continue
		}

		rc, err := file.Open()
		if err != nil {
			return "", fmt.Errorf("failed to open zip entry: %w", err)
		}
		digest, copyErr := copyHashed(dst, rc)
		closeErr := rc.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", fmt.Errorf("failed to close zip entry: %w", closeErr)
		}

		return digest, nil
	}

	return "", fmt.Errorf("%w: %s", ErrExecutableNotFound, executableName)
}

func copyHashed(dst io.Writer, src io.Reader) (string, error) {
	hasher := sha256.New()
	copied, err := io.Copy(io.MultiWriter(dst, hasher), io.LimitReader(src, MaxExecutableBytes+1))
	if err != nil {
		return "", fmt.Errorf("failed to extract executable: %w", err)
	}
	if copied > MaxExecutableBytes {
		return "", fmt.Errorf("%w: limit=%d", ErrExecutableTooLarge, MaxExecutableBytes)
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// SHA256Hex returns the hex SHA256 of data.
func SHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:])
}
