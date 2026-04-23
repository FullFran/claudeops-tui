package export

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/fullfran/claudeops-tui/internal/config"
	"github.com/fullfran/claudeops-tui/internal/hooks"
)

// ManagedOTelKeys are the env var names written/removed by otelconfig.
var ManagedOTelKeys = []string{
	"CLAUDE_CODE_ENABLE_TELEMETRY",
	"OTEL_METRICS_EXPORTER",
	"OTEL_LOGS_EXPORTER",
	"OTEL_EXPORTER_OTLP_PROTOCOL",
	"OTEL_EXPORTER_OTLP_ENDPOINT",
	"OTEL_EXPORTER_OTLP_HEADERS",
	"OTEL_RESOURCE_ATTRIBUTES",
	"OTEL_METRICS_INCLUDE_ACCOUNT_UUID",
}

// OTelConfigInput holds the values to write into the Claude settings env block.
type OTelConfigInput struct {
	Endpoint           string
	Headers            map[string]string
	UserName           string
	TeamName           string
	IncludeAccountUUID bool
}

// OTelConfigStatus is the result of StatusOTelConfig.
type OTelConfigStatus struct {
	Applied bool
	Values  map[string]string
}

// ApplyOTelConfig writes managed OTel env vars to the Claude settings.json at
// settingsPath. It merges into the existing env map, preserving all non-managed keys.
// cfg is a config.ExportSettings; the ClaudeOTel.IncludeAccountUUID field does not
// exist on ExportSettings — set IncludeAccountUUID to false by default.
func ApplyOTelConfig(settingsPath string, cfg config.ExportSettings) error {
	return applyOTelConfigInput(settingsPath, OTelConfigInput{
		Endpoint:           cfg.Endpoint,
		Headers:            cfg.Headers,
		UserName:           cfg.UserName,
		TeamName:           cfg.TeamName,
		IncludeAccountUUID: false,
	})
}

// applyOTelConfigInput is the internal implementation accepting OTelConfigInput.
func applyOTelConfigInput(settingsPath string, cfg OTelConfigInput) error {
	s, err := loadOrEmpty(settingsPath)
	if err != nil {
		return err
	}
	envMap := readEnvMap(s)

	// Always set core keys.
	envMap["CLAUDE_CODE_ENABLE_TELEMETRY"] = "1"
	envMap["OTEL_METRICS_EXPORTER"] = "otlp"
	envMap["OTEL_LOGS_EXPORTER"] = "otlp"
	envMap["OTEL_EXPORTER_OTLP_PROTOCOL"] = "http/json"
	envMap["OTEL_EXPORTER_OTLP_ENDPOINT"] = cfg.Endpoint

	// Headers — omit key if empty.
	if len(cfg.Headers) > 0 {
		envMap["OTEL_EXPORTER_OTLP_HEADERS"] = serializeHeaders(cfg.Headers)
	} else {
		delete(envMap, "OTEL_EXPORTER_OTLP_HEADERS")
	}

	// Resource attributes — omit key if both names are empty.
	resourceAttrs := buildResourceAttrs(cfg.UserName, cfg.TeamName)
	if resourceAttrs != "" {
		envMap["OTEL_RESOURCE_ATTRIBUTES"] = resourceAttrs
	} else {
		delete(envMap, "OTEL_RESOURCE_ATTRIBUTES")
	}

	// Account UUID flag.
	if cfg.IncludeAccountUUID {
		envMap["OTEL_METRICS_INCLUDE_ACCOUNT_UUID"] = "true"
	} else {
		delete(envMap, "OTEL_METRICS_INCLUDE_ACCOUNT_UUID")
	}

	return writeEnvMap(settingsPath, s, envMap)
}

// RemoveOTelConfig removes all managed OTel env vars from the Claude settings.json.
// If none are present, no changes are made (and no backup is created).
func RemoveOTelConfig(settingsPath string) error {
	s, err := loadOrEmpty(settingsPath)
	if err != nil {
		return err
	}
	envMap := readEnvMap(s)
	changed := false
	for _, k := range ManagedOTelKeys {
		if _, ok := envMap[k]; ok {
			delete(envMap, k)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return writeEnvMap(settingsPath, s, envMap)
}

// StatusOTelConfig reads current OTel env var state from the Claude settings.json.
// Returns Applied=false with nil error when the file does not exist.
func StatusOTelConfig(settingsPath string) (OTelConfigStatus, error) {
	s, err := loadOrEmpty(settingsPath)
	if err != nil {
		return OTelConfigStatus{}, err
	}
	envMap := readEnvMap(s)
	vals := map[string]string{}
	for _, k := range ManagedOTelKeys {
		if v, ok := envMap[k]; ok {
			vals[k] = v
		}
	}
	return OTelConfigStatus{Applied: len(vals) > 0, Values: vals}, nil
}

// loadOrEmpty loads the settings at path. Missing files return an empty Settings
// rather than an error, matching the hooks.Load contract.
func loadOrEmpty(path string) (*hooks.Settings, error) {
	s, err := hooks.Load(path)
	if err != nil {
		return nil, fmt.Errorf("otelconfig load: %w", err)
	}
	if s.Extra == nil {
		s.Extra = map[string]json.RawMessage{}
	}
	return s, nil
}

// readEnvMap extracts the "env" map[string]string from Settings.Extra.
func readEnvMap(s *hooks.Settings) map[string]string {
	m := map[string]string{}
	if raw, ok := s.Extra["env"]; ok {
		_ = json.Unmarshal(raw, &m)
	}
	return m
}

// writeEnvMap serializes envMap back into Settings.Extra["env"] and saves.
func writeEnvMap(settingsPath string, s *hooks.Settings, envMap map[string]string) error {
	b, err := json.Marshal(envMap)
	if err != nil {
		return fmt.Errorf("otelconfig: marshal env: %w", err)
	}
	s.Extra["env"] = json.RawMessage(b)
	return hooks.Save(settingsPath, s)
}

// serializeHeaders returns "key1=val1,key2=val2" sorted by key with no spaces.
func serializeHeaders(h map[string]string) string {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + h[k]
	}
	return strings.Join(parts, ",")
}

// buildResourceAttrs returns "team.name=...,user.name=..." sorted, or "" if both empty.
func buildResourceAttrs(userName, teamName string) string {
	var parts []string
	if teamName != "" {
		parts = append(parts, "team.name="+teamName)
	}
	if userName != "" {
		parts = append(parts, "user.name="+userName)
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}
