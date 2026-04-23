package export

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readEnvMapFromFile reads the env map directly from a settings.json file.
func readEnvMapFromFile(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("readEnvMapFromFile: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("readEnvMapFromFile unmarshal: %v", err)
	}
	m := map[string]string{}
	if envRaw, ok := raw["env"]; ok {
		if err := json.Unmarshal(envRaw, &m); err != nil {
			t.Fatalf("readEnvMapFromFile unmarshal env: %v", err)
		}
	}
	return m
}

// readTopLevelKeys returns all top-level keys in the settings.json file.
func readTopLevelKeys(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("readTopLevelKeys: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("readTopLevelKeys unmarshal: %v", err)
	}
	return raw
}

func TestApplyOTelConfig_NonExistentFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	cfg := OTelConfigInput{
		Endpoint: "http://localhost:4318",
		Headers:  map[string]string{"Authorization": "Bearer tok"},
	}
	if err := applyOTelConfigInput(path, cfg); err != nil {
		t.Fatalf("ApplyOTelConfig: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("settings.json not created: %v", err)
	}

	env := readEnvMapFromFile(t, path)
	if env["CLAUDE_CODE_ENABLE_TELEMETRY"] != "1" {
		t.Errorf("CLAUDE_CODE_ENABLE_TELEMETRY = %q, want %q", env["CLAUDE_CODE_ENABLE_TELEMETRY"], "1")
	}
	if env["OTEL_EXPORTER_OTLP_ENDPOINT"] != "http://localhost:4318" {
		t.Errorf("OTEL_EXPORTER_OTLP_ENDPOINT = %q, want %q", env["OTEL_EXPORTER_OTLP_ENDPOINT"], "http://localhost:4318")
	}
	if env["OTEL_METRICS_EXPORTER"] != "otlp" {
		t.Errorf("OTEL_METRICS_EXPORTER = %q, want %q", env["OTEL_METRICS_EXPORTER"], "otlp")
	}
	if env["OTEL_LOGS_EXPORTER"] != "otlp" {
		t.Errorf("OTEL_LOGS_EXPORTER = %q, want %q", env["OTEL_LOGS_EXPORTER"], "otlp")
	}
	if env["OTEL_EXPORTER_OTLP_PROTOCOL"] != "http/json" {
		t.Errorf("OTEL_EXPORTER_OTLP_PROTOCOL = %q, want %q", env["OTEL_EXPORTER_OTLP_PROTOCOL"], "http/json")
	}
	if env["OTEL_EXPORTER_OTLP_HEADERS"] != "Authorization=Bearer tok" {
		t.Errorf("OTEL_EXPORTER_OTLP_HEADERS = %q, want %q", env["OTEL_EXPORTER_OTLP_HEADERS"], "Authorization=Bearer tok")
	}
}

func TestApplyOTelConfig_PreservesExistingEnvVars(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	// Pre-populate with an existing non-managed env var.
	initial := map[string]interface{}{
		"env": map[string]string{
			"MY_VAR": "hello",
		},
	}
	data, _ := json.Marshal(initial)
	os.WriteFile(path, data, 0o600)

	cfg := OTelConfigInput{Endpoint: "http://localhost:4318"}
	if err := applyOTelConfigInput(path, cfg); err != nil {
		t.Fatalf("ApplyOTelConfig: %v", err)
	}

	env := readEnvMapFromFile(t, path)
	if env["MY_VAR"] != "hello" {
		t.Errorf("MY_VAR should be preserved, got %q", env["MY_VAR"])
	}
	if env["CLAUDE_CODE_ENABLE_TELEMETRY"] != "1" {
		t.Errorf("managed key not written")
	}
}

func TestApplyOTelConfig_PreservesTopLevelKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	initial := map[string]interface{}{
		"model": "claude-opus-4-5",
		"env":   map[string]string{},
	}
	data, _ := json.Marshal(initial)
	os.WriteFile(path, data, 0o600)

	cfg := OTelConfigInput{Endpoint: "http://localhost:4318"}
	if err := applyOTelConfigInput(path, cfg); err != nil {
		t.Fatalf("ApplyOTelConfig: %v", err)
	}

	top := readTopLevelKeys(t, path)
	if _, ok := top["model"]; !ok {
		t.Error("top-level 'model' key should be preserved")
	}
	var model string
	json.Unmarshal(top["model"], &model)
	if model != "claude-opus-4-5" {
		t.Errorf("model = %q, want %q", model, "claude-opus-4-5")
	}
}

