package clipboard

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRunHelperReportsDiagnosticsAndHonorsDeadline(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	helper := []string{executable, "-test.run=^TestClipboardHelperProcess$"}
	t.Run("stderr", func(t *testing.T) {
		t.Setenv("WORKLEDGER_TEST_CLIPBOARD_HELPER", "fail")
		err := runHelper(t.Context(), "APP-2\nAPP-10", helper)
		if err == nil || !strings.Contains(err.Error(), "could not connect to pasteboard") {
			t.Fatalf("clipboard error = %v", err)
		}
	})
	t.Run("no stderr", func(t *testing.T) {
		t.Setenv("WORKLEDGER_TEST_CLIPBOARD_HELPER", "fail quietly")
		err := runHelper(t.Context(), "APP-2", helper)
		if err == nil || !strings.Contains(err.Error(), "Check your desktop session") {
			t.Fatalf("clipboard error = %v", err)
		}
	})
	t.Run("deadline", func(t *testing.T) {
		t.Setenv("WORKLEDGER_TEST_CLIPBOARD_HELPER", "hang")
		ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
		defer cancel()
		if err := runHelper(ctx, "APP-2", helper); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("clipboard error = %v", err)
		}
	})
}

func TestClipboardHelperProcess(t *testing.T) {
	switch os.Getenv("WORKLEDGER_TEST_CLIPBOARD_HELPER") {
	case "fail":
		text, err := io.ReadAll(os.Stdin)
		if err != nil || string(text) != "APP-2\nAPP-10" {
			t.Fatalf("helper stdin = %q, %v", text, err)
		}
		fmt.Fprintln(os.Stderr, "could not connect to pasteboard")
		os.Exit(1)
	case "fail quietly":
		os.Exit(1)
	case "hang":
		time.Sleep(30 * time.Second)
	}
}
