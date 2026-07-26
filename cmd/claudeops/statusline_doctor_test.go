package main

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fullfran/claudeops-tui/internal/config"
	"github.com/fullfran/claudeops-tui/internal/provider"
	"github.com/fullfran/claudeops-tui/internal/statusline"
	"github.com/fullfran/claudeops-tui/internal/usage"
)

func doctor(t *testing.T, p config.Paths, fetch snapshotFetcher, reg registryFetcher) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := cmdStatuslineDoctor(p, &out, config.DefaultSettings(), fetch, reg, 2*time.Second)
	return out.String(), err
}

func TestDoctorReportsWorkingProviders(t *testing.T) {
	p := config.ForHome(t.TempDir())
	reg := fakeRegistry{usages: []provider.Usage{{
		Provider: "Codex",
		Windows:  []provider.Window{{Label: "5h", Utilization: 12}},
		Note:     "plan: plus",
		Source:   "opencode",
	}}}

	out, err := doctor(t, p, &fakeFetcher{snap: snapAt(42)}, reg)
	if err != nil {
		t.Fatalf("nothing is broken, doctor should exit zero: %v", err)
	}
	for _, want := range []string{"claude", "ok", "5h 42%", "codex", "plan: plus"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// The line that answers "which credential is it actually using".
	if !strings.Contains(out, "via opencode") {
		t.Errorf("the credential source should be named:\n%s", out)
	}
}

func TestDoctorNamesProvidersWithNoCredentials(t *testing.T) {
	// Silence is not an answer to "why is Copilot missing".
	p := config.ForHome(t.TempDir())
	out, err := doctor(t, p, &fakeFetcher{snap: snapAt(42)}, emptyRegistry{})
	if err != nil {
		t.Fatalf("an unused service is not a failure: %v", err)
	}
	for _, name := range []string{"codex", "copilot", "gemini"} {
		if !strings.Contains(out, name) {
			t.Errorf("provider %q should be listed as absent:\n%s", name, out)
		}
	}
	if !strings.Contains(out, "absent") {
		t.Errorf("expected an absent state:\n%s", out)
	}
	// Being told what is missing without being told what to do about it is only
	// half an answer.
	if !strings.Contains(out, "codex login") {
		t.Errorf("expected a remedy for codex:\n%s", out)
	}
}

func TestDoctorDistinguishesStaleFromBroken(t *testing.T) {
	// A failed refresh with a usable cache means the bar is still correct. That
	// is worth reporting and not worth a non-zero exit — rate limiting in
	// particular clears itself.
	p := config.ForHome(t.TempDir())
	if err := statusline.WriteCache(p.UsageCachePath, snapAt(17), nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	out, err := doctor(t, p, &fakeFetcher{err: errors.New("HTTP 429")}, emptyRegistry{})
	if err != nil {
		t.Fatalf("a stale-but-serving provider must not fail the doctor: %v", err)
	}
	if !strings.Contains(out, "stale") {
		t.Errorf("expected a stale state:\n%s", out)
	}
	if !strings.Contains(out, "serving cache") {
		t.Errorf("expected the cache fallback to be spelled out:\n%s", out)
	}
}

func TestDoctorFailsWhenNothingIsUsable(t *testing.T) {
	// No cache and no fetch: the bar shows nothing and that is a real problem.
	p := config.ForHome(t.TempDir())
	out, err := doctor(t, p, &fakeFetcher{err: errors.New("no credentials")}, emptyRegistry{})
	if err == nil {
		t.Error("a broken provider should make the doctor exit non-zero")
	}
	if !strings.Contains(out, "error") {
		t.Errorf("expected an error state:\n%s", out)
	}
}

func TestDoctorReportsEmptyPlanSeparately(t *testing.T) {
	// Authenticated but no quota windows is not an error: some plans simply do
	// not report any, and telling someone to re-authenticate would be wrong.
	p := config.ForHome(t.TempDir())
	out, err := doctor(t, p, &fakeFetcher{snap: usage.Snapshot{}}, emptyRegistry{})
	if err != nil {
		t.Fatalf("an empty plan is not a failure: %v", err)
	}
	if !strings.Contains(out, "empty") {
		t.Errorf("expected an empty state:\n%s", out)
	}
}

func TestDoctorShowsConfigState(t *testing.T) {
	p := config.ForHome(t.TempDir())
	out, _ := doctor(t, p, &fakeFetcher{snap: snapAt(42)}, emptyRegistry{})
	for _, want := range []string{"config", "statusline", "provider", "cache", "agent mapping"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing the %q line:\n%s", want, out)
		}
	}
}

func TestDoctorSaysWhenDisabled(t *testing.T) {
	p := config.ForHome(t.TempDir())
	if err := os.MkdirAll(p.DataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.ConfigPath, []byte("[statusline]\nenabled = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := config.Load(p.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	_ = cmdStatuslineDoctor(p, &out, settings, &fakeFetcher{snap: snapAt(42)}, emptyRegistry{}, time.Second)
	if !strings.Contains(out.String(), "disabled") {
		t.Errorf("a disabled status line should say so:\n%s", out.String())
	}
	// And say how to turn it back on.
	if !strings.Contains(out.String(), "statusline enable") {
		t.Errorf("expected the remedy:\n%s", out.String())
	}
}

func TestDoctorSurvivesEveryProviderFailing(t *testing.T) {
	// One provider erroring must not stop the others being reported.
	p := config.ForHome(t.TempDir())
	var out bytes.Buffer
	err := cmdStatuslineDoctor(p, &out, config.DefaultSettings(),
		&fakeFetcher{err: errors.New("claude down")}, failingRegistry{}, time.Second)
	if err == nil {
		t.Error("expected a non-zero exit")
	}
	for _, want := range []string{"claude", "codex"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("every provider should still be listed, missing %q:\n%s", want, out.String())
		}
	}
}
