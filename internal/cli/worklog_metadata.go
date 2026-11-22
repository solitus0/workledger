package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/solitus0/workledger/internal/config"
	"github.com/solitus0/workledger/internal/worklogs"
)

type worklogMetadataFlags struct {
	inline string
	file   string
	clear  bool
}

func (flags *worklogMetadataFlags) register(cmd *cobra.Command, allowClear bool) {
	cmd.Flags().StringVar(&flags.inline, "metadata", "", "Store one JSON value as local metadata")
	cmd.Flags().StringVar(&flags.file, "metadata-file", "", "Read local metadata JSON from a file or - for stdin")
	if allowClear {
		cmd.Flags().BoolVar(&flags.clear, "clear-metadata", false, "Unset local metadata")
	}
}

func (flags worklogMetadataFlags) read(cmd *cobra.Command) (*worklogs.Metadata, error) {
	inlineSet := cmd.Flags().Changed("metadata")
	fileSet := cmd.Flags().Changed("metadata-file")
	if inlineSet && fileSet {
		return nil, errors.New("choose either --metadata or --metadata-file")
	}
	if flags.clear && (inlineSet || fileSet) {
		return nil, errors.New("use --clear-metadata without --metadata or --metadata-file")
	}
	if flags.clear {
		return new(worklogs.Metadata), nil
	}
	if !inlineSet && !fileSet {
		return nil, nil
	}
	input := flags.inline
	source := "--metadata"
	if fileSet {
		source = "--metadata-file"
		var raw []byte
		var err error
		if flags.file == "-" {
			raw, err = io.ReadAll(cmd.InOrStdin())
		} else {
			raw, err = os.ReadFile(flags.file)
		}
		if err != nil {
			return nil, fmt.Errorf("cannot read --metadata-file: %w", err)
		}
		input = string(raw)
	}
	metadata, err := worklogs.ParseMetadata(input)
	if err != nil {
		return nil, fmt.Errorf("%s %w", source, err)
	}
	return &metadata, nil
}

func (a *app) worklogMetadataInputError(mode string, err error) error {
	return a.reportDiagnostic(mode, commandDiagnostic{
		Code: "validation_error", ExitCode: 2, Message: err.Error(),
	})
}

func (a *app) setWorklogMetadataFlagErrors(cmd *cobra.Command) {
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		var flag *pflag.Flag
		var missing *pflag.ValueRequiredError
		var invalid *pflag.InvalidValueError
		problem := "has an invalid value"
		switch {
		case errors.As(err, &missing):
			flag, problem = missing.GetFlag(), "requires a value"
		case errors.As(err, &invalid):
			flag = invalid.GetFlag()
		}
		if flag != nil {
			switch flag.Name {
			case "metadata", "metadata-file", "clear-metadata", "metadata-path", "metadata-value":
				mode := config.DetectOutputMode(diagnosticOutputFlag(a.args))
				return a.worklogMetadataInputError(mode, fmt.Errorf("--%s %s", flag.Name, problem))
			}
		}
		return err
	})
}

func includeWorklogMetadata(record map[string]any, metadata worklogs.Metadata) map[string]any {
	if metadata != "" {
		record["metadata"] = json.RawMessage(metadata)
	}
	return record
}

func metadataText(metadata worklogs.Metadata) string {
	if metadata == "" {
		return "<unset>"
	}
	compact, _ := json.Marshal(json.RawMessage(metadata))
	return string(compact)
}
