package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type EnvVarRef struct {
	Name  string
	IsSet bool
}

type RouteRule struct {
	AdapterFamily string
	Instance      string
	Profile       string
	Mode          string
	SourcePrefix  string
	TargetIssue   string
}

type ClockifyProjectResolution struct {
	ProjectName  string
	SourcePrefix string
	UsedDefault  bool
}

type RouteExplanation struct {
	IssueKey         string
	Result           string
	OwnershipMatches []RouteRule
	ReportingMatches []RouteRule
	ClockifyProject  *ClockifyProjectResolution
}

type ConfigSummary struct {
	ConfigPath               string
	DefaultOutput            string
	SQLitePath               string
	LocalTimezone            string
	MinimumDurationSeconds   int
	DailyMinimumQuotaSeconds int
	DailyLunch               string
	JiraInstanceCount        int
	UniqueEnvVarCount        int
	MissingEnvVarCount       int
	UniqueRoutedPrefixCount  int
	ReportingTargetCount     int
	ClockifyMappingCount     int
}

type ClockifyMappingAudit struct {
	RoutedPrefixes   []string
	MappedPrefixes   []string
	MissingPrefixes  []string
	OrphanedPrefixes []string
}

type SetupJiraCloudParams struct {
	Instance      string
	BaseURL       string
	Email         string
	TokenEnv      string
	IssuePrefixes []string
}

type SetupJiraDataParams struct {
	Instance      string
	BaseURL       string
	TokenEnv      string
	IssuePrefixes []string
}

type SetupClockifyParams struct {
	WorkspaceID string
	UserID      string
	APIKeyEnv   string
	ProjectMap  map[string]string
}

func EnvReferences(cfg EffectiveConfig) []EnvVarRef {
	refs := map[string]bool{}
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		refs[name] = true
	}

	if cfg.File.Clockify != nil {
		add(cfg.File.Clockify.Auth.APIKeyEnv)
	}
	if cfg.File.JiraCloud != nil {
		for _, instance := range cfg.File.JiraCloud.Instances {
			add(instance.Auth.TokenEnv)
		}
	}
	if cfg.File.JiraData != nil {
		for _, instance := range cfg.File.JiraData.Instances {
			add(instance.Auth.Bearer.TokenEnv)
		}
	}

	names := make([]string, 0, len(refs))
	for name := range refs {
		names = append(names, name)
	}
	sort.Strings(names)

	items := make([]EnvVarRef, 0, len(names))
	for _, name := range names {
		_, ok := os.LookupEnv(name)
		items = append(items, EnvVarRef{Name: name, IsSet: ok})
	}
	return items
}

func Summary(cfg EffectiveConfig) ConfigSummary {
	rules := RouteRules(cfg)
	prefixes := map[string]struct{}{}
	reportingTargets := 0
	for _, rule := range rules {
		prefixes[rule.SourcePrefix] = struct{}{}
		if rule.Mode == "reporting" {
			reportingTargets++
		}
	}

	envRefs := EnvReferences(cfg)
	missingEnv := 0
	for _, ref := range envRefs {
		if !ref.IsSet {
			missingEnv++
		}
	}

	jiraInstances := 0
	if cfg.File.JiraCloud != nil {
		jiraInstances += len(cfg.File.JiraCloud.Instances)
	}
	if cfg.File.JiraData != nil {
		jiraInstances += len(cfg.File.JiraData.Instances)
	}

	clockifyMappings := 0
	if cfg.File.Clockify != nil && cfg.File.Clockify.ProjectMapping != nil {
		clockifyMappings = len(cfg.File.Clockify.ProjectMapping.IssuePrefixes)
	}

	return ConfigSummary{
		ConfigPath:               cfg.ConfigPath,
		DefaultOutput:            cfg.DefaultOutput,
		SQLitePath:               cfg.SQLitePath,
		LocalTimezone:            cfg.TimezoneName,
		MinimumDurationSeconds:   cfg.MinimumDurationSeconds,
		DailyMinimumQuotaSeconds: cfg.DailyMinimumQuotaSeconds,
		DailyLunch:               cfg.DailyLunch,
		JiraInstanceCount:        jiraInstances,
		UniqueEnvVarCount:        len(envRefs),
		MissingEnvVarCount:       missingEnv,
		UniqueRoutedPrefixCount:  len(prefixes),
		ReportingTargetCount:     reportingTargets,
		ClockifyMappingCount:     clockifyMappings,
	}
}

