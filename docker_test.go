package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// Define static errors for testing
var (
	errInspectFailed = errors.New("inspect failed")
	errPullFailed    = errors.New("pull failed")
	errCreateFailed  = errors.New("create failed")
	errAttachFailed  = errors.New("attach failed")
	errStartFailed   = errors.New("start failed")
	errWaitFailed    = errors.New("wait failed")
)

// mockDockerClient implements dockerClientInterface for testing
type mockDockerClient struct {
	imageInspectErr     error
	imagePullErr        error
	imagePullResponse   string
	containerCreateErr  error
	containerID         string
	containerAttachErr  error
	containerStartErr   error
	containerWaitStatus int64
	containerWaitErr    error
}

func (m *mockDockerClient) ImageInspectWithRaw(
	_ context.Context,
	imageID string,
) (image.InspectResponse, []byte, error) {
	if m.imageInspectErr != nil {
		return image.InspectResponse{}, nil, m.imageInspectErr
	}
	return image.InspectResponse{ID: imageID}, nil, nil
}

func (m *mockDockerClient) ImagePull(
	_ context.Context,
	_ string,
	_ image.PullOptions,
) (io.ReadCloser, error) {
	if m.imagePullErr != nil {
		return nil, m.imagePullErr
	}
	return io.NopCloser(strings.NewReader(m.imagePullResponse)), nil
}

func (m *mockDockerClient) ContainerCreate(
	_ context.Context,
	_ *container.Config,
	_ *container.HostConfig,
	_ *network.NetworkingConfig,
	_ *ocispec.Platform,
	_ string,
) (container.CreateResponse, error) {
	if m.containerCreateErr != nil {
		return container.CreateResponse{}, m.containerCreateErr
	}
	return container.CreateResponse{ID: m.containerID}, nil
}

func (m *mockDockerClient) ContainerAttach(
	_ context.Context,
	_ string,
	_ container.AttachOptions,
) (types.HijackedResponse, error) {
	if m.containerAttachErr != nil {
		return types.HijackedResponse{}, m.containerAttachErr
	}
	// Return a mock HijackedResponse with proper Reader
	return types.HijackedResponse{
		Reader: bufio.NewReader(bytes.NewReader([]byte{})),
		Conn:   &mockConn{},
	}, nil
}

func (m *mockDockerClient) ContainerStart(
	_ context.Context,
	_ string,
	_ container.StartOptions,
) error {
	return m.containerStartErr
}

func (m *mockDockerClient) ContainerWait(
	_ context.Context,
	_ string,
	_ container.WaitCondition,
) (<-chan container.WaitResponse, <-chan error) {
	statusCh := make(chan container.WaitResponse, 1)
	errCh := make(chan error, 1)

	if m.containerWaitErr != nil {
		errCh <- m.containerWaitErr
	} else {
		statusCh <- container.WaitResponse{StatusCode: m.containerWaitStatus}
	}

	return statusCh, errCh
}

func (m *mockDockerClient) ContainerStop(
	_ context.Context,
	_ string,
	_ container.StopOptions,
) error {
	return nil
}

func (m *mockDockerClient) Close() error {
	return nil
}

func TestEnsureImage(t *testing.T) {
	tests := []struct {
		name      string
		mock      *mockDockerClient
		imageName string
		wantErr   string
	}{
		{
			name:      "image exists locally",
			mock:      &mockDockerClient{},
			imageName: "test-image:latest",
		},
		{
			name: "image not found - successful pull",
			mock: &mockDockerClient{
				imageInspectErr:   &objectNotFoundError{object: "image", id: "test-image"},
				imagePullResponse: "Pull complete",
			},
			imageName: "test-image:latest",
		},
		{
			name: "image inspect error",
			mock: &mockDockerClient{
				imageInspectErr: errInspectFailed,
			},
			imageName: "test-image:latest",
			wantErr:   "failed to inspect image: inspect failed",
		},
		{
			name: "image pull error",
			mock: &mockDockerClient{
				imageInspectErr: &objectNotFoundError{object: "image", id: "test-image"},
				imagePullErr:    errPullFailed,
			},
			imageName: "test-image:latest",
			wantErr:   "failed to pull docker image 'test-image:latest': pull failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ensureImage(t.Context(), tt.mock, tt.imageName)

			if tt.wantErr != "" {
				if err == nil {
					t.Errorf("expected error %q, got nil", tt.wantErr)
					return
				}
				if err.Error() != tt.wantErr {
					t.Errorf("error = %q, want %q", err.Error(), tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestRunServerContainer(t *testing.T) {
	tests := []struct {
		name    string
		mock    *mockDockerClient
		env     []string
		wantErr string
	}{
		{
			name: "successful container run",
			mock: &mockDockerClient{
				containerID:         "test-container-123",
				containerWaitStatus: 0,
			},
			env: []string{"TEST=value"},
		},
		{
			name: "container create error",
			mock: &mockDockerClient{
				containerCreateErr: errCreateFailed,
			},
			wantErr: "failed to create container: create failed",
		},
		{
			name: "container attach error",
			mock: &mockDockerClient{
				containerID:        "test-container-123",
				containerAttachErr: errAttachFailed,
			},
			wantErr: "failed to attach to container: attach failed",
		},
		{
			name: "container start error",
			mock: &mockDockerClient{
				containerID:       "test-container-123",
				containerStartErr: errStartFailed,
			},
			wantErr: "failed to start container: start failed",
		},
		{
			name: "container wait error",
			mock: &mockDockerClient{
				containerID:      "test-container-123",
				containerWaitErr: errWaitFailed,
			},
			wantErr: "error waiting for container: wait failed",
		},
		{
			name: "container non-zero exit",
			mock: &mockDockerClient{
				containerID:         "test-container-123",
				containerWaitStatus: 1,
			},
			wantErr: fmt.Sprintf("%s: %d", ErrContainerNonZeroExit.Error(), 1),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use io.Pipe to create a blocking reader that won't EOF immediately
			// This prevents the stdinClosed channel from firing before error cases
			pipeReader, pipeWriter := io.Pipe()
			// Close the writer when test completes to clean up the goroutine
			t.Cleanup(func() {
				pipeWriter.Close()
			})

			streams := &ioStreams{
				in:  pipeReader,      // Blocking stdin
				out: &bytes.Buffer{}, // Capture stdout
				err: &bytes.Buffer{}, // Capture stderr
			}
			err := runServerContainer(t.Context(), tt.mock, tt.env, "test-image", streams)

			if tt.wantErr != "" {
				if err == nil {
					t.Errorf("expected error %q, got nil", tt.wantErr)
					return
				}
				if err.Error() != tt.wantErr {
					t.Errorf("error = %q, want %q", err.Error(), tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// objectNotFoundError implementation for testing
type objectNotFoundError struct {
	object string
	id     string
}

func (e objectNotFoundError) Error() string {
	return fmt.Sprintf("Error: No such %s: %s", e.object, e.id)
}

func (e objectNotFoundError) NotFound() {}

// Helper to check if error is not found
func IsErrNotFound(err error) bool {
	type notFound interface {
		NotFound()
	}
	_, ok := err.(notFound)
	return ok
}

// mockConn implements net.Conn for testing
type mockConn struct {
	bytes.Buffer
}

func (m *mockConn) Close() error                       { return nil }
func (m *mockConn) CloseWrite() error                  { return nil }
func (m *mockConn) LocalAddr() net.Addr                { return &net.TCPAddr{} }
func (m *mockConn) RemoteAddr() net.Addr               { return &net.TCPAddr{} }
func (m *mockConn) SetDeadline(_ time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(_ time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(_ time.Time) error { return nil }
