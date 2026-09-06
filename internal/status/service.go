package status

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"time"

	clockifyadapter "github.com/solitus0/workledger/internal/adapter/clockify"
	jiracloudadapter "github.com/solitus0/workledger/internal/adapter/jiracloud"
	jiradataadapter "github.com/solitus0/workledger/internal/adapter/jiradatacenter"
	"github.com/solitus0/workledger/internal/config"
	sqlitestore "github.com/solitus0/workledger/internal/store/sqlite"
	"github.com/solitus0/workledger/internal/worklogs"
)

type FailureKind string

const (
	FailureNone       FailureKind = ""
	FailureInternal   FailureKind = "internal"
	FailureValidation FailureKind = "validation"
	FailureNotFound   FailureKind = "not_found"
	FailureAuth       FailureKind = "auth"
	FailureRemote     FailureKind = "remote"
)

type Item struct {
	Category    string
	Target      string
	Status      string
	Message     string
	FailureKind FailureKind
}

type Report struct {
	Items         []Item
	ConfigSummary *config.ConfigSummary
	ConfigIssues  []config.ValidationIssue
}

type Dependencies struct {
	ValidateConfig    func() (config.EffectiveConfig, []config.ValidationIssue, error)
	CheckStorage      func(path, operation string) error
	CheckConnectivity func(context.Context, config.EffectiveConfig) []Item
	Now               func() time.Time
}

type Service struct {
	deps Dependencies
}

func NewService() *Service {
	return NewServiceWith(Dependencies{})
}