func RouteRules(cfg EffectiveConfig) []RouteRule {
	rules := make([]RouteRule, 0)
	appendRules := func(adapter string, instances map[string]JiraInstanceRoutesOwner) {
		instanceNames := make([]string, 0, len(instances))
		for name := range instances {
			instanceNames = append(instanceNames, name)
		}
		sort.Strings(instanceNames)
		for _, instanceName := range instanceNames {
			routing := instances[instanceName].RoutingConfig()
			if routing == nil {
				continue
			}
			profileNames := make([]string, 0, len(routing.Profiles))
			for name := range routing.Profiles {
				profileNames = append(profileNames, name)
			}
			sort.Strings(profileNames)
			for _, profileName := range profileNames {
				profile := routing.Profiles[profileName]
				prefixes := append([]string(nil), profile.IssuePrefixes...)
				sort.Strings(prefixes)
				for _, prefix := range prefixes {
					rules = append(rules, RouteRule{
						AdapterFamily: adapter,
						Instance:      instanceName,
						Profile:       profileName,
						Mode:          "ownership",
						SourcePrefix:  prefix,
					})
				}

				reportPrefixes := make([]string, 0, len(profile.ReportingTargets))
				for prefix := range profile.ReportingTargets {
					reportPrefixes = append(reportPrefixes, prefix)
				}
				sort.Strings(reportPrefixes)
				for _, prefix := range reportPrefixes {
					rules = append(rules, RouteRule{
						AdapterFamily: adapter,
						Instance:      instanceName,
						Profile:       profileName,
						Mode:          "reporting",
						SourcePrefix:  prefix,
						TargetIssue:   profile.ReportingTargets[prefix],
					})
				}
			}
		}
	}

	if cfg.File.JiraCloud != nil {
		instances := make(map[string]JiraInstanceRoutesOwner, len(cfg.File.JiraCloud.Instances))
		for name, instance := range cfg.File.JiraCloud.Instances {
			instances[name] = jiraCloudRoutesOwner(instance)
		}
		appendRules("jira-cloud", instances)
	}
	if cfg.File.JiraData != nil {
		instances := make(map[string]JiraInstanceRoutesOwner, len(cfg.File.JiraData.Instances))
		for name, instance := range cfg.File.JiraData.Instances {
			instances[name] = jiraDataRoutesOwner(instance)
		}
		appendRules("jira-data-center", instances)
	}
	return rules
}

type JiraInstanceRoutesOwner interface {
	RoutingConfig() *JiraInstanceRoutes
}

type jiraCloudRoutesOwner JiraCloudInstance
type jiraDataRoutesOwner JiraDataCenterInstance

func (o jiraCloudRoutesOwner) RoutingConfig() *JiraInstanceRoutes {
	return JiraCloudInstance(o).Routing
}
func (o jiraDataRoutesOwner) RoutingConfig() *JiraInstanceRoutes {
	return JiraDataCenterInstance(o).Routing
}

func ExplainRoute(cfg EffectiveConfig, issueKey string) RouteExplanation {
	ownership := make([]RouteRule, 0)
	reporting := make([]RouteRule, 0)
	for _, rule := range RouteRules(cfg) {
		if strings.HasPrefix(issueKey, rule.SourcePrefix) {
			if rule.Mode == "ownership" {
				ownership = append(ownership, rule)
			} else {
				reporting = append(reporting, rule)
			}
		}
	}

	result := "unmatched"
	switch total := len(ownership) + len(reporting); {
	case total == 0:
		result = "unmatched"
	case total == 1 && len(ownership) == 1:
		result = "owned"
	case total == 1 && len(reporting) == 1:
		result = "reporting"
	default:
		result = "ambiguous"
	}

	return RouteExplanation{
		IssueKey:         issueKey,
		Result:           result,
		OwnershipMatches: ownership,
		ReportingMatches: reporting,
		ClockifyProject:  ResolveClockifyProject(cfg, issueKey),
	}
}

