package clipboard

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// WriteText writes text to the local clipboard using the platform's helper.
func WriteText(ctx context.Context, text string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var helper []string
	switch runtime.GOOS {
	case "darwin":
		helper = []string{"pbcopy"}
	case "windows":
		helper = []string{"clip.exe"}
	case "linux", "android", "freebsd", "netbsd", "openbsd", "dragonfly", "solaris", "illumos":
		var err error
		helper, err = unixHelper()
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("clipboard copy is unavailable on %s", runtime.GOOS)
	}
	return runHelper(ctx, text, helper)
}

func unixHelper() ([]string, error) {
	wayland := os.Getenv("WAYLAND_DISPLAY") != ""
	x11 := os.Getenv("DISPLAY") != ""
	if wayland {
		if path, err := exec.LookPath("wl-copy"); err == nil {
			return []string{path}, nil
		}
	}
	if x11 {
		if path, err := exec.LookPath("xclip"); err == nil {
			return []string{path, "-in", "-selection", "clipboard"}, nil
		}
		if path, err := exec.LookPath("xsel"); err == nil {
			return []string{path, "--input", "--clipboard"}, nil
		}
	}
	for _, name := range []string{"termux-clipboard-set", "clip.exe"} {
		if path, err := exec.LookPath(name); err == nil {
			return []string{path}, nil
		}
	}
	switch {
	case wayland:
		return nil, errors.New("install wl-clipboard for Wayland")
	case x11:
		return nil, errors.New("install xclip or xsel for X11")
	default:
		return nil, errors.New("no local clipboard session found")
	}
}

func runHelper(ctx context.Context, text string, helper []string) error {
	// Wayland and X11 helpers can keep stderr open in a background process
	// that serves paste requests. A file captures diagnostics without waiting for EOF.
	diagnostics, err := os.CreateTemp("", "workledger-clipboard-*")
	if err != nil {
		return fmt.Errorf("cannot capture clipboard helper diagnostics (%w)", err)
	}
	defer os.Remove(diagnostics.Name())
	defer diagnostics.Close()

	cmd := exec.CommandContext(ctx, helper[0], helper[1:]...)
	cmd.Stdin = strings.NewReader(text)
	cmd.Stderr = diagnostics
	cmd.WaitDelay = 250 * time.Millisecond
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		name := filepath.Base(helper[0])
		if errors.Is(err, exec.ErrNotFound) {
			return fmt.Errorf("%s is missing from PATH (%w)", name, err)
		}
		if _, seekErr := diagnostics.Seek(0, io.SeekStart); seekErr == nil {
			data, _ := io.ReadAll(io.LimitReader(diagnostics, 512))
			if message := strings.TrimSpace(string(data)); message != "" {
				return fmt.Errorf("%s (%w)", message, err)
			}
		}
		return fmt.Errorf("%s failed. Check your desktop session (%w)", name, err)
	}
	return nil
}
