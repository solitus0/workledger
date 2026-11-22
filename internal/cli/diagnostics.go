package cli

import (
	"encoding/json"
	"fmt"
	"strings"
)

type commandDiagnostic struct {
	Code     string `json:"code"`
	ExitCode int    `json:"exit_code"`
	Message  string `json:"message"`
	Run      string `json:"run,omitempty"`
	Details  any    `json:"details,omitempty"`
}

func (a *app) reportDiagnostic(mode string, diagnostic commandDiagnostic) error {
	a.activityErrorCode = diagnostic.Code
	a.activityErrorMessage = safeActivityErrorMessage(diagnostic.Code)
	if mode == "json" {
		_ = json.NewEncoder(a.stderr).Encode(map[string]commandDiagnostic{"error": diagnostic})
	} else {
		_, _ = fmt.Fprintln(a.stderr, "Error:", diagnostic.Message)
		if diagnostic.Run != "" {
			_, _ = fmt.Fprintln(a.stderr, "Run:", diagnostic.Run)
		}
	}
	return exitError{code: diagnostic.ExitCode}
}

// Recover the output flag without parsing the flags that caused the error.
func diagnosticOutputFlag(args []string) string {
	var requested string
	for index, arg := range args {
		if arg == "--" {
			break
		}
		if value, ok := strings.CutPrefix(arg, "--output="); ok {
			requested = value
		} else if arg == "--output" && index+1 < len(args) && !strings.HasPrefix(args[index+1], "-") {
			requested = args[index+1]
		}
	}
	return requested
}