func ResolveClockifyProject(cfg EffectiveConfig, issueKey string) *ClockifyProjectResolution {
	if cfg.File.Clockify == nil || cfg.File.Clockify.ProjectMapping == nil {
		return nil
	}
	return resolveClockifyProjectMapping(cfg.File.Clockify.ProjectMapping, issueKey)
}

func resolveClockifyProjectMapping(mapping *ClockifyProjectConfig, issueKey string) *ClockifyProjectResolution {
	if mapping == nil {
		return nil
	}
	prefixes := make([]string, 0, len(mapping.IssuePrefixes))
	for prefix := range mapping.IssuePrefixes {
		prefixes = append(prefixes, prefix)
	}
	sort.Strings(prefixes)
	for _, prefix := range prefixes {
		if len(issueKey) > len(prefix) && strings.HasPrefix(issueKey, prefix) {
			return &ClockifyProjectResolution{
				ProjectName:  mapping.IssuePrefixes[prefix],
				SourcePrefix: prefix,
			}
		}
	}
	if strings.TrimSpace(mapping.DefaultProject) == "" {
		return nil
	}
	return &ClockifyProjectResolution{
		ProjectName: mapping.DefaultProject,
		UsedDefault: true,
	}
}

func AuditClockifyMappings(cfg EffectiveConfig) ClockifyMappingAudit {
	routedSet := map[string]struct{}{}
	for _, rule := range RouteRules(cfg) {
		routedSet[rule.SourcePrefix] = struct{}{}
	}

	mappedSet := map[string]struct{}{}
	if cfg.File.Clockify != nil && cfg.File.Clockify.ProjectMapping != nil {
		for prefix := range cfg.File.Clockify.ProjectMapping.IssuePrefixes {
			mappedSet[prefix] = struct{}{}
		}
	}

	audit := ClockifyMappingAudit{
		RoutedPrefixes:   sortedSetKeys(routedSet),
		MappedPrefixes:   sortedSetKeys(mappedSet),
		MissingPrefixes:  make([]string, 0),
		OrphanedPrefixes: make([]string, 0),
	}
	for prefix := range routedSet {
		if _, ok := mappedSet[prefix]; !ok {
			audit.MissingPrefixes = append(audit.MissingPrefixes, prefix)
		}
	}
	for prefix := range mappedSet {
		if _, ok := routedSet[prefix]; !ok {
			audit.OrphanedPrefixes = append(audit.OrphanedPrefixes, prefix)
		}
	}
	sort.Strings(audit.MissingPrefixes)
	sort.Strings(audit.OrphanedPrefixes)
	return audit
}

func ResolveClockifyProjectName(mapping *ClockifyProjectConfig, issueKey string) string {
	resolution := resolveClockifyProjectMapping(mapping, issueKey)
	if resolution == nil {
		return ""
	}
	return resolution.ProjectName
}

func AddJiraCloudInstance(params SetupJiraCloudParams) error {
	instanceNode := mapNode(
		"base_url", scalarNode(strings.TrimRight(params.BaseURL, "/")),
		"auth", mapNode(
			"email", scalarNode(params.Email),
			"token_env", scalarNode(params.TokenEnv),
		),
		"routing", mapNode(
			"profiles", mapNode(
				"default", mapNode(
					"issue_prefixes", sequenceNode(params.IssuePrefixes...),
				),
			),
		),
	)
	return patchConfigFile(func(root *yaml.Node) error {
		instances := ensureMappingPath(root, "jira_cloud", "instances")
		if mappingHasKey(instances, params.Instance) {
			return fmt.Errorf("jira_cloud instance %q already exists", params.Instance)
		}
		setMappingValue(instances, params.Instance, instanceNode)
		return nil
	})
}

