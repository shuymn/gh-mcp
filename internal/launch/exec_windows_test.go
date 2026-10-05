//go:build windows

package launch_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/shuymn/gh-mcp/internal/launch"
)

// CREATE_NEW_CONSOLE from the Windows process creation flags.
const createNewConsole = 0x00000010

func TestExecWaitsForInterruptedChild(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, path, "-test.run=^TestExecConsoleHelper$")
	cmd.Env = append(os.Environ(), "GH_MCP_EXEC_TEST=launcher")
	// Isolate console events from the test runner and its other tests.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNewConsole,
		HideWindow:    true,
	}
	cmd.WaitDelay = 5 * time.Second
	output, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 {
		t.Fatalf("launcher exit = %v, want child exit 7; output=%s", err, output)
	}
}

func TestExecConsoleHelper(t *testing.T) {
	switch os.Getenv("GH_MCP_EXEC_TEST") {
	case "launcher":
		code, err := launch.Exec(
			os.Args[0],
			[]string{"-test.run=^TestExecConsoleHelper$"},
			append(os.Environ(), "GH_MCP_EXEC_TEST=server"),
		)
		if err != nil {
			t.Fatal(err)
		}
		os.Exit(code)
	case "server":
		interrupts := make(chan os.Signal, 1)
		signal.Notify(interrupts, os.Interrupt)

		// CTRL_BREAK reaches both processes sharing this helper's console.
		generate := syscall.NewLazyDLL("kernel32.dll").NewProc("GenerateConsoleCtrlEvent")
		if ok, _, err := generate.Call(syscall.CTRL_BREAK_EVENT, 0); ok == 0 {
			t.Fatalf("GenerateConsoleCtrlEvent: %v", err)
		}
		select {
		case <-interrupts:
		case <-time.After(5 * time.Second):
			t.Fatal("child did not receive console interrupt")
		}
		// Simulate graceful shutdown; the launcher must stay alive to wait for it.
		time.Sleep(250 * time.Millisecond)
		os.Exit(7)
	}
}