func NewServiceWith(deps Dependencies) *Service {
	if deps.ValidateConfig == nil {
		deps.ValidateConfig = config.ValidateExisting
	}
	if deps.CheckStorage == nil {
		deps.CheckStorage = checkStorage
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.CheckConnectivity == nil {
		deps.CheckConnectivity = func(ctx context.Context, cfg config.EffectiveConfig) []Item {
			return checkConnectivity(ctx, cfg, deps.Now)
		}
	}
	return &Service{deps: deps}
}

func checkStorage(path, operation string) error {
	if err := sqlitestore.CheckWritable(path, operation); err != nil {
		return err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	store, err := sqlitestore.OpenExistingReadOnly(path)
	if err != nil {
		return err
	}
	return store.Close()
}

func (s *Service) Check(ctx context.Context) (Report, error) {
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	effective, issues, err := s.deps.ValidateConfig()
	if err != nil {
		return Report{}, err
	}

	report := Report{ConfigIssues: append([]config.ValidationIssue(nil), issues...)}
	items := make([]Item, 0)
	configValid := len(issues) == 0
	if configValid {
		summary := config.Summary(effective)
		report.ConfigSummary = &summary
		items = append(items, Item{Category: "local", Target: "config", Status: "ok", Message: "config is valid"})
		if err := s.deps.CheckStorage(effective.SQLitePath, "status"); err != nil {
			items = append(items, Item{Category: "local", Target: "storage", Status: "error", Message: err.Error(), FailureKind: FailureInternal})
		} else {
			items = append(items, Item{Category: "local", Target: "storage", Status: "ok", Message: "local SQLite storage is writable"})
		}
	} else {
		items = append(items, Item{Category: "local", Target: "config", Status: "error", Message: "config validation failed", FailureKind: FailureValidation})
	}

	if !configValid {
		items = append(items, Item{Category: "env", Target: "config", Status: "skipped", Message: "config validation failed"})
	} else {
		for _, ref := range config.EnvReferences(effective) {
			item := Item{Category: "env", Target: ref.Name, Status: "ok", Message: "set"}
			if !ref.IsSet {
				item.Status = "error"
				item.Message = "missing"
				item.FailureKind = FailureValidation
			}
			items = append(items, item)
		}
	}

	if !configValid {
		items = append(items, Item{Category: "routing", Target: "config", Status: "skipped", Message: "config validation failed"})
	} else {
		rules := config.RouteRules(effective)
		message := "no routing rules configured"
		if len(rules) > 0 {
			message = fmt.Sprintf("%d routing rules configured", len(rules))
		}
		items = append(items, Item{Category: "routing", Target: "rules", Status: "ok", Message: message})
		audit := config.AuditClockifyMappings(effective)
		for _, prefix := range audit.MissingPrefixes {
			items = append(items, Item{Category: "routing", Target: prefix, Status: "warning", Message: "routed prefix has no Clockify project mapping"})
		}
		for _, prefix := range audit.OrphanedPrefixes {
			items = append(items, Item{Category: "routing", Target: prefix, Status: "warning", Message: "Clockify project mapping is not referenced by Jira routing"})
		}
	}

	if !configValid {
		items = append(items, Item{Category: "connectivity", Target: "config", Status: "skipped", Message: "config validation failed"})
	} else {
		if err := ctx.Err(); err != nil {
			return Report{}, err
		}
		items = append(items, s.deps.CheckConnectivity(ctx, effective)...)
		if err := ctx.Err(); err != nil {
			return Report{}, err
		}
	}
	report.Items = items
	return report, nil
}

func checkConnectivity(ctx context.Context, cfg config.EffectiveConfig, now func() time.Time) []Item {
	items := make([]Item, 0)
	if cfg.File.Clockify != nil {
		items = append(items, checkClockify(ctx, cfg, now))
	}
	items = append(items, checkJiraCloud(ctx, cfg)...)
	items = append(items, checkJiraData(ctx, cfg)...)
	return items
}

func checkClockify(ctx context.Context, cfg config.EffectiveConfig, now func() time.Time) Item {
	name, instance, err := config.ResolveClockifyInstance(cfg, "")
	target := "clockify"
	if instance.WorkspaceID != "" {
		target += ":" + instance.WorkspaceID
	} else if name != "" {
		target += ":" + name
	}
	if err != nil {
		return connectivityError(target, err, "clockify")
	}
	client := clockifyadapter.NewClient(instance.Auth.APIKey)
	user, err := client.CurrentUser(ctx)
	if err != nil {
		return connectivityError(target, err, "clockify")
	}
	if user.ID != instance.UserID {
		return Item{Category: "connectivity", Target: target, Status: "error", Message: "configured clockify.user_id does not match authenticated user", FailureKind: FailureValidation}
	}
	if instance.WorkspaceID != user.ActiveWorkspace && instance.WorkspaceID != user.DefaultWorkspace {
		return Item{Category: "connectivity", Target: target, Status: "error", Message: "configured clockify.workspace_id is not visible for the authenticated user", FailureKind: FailureValidation}
	}
	from, to, err := worklogs.ResolveDateWindowSelectionAt(cfg, worklogs.DateWindowSelection{CurrentWeek: true}, now)
	if err != nil {
		return connectivityError(target, err, "clockify")
	}
	if _, err := client.ListUserTimeEntries(ctx, instance.WorkspaceID, instance.UserID, *from, *to); err != nil {
		return connectivityError(target, err, "clockify")
	}
	if _, err := client.ListTags(ctx, instance.WorkspaceID); err != nil {
		return connectivityError(target, err, "clockify")
	}
	return Item{Category: "connectivity", Target: target, Status: "ok", Message: instance.UserID}
}

func checkJiraCloud(ctx context.Context, cfg config.EffectiveConfig) []Item {
	if cfg.File.JiraCloud == nil {
		return nil
	}
	names := sortedKeys(cfg.File.JiraCloud.Instances)
	items := make([]Item, 0, len(names))
	for _, name := range names {
		target := "jira-cloud:" + name
		_, instance, err := config.ResolveJiraCloudInstance(cfg, name)
		if err != nil {
			items = append(items, connectivityError(target, err, "jira-cloud"))
			continue
		}
		user, err := jiracloudadapter.NewClient(instance.BaseURL, instance.Auth.Email, instance.Auth.Token).CurrentUser(ctx)
		if err != nil {
			items = append(items, connectivityError(target, err, "jira-cloud"))
			continue
		}
		items = append(items, Item{Category: "connectivity", Target: target, Status: "ok", Message: firstNonEmpty(user.DisplayName, user.EmailAddress, user.Name, user.Key, user.AccountID)})
	}
	return items
}

func checkJiraData(ctx context.Context, cfg config.EffectiveConfig) []Item {
	if cfg.File.JiraData == nil {
		return nil
	}
	names := sortedKeys(cfg.File.JiraData.Instances)
	items := make([]Item, 0, len(names))
	for _, name := range names {
		target := "jira-data-center:" + name
		_, instance, err := config.ResolveJiraDataInstance(cfg, name)
		if err != nil {
			items = append(items, connectivityError(target, err, "jira-data-center"))
			continue
		}
		user, err := jiradataadapter.NewClient(instance.BaseURL, instance.Auth.Bearer.Token).CurrentUser(ctx)
		if err != nil {
			items = append(items, connectivityError(target, err, "jira-data-center"))
			continue
		}
		items = append(items, Item{Category: "connectivity", Target: target, Status: "ok", Message: firstNonEmpty(user.DisplayName, user.EmailAddress, user.Name, user.Key, user.AccountID)})
	}
	return items
}

func connectivityError(target string, err error, family string) Item {
	kind := FailureInternal
	var clockifyErr *clockifyadapter.RequestError
	var cloudErr *jiracloudadapter.RequestError
	var dataErr *jiradataadapter.RequestError
	switch {
	case family == "clockify" && errors.As(err, &clockifyErr):
		kind = httpFailureKind(clockifyErr.StatusCode)
	case family == "jira-cloud" && errors.As(err, &cloudErr):
		kind = httpFailureKind(cloudErr.StatusCode)
	case family == "jira-data-center" && errors.As(err, &dataErr):
		kind = httpFailureKind(dataErr.StatusCode)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		kind = FailureInternal
	default:
		kind = FailureValidation
	}
	return Item{Category: "connectivity", Target: target, Status: "error", Message: err.Error(), FailureKind: kind}
}

func httpFailureKind(statusCode int) FailureKind {
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return FailureAuth
	case http.StatusNotFound:
		return FailureNotFound
	default:
		return FailureRemote
	}
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