func AddJiraDataInstance(params SetupJiraDataParams) error {
	instanceNode := mapNode(
		"base_url", scalarNode(strings.TrimRight(params.BaseURL, "/")),
		"auth", mapNode(
			"bearer", mapNode(
				"token_env", scalarNode(params.TokenEnv),
			),
		),
		"routing", mapNode(
			"profiles", mapNode(
				"default", mapNode(
					"issue_prefixes", sequenceNode(params.IssuePrefixes...),
				),
			),
		),
	)
	return patchConfigFile(func(root *yaml.Node) error {
		instances := ensureMappingPath(root, "jira_data_center", "instances")
		if mappingHasKey(instances, params.Instance) {
			return fmt.Errorf("jira_data_center instance %q already exists", params.Instance)
		}
		setMappingValue(instances, params.Instance, instanceNode)
		return nil
	})
}

func UpsertClockifyConfig(params SetupClockifyParams) error {
	issuePrefixes := make([]string, 0, len(params.ProjectMap))
	for prefix := range params.ProjectMap {
		issuePrefixes = append(issuePrefixes, prefix)
	}
	sort.Strings(issuePrefixes)

	projectMappingPairs := make([]any, 0, len(issuePrefixes)*2)
	for _, prefix := range issuePrefixes {
		projectMappingPairs = append(projectMappingPairs, prefix, scalarNode(params.ProjectMap[prefix]))
	}

	clockifyNode := mapNode(
		"workspace_id", scalarNode(params.WorkspaceID),
		"user_id", scalarNode(params.UserID),
		"auth", mapNode(
			"api_key_env", scalarNode(params.APIKeyEnv),
		),
	)
	if len(projectMappingPairs) > 0 {
		clockifyNode.Content = append(clockifyNode.Content,
			scalarNode("project_mapping"),
			mapNode(
				"issue_prefixes", mapNode(projectMappingPairs...),
			),
		)
	}

	return patchConfigFile(func(root *yaml.Node) error {
		top := documentMap(root)
		if mappingHasKey(top, "clockify") {
			return fmt.Errorf("clockify config already exists")
		}
		setMappingValue(top, "clockify", clockifyNode)
		return nil
	})
}

func patchConfigFile(mutate func(root *yaml.Node) error) error {
	configPath, err := ConfigPath()
	if err != nil {
		return err
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrConfigNotFound
		}
		return err
	}
	if _, err := loadEffective(configPath, data); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return err
	}
	if len(doc.Content) == 0 {
		doc.Content = append(doc.Content, &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"})
	}

	if err := mutate(&doc); err != nil {
		return err
	}

	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(&doc); err != nil {
		return err
	}
	if err := encoder.Close(); err != nil {
		return err
	}

	if issues, _ := validateConfigBytes(buf.Bytes()); len(issues) > 0 {
		return ValidationErrors{Issues: issues}
	}

	dir := filepath.Dir(configPath)
	tmp, err := os.CreateTemp(dir, "config.yaml.*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, configPath)
}

func documentMap(doc *yaml.Node) *yaml.Node {
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		return doc.Content[0]
	}
	return doc
}

func ensureMappingPath(doc *yaml.Node, path ...string) *yaml.Node {
	current := documentMap(doc)
	for _, key := range path {
		next := mappingValue(current, key)
		if next == nil {
			next = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			setMappingValue(current, key, next)
		}
		current = next
	}
	return current
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func mappingHasKey(node *yaml.Node, key string) bool {
	return mappingValue(node, key) != nil
}

func setMappingValue(node *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content[i+1] = value
			return
		}
	}
	node.Content = append(node.Content, scalarNode(key), value)
}

func mapNode(values ...any) *yaml.Node {
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for i := 0; i < len(values); i += 2 {
		key := values[i].(string)
		value := values[i+1].(*yaml.Node)
		node.Content = append(node.Content, scalarNode(key), value)
	}
	return node
}

func sequenceNode(values ...string) *yaml.Node {
	node := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, value := range values {
		node.Content = append(node.Content, scalarNode(value))
	}
	return node
}

func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func sortedSetKeys(items map[string]struct{}) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
