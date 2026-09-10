package tui

import (
	"context"
	"errors"
	"io"
	"os"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/solitus0/workledger/internal/activity"
	"github.com/solitus0/workledger/internal/config"
	"github.com/solitus0/workledger/internal/presets"
	"github.com/solitus0/workledger/internal/reconcile"
	statusservice "github.com/solitus0/workledger/internal/status"
	sqlitestore "github.com/solitus0/workledger/internal/store/sqlite"
	"github.com/solitus0/workledger/internal/worklogs"
)

type statusChecker interface {
	Check(context.Context) (statusservice.Report, error)
}

type worklogQueries interface {
	Context(context.Context, config.EffectiveConfig, worklogs.ContextInput) (worklogs.ContextResult, error)
	ActiveIssueTotalSeconds(context.Context, string) (int, error)
	ListKnownIssueKeys(context.Context, string, int) ([]string, error)
	ListRecentDescriptions(context.Context, string, int) ([]string, error)
	LookupIssueMetadata(context.Context, []string) (map[string]worklogs.IssueMetadata, error)
	PreviewAdd(context.Context, config.EffectiveConfig, worklogs.AddInput) (worklogs.AddResult, error)
}

type worklogMutations interface {
	Add(context.Context, config.EffectiveConfig, worklogs.AddInput) (worklogs.AddResult, error)
	Update(context.Context, config.EffectiveConfig, string, worklogs.PatchInput) (worklogs.LocalWorklog, error)
	Delete(context.Context, string, int64) (worklogs.DeleteResult, error)
	DeleteBatchExpected(context.Context, config.EffectiveConfig, worklogs.ListFilters, []worklogs.DeleteExpectation) (worklogs.DeleteBatchResult, error)
}

type trashService interface {
	ListTrash(config.EffectiveConfig, worklogs.TrashFilters) ([]worklogs.TrashRecord, worklogs.EffectiveFilters, error)
	RestoreTrash(context.Context, config.EffectiveConfig, string) (worklogs.TrashRestoreItem, error)
	RestoreTrashBatchExpected(context.Context, config.EffectiveConfig, worklogs.TrashFilters, []string) (worklogs.TrashRestoreResult, error)
}

type presetService interface {
	List(context.Context, string, int) ([]presets.Preset, error)
	Create(context.Context, config.EffectiveConfig, presets.CreateInput) (presets.Preset, error)
	Update(context.Context, config.EffectiveConfig, string, presets.PatchInput) (presets.Preset, error)
	Delete(context.Context, string, int64) (presets.DeleteResult, error)
	ApplyDraft(context.Context, config.EffectiveConfig, string, int64, worklogs.AddInput) (worklogs.AddResult, error)
}

type activityService interface {
	Start(context.Context, activity.StartInput) (activity.Entry, error)
	Finish(context.Context, string, activity.FinishInput) (activity.Entry, error)
	List(context.Context, activity.ListFilters) ([]activity.Entry, error)
}

type planService interface {
	ListRecentPlansInCreatedRange(int, time.Time, time.Time) ([]reconcile.ListEntry, error)
	LoadPlan(string) (reconcile.Plan, error)
	Reconcile(context.Context, config.EffectiveConfig, reconcile.ReconcileRequest, ...reconcile.PlanOptions) (reconcile.ReconcileResult, error)
	ApplyPlan(context.Context, config.EffectiveConfig, string, ...reconcile.ApplyOptions) (reconcile.ApplyResult, error)
	RetryPlan(context.Context, config.EffectiveConfig, string, string, ...reconcile.ApplyOptions) (reconcile.ApplyResult, error)
}

type changeTracker interface {
	Poll(context.Context) (bool, error)
	Close() error
}

type workspace struct {
	cfg             config.EffectiveConfig
	worklogs        worklogQueries
	mutations       worklogMutations
	trash           trashService
	presets         presetService
	activities      activityService
	plans           planService
	tracker         changeTracker
	activityTracker changeTracker
	sqlitePath      string
	close           func() error
}

