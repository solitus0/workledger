package issues

import (
	"context"
	"errors"
	"strings"
	"time"

	jiracloudadapter "github.com/solitus0/workledger/internal/adapter/jiracloud"
	jiradataadapter "github.com/solitus0/workledger/internal/adapter/jiradatacenter"
	"github.com/solitus0/workledger/internal/config"
	"github.com/solitus0/workledger/internal/worklogs"
)

var ErrValidation = errors.New("issue metadata refresh validation failed")

type ValidationError struct {
	Message string
}

type RemoteError struct {
	Adapter string
	Err     error
}

func (e *RemoteError) Error() string {
	return e.Err.Error()
}

func (e *RemoteError) Unwrap() error {
	return e.Err
}

func (e ValidationError) Error() string {
	return e.Message
}

func (e ValidationError) Unwrap() error {
	return ErrValidation
}

type EstimateClient interface {
	OriginalEstimateSeconds(context.Context, string) (*int64, error)
}

type Dependencies struct {
	JiraCloudClient func(config.JiraCloudInstance) EstimateClient
	JiraDataClient  func(config.JiraDataCenterInstance) EstimateClient
	Now             func() time.Time
}

type Service struct {
	worklogs *worklogs.Service
	deps     Dependencies
}

type RefreshInput struct {
	Adapter  string
	Instance string
	Field    string
	Filters  worklogs.ListFilters
}

type RefreshResult struct {
	Adapter  string
	Instance string
	Field    string
	Items    []worklogs.IssueMetadata
}

func NewService(worklogService *worklogs.Service) *Service {
	return NewServiceWith(worklogService, Dependencies{})
}

func NewServiceWith(worklogService *worklogs.Service, deps Dependencies) *Service {
	if deps.JiraCloudClient == nil {
		deps.JiraCloudClient = func(cfg config.JiraCloudInstance) EstimateClient {
			return jiraCloudEstimateClient{client: jiracloudadapter.NewClient(cfg.BaseURL, cfg.Auth.Email, cfg.Auth.Token)}
		}
	}
	if deps.JiraDataClient == nil {
		deps.JiraDataClient = func(cfg config.JiraDataCenterInstance) EstimateClient {
			return jiraDataEstimateClient{client: jiradataadapter.NewClient(cfg.BaseURL, cfg.Auth.Bearer.Token)}
		}
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &Service{worklogs: worklogService, deps: deps}
}

func (s *Service) Refresh(ctx context.Context, cfg config.EffectiveConfig, input RefreshInput) (RefreshResult, error) {
	if input.Field != "max-estimate" {
		return RefreshResult{}, validationError("only --field=max-estimate is supported in this slice")
	}
	if err := ctx.Err(); err != nil {
		return RefreshResult{}, err
	}

	active, _, err := s.worklogs.List(ctx, cfg, input.Filters)
	if err != nil {
		return RefreshResult{}, err
	}
	keys := issueKeys(active)
	result := RefreshResult{Adapter: input.Adapter, Field: input.Field}

	var client EstimateClient
	var prefixes []string
	switch input.Adapter {
	case "jira-cloud":
		name, instance, err := config.ResolveJiraCloudInstance(cfg, input.Instance)
		if err != nil {
			return RefreshResult{}, validationError(err.Error())
		}
		result.Instance = name
		if configured := cfg.File.JiraCloud.Instances[name]; configured.Routing != nil {
			prefixes, err = config.JiraCloudIssuePrefixes(cfg, name)
			if err != nil {
				return RefreshResult{}, validationError(err.Error())
			}
		}
		client = s.deps.JiraCloudClient(instance)
	case "jira-data-center":
		name, instance, err := config.ResolveJiraDataInstance(cfg, input.Instance)
		if err != nil {
			return RefreshResult{}, validationError(err.Error())
		}
		result.Instance = name
		if configured := cfg.File.JiraData.Instances[name]; configured.Routing != nil {
			prefixes, err = config.JiraDataIssuePrefixes(cfg, name)
			if err != nil {
				return RefreshResult{}, validationError(err.Error())
			}
		}
		client = s.deps.JiraDataClient(instance)
	default:
		return RefreshResult{}, validationError("supported adapters are jira-cloud and jira-data-center")
	}

	keys = filterIssueKeys(keys, prefixes)
	if len(keys) == 0 {
		result.Items = []worklogs.IssueMetadata{}
		return result, nil
	}

	refreshedAt := s.deps.Now().UTC()
	items := make([]worklogs.IssueMetadata, 0, len(keys))
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return RefreshResult{}, err
		}
		estimate, err := client.OriginalEstimateSeconds(ctx, key)
		if err != nil {
			return RefreshResult{}, &RemoteError{Adapter: input.Adapter, Err: err}
		}
		items = append(items, worklogs.IssueMetadata{
			IssueKey:            key,
			MaxEstimateSeconds:  estimate,
			SourceAdapterFamily: input.Adapter,
			SourceAdapterInst:   result.Instance,
			RefreshedAt:         refreshedAt,
		})
	}
	if err := ctx.Err(); err != nil {
		return RefreshResult{}, err
	}
	if err := s.worklogs.UpsertIssueMetadataBatch(ctx, items); err != nil {
		return RefreshResult{}, err
	}
	result.Items = items
	return result, nil
}

type jiraCloudEstimateClient struct {
	client *jiracloudadapter.Client
}

func (c jiraCloudEstimateClient) OriginalEstimateSeconds(ctx context.Context, key string) (*int64, error) {
	item, err := c.client.GetIssue(ctx, key, []string{"timetracking"})
	if err != nil || item.Fields.Timetracking == nil {
		return nil, err
	}
	return item.Fields.Timetracking.OriginalEstimateSeconds, nil
}

type jiraDataEstimateClient struct {
	client *jiradataadapter.Client
}

func (c jiraDataEstimateClient) OriginalEstimateSeconds(ctx context.Context, key string) (*int64, error) {
	item, err := c.client.GetIssue(ctx, key, []string{"timetracking"})
	if err != nil || item.Fields.Timetracking == nil {
		return nil, err
	}
	return item.Fields.Timetracking.OriginalEstimateSeconds, nil
}

func issueKeys(items []worklogs.LocalWorklog) []string {
	keys := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if _, ok := seen[item.IssueKey]; ok {
			continue
		}
		seen[item.IssueKey] = struct{}{}
		keys = append(keys, item.IssueKey)
	}
	return keys
}

func filterIssueKeys(keys, prefixes []string) []string {
	if len(prefixes) == 0 {
		return keys
	}
	filtered := make([]string, 0, len(keys))
	for _, key := range keys {
		for _, prefix := range prefixes {
			if strings.HasPrefix(key, prefix+"-") {
				filtered = append(filtered, key)
				break
			}
		}
	}
	return filtered
}

func validationError(message string) error {
	return ValidationError{Message: message}
}
