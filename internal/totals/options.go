package totals

import "github.com/solitus0/workledger/internal/progress"

type Options struct {
	Reporter progress.Reporter
	Instance string
}

func resolveOptions(options []Options) Options {
	if len(options) == 0 {
		return Options{Reporter: progress.NewNoopReporter()}
	}
	resolved := options[0]
	if resolved.Reporter == nil {
		resolved.Reporter = progress.NewNoopReporter()
	}
	return resolved
}