type dependencies struct {
	status            statusChecker
	openWorkspace     func(context.Context) (*workspace, error)
	checkWritable     func(string, string) error
	now               func() time.Time
	noColor           bool
	waitForBackground bool
	theme             Theme
}

func defaultDependencies(theme Theme) dependencies {
	return dependencies{
		status:            statusservice.NewService(),
		openWorkspace:     openWorkspace,
		checkWritable:     sqlitestore.CheckWritable,
		now:               time.Now,
		noColor:           os.Getenv("NO_COLOR") != "",
		waitForBackground: true,
		theme:             theme,
	}
}

func openWorkspace(ctx context.Context) (*workspace, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cfg, err := config.LoadEffective()
	if err != nil {
		return nil, err
	}
	if err := sqlitestore.CheckWritable(cfg.SQLitePath, "tui"); err != nil {
		return nil, err
	}
	store, err := sqlitestore.OpenExisting(cfg.SQLitePath)
	if err != nil {
		return nil, err
	}
	tracker, err := store.NewChangeTracker(ctx)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	activityTracker, err := store.NewActivityChangeTracker(ctx)
	if err != nil {
		_ = tracker.Close()
		_ = store.Close()
		return nil, err
	}
	worklogService := worklogs.NewService(store)
	return &workspace{
		cfg:             cfg,
		worklogs:        worklogService,
		mutations:       worklogService,
		trash:           worklogService,
		presets:         presets.NewService(store),
		activities:      activity.NewService(store),
		plans:           reconcile.NewService(store),
		tracker:         tracker,
		activityTracker: activityTracker,
		sqlitePath:      cfg.SQLitePath,
		close: func() error {
			trackerErr := tracker.Close()
			activityTrackerErr := activityTracker.Close()
			return errors.Join(trackerErr, activityTrackerErr, store.Close())
		},
	}, nil
}

// Run starts the interactive Workledger frontend.
func Run(ctx context.Context, input io.Reader, output io.Writer, theme Theme) error {
	deps := defaultDependencies(theme)
	m := newModel(ctx, deps, nil, nil)
	profile := colorprofile.Detect(output, os.Environ())
	if deps.noColor {
		profile = colorprofile.ASCII
	} else if profile <= colorprofile.ASCII {
		profile = colorprofile.ANSI256
	}
	program := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(input), tea.WithOutput(output), tea.WithColorProfile(profile))
	final, err := program.Run()
	if finalModel, ok := final.(model); ok {
		finalModel.close()
	}
	if errors.Is(err, tea.ErrInterrupted) || errors.Is(err, tea.ErrProgramKilled) || errors.Is(ctx.Err(), context.Canceled) {
		return context.Canceled
	}
	return err
}

type formState struct {
	inputs                   [4]textinput.Model
	focus                    formField
	editing                  bool
	id                       string
	revision                 int64
	stale                    bool
	force                    bool
	placement                placementMode
	overtime                 bool
	noLunch                  bool
	previewGeneration        int
	previewLoading           bool
	previewRecords           []worklogs.LocalWorklog
	previewError             string
	previewConflict          string
	previewOvertimeAvailable bool
	suggestedIssue           string
	suggestedStart           string
	suggestedDescription     string
	sourcePresetID           string
	sourcePresetRevision     int64
}

type presetFormState struct {
	inputs   [5]textinput.Model
	focus    int
	editing  bool
	name     string
	revision int64
	stale    bool
}

type presetPickerState struct {
	input     textinput.Model
	items     []presets.Preset
	selected  int
	searching bool
}

type placementMode int

const (
	manualPlacement placementMode = iota
	fitPlacement
	fillPlacement
)

func (p placementMode) String() string {
	switch p {
	case fitPlacement:
		return "Fit"
	case fillPlacement:
		return "Fill"
	default:
		return "Manual"
	}
}

type formField int

const (
	placementField formField = iota
	issueField
	startField
	durationField
	descriptionField
)

const (
	issueInput = iota
	startInput
	durationInput
	descriptionInput
)
