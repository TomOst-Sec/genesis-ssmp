package util

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/genesis-ssmp/genesis/cli/internal/logging"
)

// OpenURLResult reports what happened when trying to open a URL.
type OpenURLResult struct {
	URL       string
	Opened    bool   // true if a browser was successfully launched
	Method    string // which opener succeeded (or "" if none)
	Error     error  // last error encountered
	Clipboard bool   // true if URL was copied to clipboard
}

// OpenURL tries to open a URL in the user's browser using a fallback chain.
// It always returns the URL so the caller can display it regardless.
func OpenURL(rawURL string) OpenURLResult {
	result := OpenURLResult{URL: rawURL}

	openers := []struct {
		name string
		cmd  string
		args []string
	}{
		{"xdg-open", "xdg-open", []string{rawURL}},
		{"gio", "gio", []string{"open", rawURL}},
	}

	// Check $BROWSER env
	if browser := os.Getenv("BROWSER"); browser != "" {
		openers = append(openers, struct {
			name string
			cmd  string
			args []string
		}{"$BROWSER (" + browser + ")", browser, []string{rawURL}})
	}

	for _, opener := range openers {
		logging.Debug("Trying to open URL", "opener", opener.name, "url", rawURL)

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		cmd := exec.CommandContext(ctx, opener.cmd, opener.args...)
		cmd.Stdout = nil
		cmd.Stderr = nil

		err := cmd.Start()
		if err != nil {
			cancel()
			logging.Debug("Opener failed to start", "opener", opener.name, "error", err)
			result.Error = err
			continue
		}

		// Don't wait for the browser process to exit — just check it started
		go func() {
			cmd.Wait()
			cancel()
		}()

		// Give it a moment to fail fast (e.g., command not found)
		time.Sleep(200 * time.Millisecond)
		if cmd.ProcessState != nil && !cmd.ProcessState.Success() {
			cancel()
			logging.Debug("Opener exited with error", "opener", opener.name)
			result.Error = fmt.Errorf("%s exited with error", opener.name)
			continue
		}

		logging.Info("URL opened successfully", "opener", opener.name, "url", rawURL)
		result.Opened = true
		result.Method = opener.name
		cancel()
		break
	}

	// Try to copy to clipboard
	result.Clipboard = copyToClipboard(rawURL)

	if !result.Opened {
		logging.Warn("Could not open URL in browser", "url", rawURL, "last_error", result.Error)
	}

	return result
}

// copyToClipboard tries wl-copy (Wayland), then xclip, then xsel.
func copyToClipboard(text string) bool {
	clippers := []struct {
		name string
		cmd  string
		args []string
	}{
		{"wl-copy", "wl-copy", []string{text}},
		{"xclip", "xclip", []string{"-selection", "clipboard"}},
		{"xsel", "xsel", []string{"--clipboard", "--input"}},
	}

	for _, c := range clippers {
		path, err := exec.LookPath(c.cmd)
		if err != nil {
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		cmd := exec.CommandContext(ctx, path, c.args...)

		// xclip and xsel read from stdin
		if c.cmd != "wl-copy" {
			cmd.Stdin = strings.NewReader(text)
		}

		err = cmd.Run()
		cancel()

		if err == nil {
			logging.Debug("Copied URL to clipboard", "tool", c.name)
			return true
		}
		logging.Debug("Clipboard tool failed", "tool", c.name, "error", err)
	}

	return false
}

// DiagnoseOpeners returns a human-readable report of available URL openers and clipboard tools.
func DiagnoseOpeners() string {
	var lines []string

	tools := []string{"xdg-open", "gio", "wl-copy", "xclip", "xsel"}
	for _, t := range tools {
		path, err := exec.LookPath(t)
		if err != nil {
			lines = append(lines, fmt.Sprintf("  %s: NOT FOUND", t))
		} else {
			lines = append(lines, fmt.Sprintf("  %s: %s", t, path))
		}
	}

	browser := os.Getenv("BROWSER")
	if browser == "" {
		lines = append(lines, "  $BROWSER: not set")
	} else {
		lines = append(lines, fmt.Sprintf("  $BROWSER: %s", browser))
	}

	return strings.Join(lines, "\n")
}
