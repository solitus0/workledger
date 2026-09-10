package cli

import (
	"context"
	"errors"

	workledgertui "github.com/solitus0/workledger/internal/tui"
	"github.com/spf13/cobra"
)

var runTUI = workledgertui.Run

func (a *app) newTUICommand() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Open the interactive status and worklog workspace",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 0 {
				return a.fail("table", 2, "validation_error", "tui accepts no arguments", nil)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if flag := cmd.Flag("output"); flag != nil && flag.Changed {
				return a.fail("table", 2, "validation_error", "tui does not support --output", nil)
			}
			if !isTTYReader(a.stdin) || !isTTYWriter(a.stdout) {
				return a.fail("table", 2, "validation_error", "tui requires interactive stdin and stdout terminals", nil)
			}
			if err := runTUI(cmd.Context(), a.stdin, a.stdout, workledgertui.DefaultTheme()); err != nil {
				if errors.Is(err, context.Canceled) {
					return exitError{code: 130}
				}
				return a.fail("table", 1, "unexpected_error", err.Error(), nil)
			}
			return nil
		},
	}
}