func TestApplyOTelConfig_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	cfg := OTelConfigInput{Endpoint: "http://localhost:4318"}

	if err := applyOTelConfigInput(path, cfg); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if err := applyOTelConfigInput(path, cfg); err != nil {
		t.Fatalf("second apply: %v", err)
	}

	env := readEnvMapFromFile(t, path)
	// Should have exactly the managed keys (no duplicates as env is a map).
	if env["CLAUDE_CODE_ENABLE_TELEMETRY"] != "1" {
		t.Errorf("idempotent apply: CLAUDE_CODE_ENABLE_TELEMETRY wrong")
	}
	if env["OTEL_EXPORTER_OTLP_ENDPOINT"] != "http://localhost:4318" {
		t.Errorf("idempotent apply: endpoint wrong")
	}
}

func TestApplyOTelConfig_UpdatesEndpoint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	cfg1 := OTelConfigInput{Endpoint: "http://old:4318"}
	if err := applyOTelConfigInput(path, cfg1); err != nil {
		t.Fatalf("first apply: %v", err)
	}

	cfg2 := OTelConfigInput{Endpoint: "http://new:4318"}
	if err := applyOTelConfigInput(path, cfg2); err != nil {
		t.Fatalf("second apply: %v", err)
	}

	env := readEnvMapFromFile(t, path)
	if env["OTEL_EXPORTER_OTLP_ENDPOINT"] != "http://new:4318" {
		t.Errorf("endpoint not updated, got %q", env["OTEL_EXPORTER_OTLP_ENDPOINT"])
	}
}

func TestApplyOTelConfig_HeadersSortedNoSpaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	cfg := OTelConfigInput{
		Endpoint: "http://localhost:4318",
		Headers: map[string]string{
			"X-Org":         "acme",
			"Authorization": "Bearer tok",
		},
	}
	if err := applyOTelConfigInput(path, cfg); err != nil {
		t.Fatalf("ApplyOTelConfig: %v", err)
	}

	env := readEnvMapFromFile(t, path)
	got := env["OTEL_EXPORTER_OTLP_HEADERS"]
	// Sorted by key: Authorization before X-Org.
	want := "Authorization=Bearer tok,X-Org=acme"
	if got != want {
		t.Errorf("OTEL_EXPORTER_OTLP_HEADERS = %q, want %q", got, want)
	}
	// No spaces around = (values may contain spaces, e.g. "Bearer tok")
	if strings.Contains(got, " =") || strings.Contains(got, "= ") {
		t.Errorf("headers should have no spaces around =, got %q", got)
	}
}

func TestApplyOTelConfig_EmptyHeaders_KeyAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	cfg := OTelConfigInput{
		Endpoint: "http://localhost:4318",
		Headers:  map[string]string{},
	}
	if err := applyOTelConfigInput(path, cfg); err != nil {
		t.Fatalf("ApplyOTelConfig: %v", err)
	}

	env := readEnvMapFromFile(t, path)
	if _, ok := env["OTEL_EXPORTER_OTLP_HEADERS"]; ok {
		t.Error("OTEL_EXPORTER_OTLP_HEADERS should not be present when headers empty")
	}
}

func TestApplyOTelConfig_IncludeAccountUUID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	cfg := OTelConfigInput{
		Endpoint:           "http://localhost:4318",
		IncludeAccountUUID: true,
	}
	if err := applyOTelConfigInput(path, cfg); err != nil {
		t.Fatalf("ApplyOTelConfig: %v", err)
	}

	env := readEnvMapFromFile(t, path)
	if env["OTEL_METRICS_INCLUDE_ACCOUNT_UUID"] != "true" {
		t.Errorf("OTEL_METRICS_INCLUDE_ACCOUNT_UUID = %q, want %q", env["OTEL_METRICS_INCLUDE_ACCOUNT_UUID"], "true")
	}
}

func TestApplyOTelConfig_NoIncludeAccountUUID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	cfg := OTelConfigInput{
		Endpoint:           "http://localhost:4318",
		IncludeAccountUUID: false,
	}
	if err := applyOTelConfigInput(path, cfg); err != nil {
		t.Fatalf("ApplyOTelConfig: %v", err)
	}

	env := readEnvMapFromFile(t, path)
	if _, ok := env["OTEL_METRICS_INCLUDE_ACCOUNT_UUID"]; ok {
		t.Error("OTEL_METRICS_INCLUDE_ACCOUNT_UUID should not be present when IncludeAccountUUID=false")
	}
}

func TestApplyOTelConfig_ResourceAttributes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	cfg := OTelConfigInput{
		Endpoint: "http://localhost:4318",
		UserName: "alice",
		TeamName: "eng",
	}
	if err := applyOTelConfigInput(path, cfg); err != nil {
		t.Fatalf("ApplyOTelConfig: %v", err)
	}

	env := readEnvMapFromFile(t, path)
	got := env["OTEL_RESOURCE_ATTRIBUTES"]
	// Sorted: team.name before user.name.
	want := "team.name=eng,user.name=alice"
	if got != want {
		t.Errorf("OTEL_RESOURCE_ATTRIBUTES = %q, want %q", got, want)
	}
}

func TestApplyOTelConfig_EmptyResourceAttributes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	cfg := OTelConfigInput{
		Endpoint: "http://localhost:4318",
		UserName: "",
		TeamName: "",
	}
	if err := applyOTelConfigInput(path, cfg); err != nil {
		t.Fatalf("ApplyOTelConfig: %v", err)
	}

	env := readEnvMapFromFile(t, path)
	if _, ok := env["OTEL_RESOURCE_ATTRIBUTES"]; ok {
		t.Error("OTEL_RESOURCE_ATTRIBUTES should not be present when both names are empty")
	}
}

func TestRemoveOTelConfig_RemovesManagedKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	// Apply first.
	cfg := OTelConfigInput{Endpoint: "http://localhost:4318"}
	applyOTelConfigInput(path, cfg)

	// Also add a non-managed key.
	s, _ := loadOrEmpty(path)
	env := readEnvMap(s)
	env["KEEP_ME"] = "yes"
	writeEnvMap(path, s, env)

	if err := RemoveOTelConfig(path); err != nil {
		t.Fatalf("RemoveOTelConfig: %v", err)
	}

	finalEnv := readEnvMapFromFile(t, path)
	for _, k := range ManagedOTelKeys {
		if _, ok := finalEnv[k]; ok {
			t.Errorf("managed key %q should have been removed", k)
		}
	}
	if finalEnv["KEEP_ME"] != "yes" {
		t.Error("non-managed key KEEP_ME should be preserved")
	}
}

func TestRemoveOTelConfig_NoPanicWhenNoManagedKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	initial := map[string]interface{}{
		"env": map[string]string{"MY_VAR": "hello"},
	}
	data, _ := json.Marshal(initial)
	os.WriteFile(path, data, 0o600)

	if err := RemoveOTelConfig(path); err != nil {
		t.Fatalf("RemoveOTelConfig on clean file: %v", err)
	}
}

func TestRemoveOTelConfig_CreatesBakFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	// Apply to create file with content.
	cfg := OTelConfigInput{Endpoint: "http://localhost:4318"}
	applyOTelConfigInput(path, cfg)

	if err := RemoveOTelConfig(path); err != nil {
		t.Fatalf("RemoveOTelConfig: %v", err)
	}

	matches, err := filepath.Glob(path + ".bak-*")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) == 0 {
		t.Error("expected a .bak-* file after RemoveOTelConfig")
	}
}

func TestStatusOTelConfig_NotApplied(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	initial := map[string]interface{}{
		"env": map[string]string{"MY_VAR": "hello"},
	}
	data, _ := json.Marshal(initial)
	os.WriteFile(path, data, 0o600)

	status, err := StatusOTelConfig(path)
	if err != nil {
		t.Fatalf("StatusOTelConfig: %v", err)
	}
	if status.Applied {
		t.Error("Applied should be false when no managed keys present")
	}
}

func TestStatusOTelConfig_Applied(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	cfg := OTelConfigInput{Endpoint: "http://localhost:4318"}
	applyOTelConfigInput(path, cfg)

	status, err := StatusOTelConfig(path)
	if err != nil {
		t.Fatalf("StatusOTelConfig: %v", err)
	}
	if !status.Applied {
		t.Error("Applied should be true after ApplyOTelConfig")
	}
	if status.Values["OTEL_EXPORTER_OTLP_ENDPOINT"] != "http://localhost:4318" {
		t.Errorf("Values[OTEL_EXPORTER_OTLP_ENDPOINT] = %q", status.Values["OTEL_EXPORTER_OTLP_ENDPOINT"])
	}
}

func TestStatusOTelConfig_NonExistentFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	status, err := StatusOTelConfig(path)
	if err != nil {
		t.Fatalf("StatusOTelConfig on non-existent file: %v", err)
	}
	if status.Applied {
		t.Error("Applied should be false for non-existent file")
	}
}
