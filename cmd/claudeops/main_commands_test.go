package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fullfran/claudeops-tui/internal/config"
	"github.com/fullfran/claudeops-tui/internal/store"
)

// newTestPaths returns Paths rooted at a fresh temp home. Nothing on disk is
// created, so commands must provision their own data dir.
func newTestPaths(t *testing.T) config.Paths {
	t.Helper()
	return config.ForHome(t.TempDir())
}

func TestCmdMCPWith(t *testing.T) {
	t.Run("fresh data dir does not fail", func(t *testing.T) {
		p := newTestPaths(t)
		served := false
		err := cmdMCPWith(p, func(s *store.Store) error {
			served = true
			if _, err := s.TaskAggregates(context.Background()); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			t.Fatalf("cmdMCPWith on fresh install: %v", err)
		}
		if !served {
			t.Fatal("expected serve func to be called")
		}
		if _, err := os.Stat(p.DBPath); err != nil {
			t.Fatalf("expected db file at %s: %v", p.DBPath, err)
		}
	})

	t.Run("existing store is reused", func(t *testing.T) {
		p := newTestPaths(t)
		if err := p.EnsureDataDir(); err != nil {
			t.Fatal(err)
		}
		s, err := store.Open(p.DBPath)
		if err != nil {
			t.Fatal(err)
		}
		_ = s.Close()

		if err := cmdMCPWith(p, func(*store.Store) error { return nil }); err != nil {
			t.Fatalf("cmdMCPWith on existing install: %v", err)
		}
	})

	t.Run("serve error propagates", func(t *testing.T) {
		p := newTestPaths(t)
		want := errors.New("serve boom")
		if err := cmdMCPWith(p, func(*store.Store) error { return want }); !errors.Is(err, want) {
			t.Fatalf("err = %v, want %v", err, want)
		}
	})
}

// fakeUnit builds an ingestUnit with canned behaviour.
func fakeUnit(name string, err error, ingested, unknown, parseErrors int64) ingestUnit {
	return ingestUnit{
		name:   name,
		ingest: func(context.Context) error { return err },
		counts: func() (int64, int64, int64) { return ingested, unknown, parseErrors },
	}
}

func TestRunIngestUnits(t *testing.T) {
	tests := []struct {
		name         string
		units        []ingestUnit
		wantIngested int64
		wantFailures []string
	}{
		{
			name: "all sources succeed",
			units: []ingestUnit{
				fakeUnit("claude", nil, 3, 1, 0),
				fakeUnit("codex", nil, 2, 0, 1),
			},
			wantIngested: 5,
		},
		{
			name: "one source fails",
			units: []ingestUnit{
				fakeUnit("claude", nil, 3, 0, 0),
				fakeUnit("codex", errors.New("boom"), 0, 0, 0),
			},
			wantIngested: 3,
			wantFailures: []string{"codex"},
		},
		{
			name: "every source fails",
			units: []ingestUnit{
				fakeUnit("claude", errors.New("boom"), 0, 0, 0),
				fakeUnit("codex", errors.New("boom"), 0, 0, 0),
			},
			wantFailures: []string{"claude", "codex"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var errOut bytes.Buffer
			res := runIngestUnits(context.Background(), tc.units, &errOut)
			if res.ingested != tc.wantIngested {
				t.Errorf("ingested = %d, want %d", res.ingested, tc.wantIngested)
			}
			if len(res.failed) != len(tc.wantFailures) {
				t.Fatalf("failed = %v, want %v", res.failed, tc.wantFailures)
			}
			for i, name := range tc.wantFailures {
				if res.failed[i] != name {
					t.Errorf("failed[%d] = %q, want %q", i, res.failed[i], name)
				}
				if !strings.Contains(errOut.String(), name) {
					t.Errorf("stderr %q does not mention failed source %q", errOut.String(), name)
				}
			}
			err := res.err()
			if len(tc.wantFailures) == 0 && err != nil {
				t.Errorf("err = %v, want nil", err)
			}
			if len(tc.wantFailures) > 0 && err == nil {
				t.Error("err = nil, want non-nil so the process exits non-zero")
			}
		})
	}
}

func TestCmdIngestWith(t *testing.T) {
	t.Run("success prints counters and exits zero", func(t *testing.T) {
		p := newTestPaths(t)
		var out, errOut bytes.Buffer
		build := func(config.Paths, *core) []ingestUnit {
			return []ingestUnit{fakeUnit("claude", nil, 7, 1, 2)}
		}
		if err := cmdIngestWith(context.Background(), p, build, &out, &errOut); err != nil {
			t.Fatalf("cmdIngestWith: %v", err)
		}
		if !strings.Contains(out.String(), "ingested: 7") {
			t.Errorf("stdout = %q, want ingested count", out.String())
		}
	})

	t.Run("source failure returns an error", func(t *testing.T) {
		p := newTestPaths(t)
		var out, errOut bytes.Buffer
		build := func(config.Paths, *core) []ingestUnit {
			return []ingestUnit{fakeUnit("codex", errors.New("boom"), 0, 0, 0)}
		}
		err := cmdIngestWith(context.Background(), p, build, &out, &errOut)
		if err == nil {
			t.Fatal("expected an error so cron jobs can detect the failure")
		}
		if !strings.Contains(err.Error(), "codex") {
			t.Errorf("err = %v, want it to name the failed source", err)
		}
	})

}

func TestBuildIngestUnits(t *testing.T) {
	p := newTestPaths(t)
	c, err := openCoreAt(p)
	if err != nil {
		t.Fatalf("openCoreAt: %v", err)
	}
	defer c.close()

	units := buildIngestUnits(p, c)
	if len(units) == 0 {
		t.Fatal("expected at least the claude source")
	}
	if units[0].name != "claude" {
		t.Errorf("units[0].name = %q, want %q", units[0].name, "claude")
	}
	for _, u := range units {
		if u.ingest == nil {
			t.Errorf("unit %q has no ingest func", u.name)
		}
	}
}

func TestCmdReingestWith(t *testing.T) {
	t.Run("declining the prompt aborts", func(t *testing.T) {
		p := newTestPaths(t)
		var out, errOut bytes.Buffer
		called := false
		build := func(config.Paths, *core) []ingestUnit {
			called = true
			return nil
		}
		err := cmdReingestWith(context.Background(), p, build,
			strings.NewReader("n\n"), &out, &errOut, false)
		if err != nil {
			t.Fatalf("cmdReingestWith: %v", err)
		}
		if called {
			t.Error("expected no ingest after the user declined")
		}
		if !strings.Contains(out.String(), "aborted") {
			t.Errorf("stdout = %q, want abort notice", out.String())
		}
	})

	t.Run("--yes rebuilds without prompting", func(t *testing.T) {
		p := newTestPaths(t)
		var out, errOut bytes.Buffer
		build := func(config.Paths, *core) []ingestUnit {
			return []ingestUnit{fakeUnit("claude", nil, 4, 0, 0)}
		}
		if err := cmdReingestWith(context.Background(), p, build,
			strings.NewReader(""), &out, &errOut, true); err != nil {
			t.Fatalf("cmdReingestWith: %v", err)
		}
		if !strings.Contains(out.String(), "reingested: 4") {
			t.Errorf("stdout = %q, want reingested count", out.String())
		}
	})

	t.Run("source failure returns an error", func(t *testing.T) {
		p := newTestPaths(t)
		var out, errOut bytes.Buffer
		build := func(config.Paths, *core) []ingestUnit {
			return []ingestUnit{fakeUnit("opencode", errors.New("boom"), 0, 0, 0)}
		}
		err := cmdReingestWith(context.Background(), p, build,
			strings.NewReader(""), &out, &errOut, true)
		if err == nil {
			t.Fatal("expected an error so scripts can detect the failure")
		}
		if !strings.Contains(err.Error(), "opencode") {
			t.Errorf("err = %v, want it to name the failed source", err)
		}
	})
}

func TestCmdReingestFlagParsing(t *testing.T) {
	if err := cmdReingest([]string{"--bogus"}); err == nil {
		t.Fatal("expected an error for an unknown flag")
	}
}

func TestCmdTaskWith(t *testing.T) {
	ctx := context.Background()

	t.Run("missing subcommand errors", func(t *testing.T) {
		if err := cmdTaskWith(ctx, newTestPaths(t), &bytes.Buffer{}, nil); err == nil {
			t.Fatal("expected an error for a missing subcommand")
		}
	})

	t.Run("unknown subcommand errors", func(t *testing.T) {
		if err := cmdTaskWith(ctx, newTestPaths(t), &bytes.Buffer{}, []string{"nope"}); err == nil {
			t.Fatal("expected an error for an unknown subcommand")
		}
	})

	t.Run("start without a name errors", func(t *testing.T) {
		if err := cmdTaskWith(ctx, newTestPaths(t), &bytes.Buffer{}, []string{"start"}); err == nil {
			t.Fatal("expected an error for a missing task name")
		}
	})

	t.Run("start then stop then list", func(t *testing.T) {
		p := newTestPaths(t)
		var out bytes.Buffer
		if err := cmdTaskWith(ctx, p, &out, []string{"start", "refactor"}); err != nil {
			t.Fatalf("task start: %v", err)
		}
		if !strings.Contains(out.String(), "refactor") {
			t.Errorf("stdout = %q, want the task name", out.String())
		}
		out.Reset()
		if err := cmdTaskWith(ctx, p, &out, []string{"stop"}); err != nil {
			t.Fatalf("task stop: %v", err)
		}
		out.Reset()
		if err := cmdTaskWith(ctx, p, &out, []string{"list"}); err != nil {
			t.Fatalf("task list: %v", err)
		}
		got := out.String()
		for _, want := range []string{"DURATION", "IN", "OUT", "CACHE R", "CACHE W", "refactor"} {
			if !strings.Contains(got, want) {
				t.Errorf("task list output %q missing %q", got, want)
			}
		}
	})
}

func TestFormatDur(t *testing.T) {
	tests := []struct {
		name string
		in   time.Duration
		want string
	}{
		{"zero", 0, "0m"},
		{"minutes", 45 * time.Minute, "45m"},
		{"hours", 3*time.Hour + 20*time.Minute, "3h 20m"},
		{"days", 50 * time.Hour, "2d 2h"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatDur(tc.in); got != tc.want {
				t.Errorf("formatDur(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestCmdHooksWith(t *testing.T) {
	t.Run("missing subcommand errors", func(t *testing.T) {
		if err := cmdHooksWith(newTestPaths(t), strings.NewReader(""), &bytes.Buffer{}, nil); err == nil {
			t.Fatal("expected an error for a missing subcommand")
		}
	})

	t.Run("unknown subcommand errors", func(t *testing.T) {
		if err := cmdHooksWith(newTestPaths(t), strings.NewReader(""), &bytes.Buffer{}, []string{"nope"}); err == nil {
			t.Fatal("expected an error for an unknown subcommand")
		}
	})

	t.Run("install then status then uninstall", func(t *testing.T) {
		p := newTestPaths(t)
		if err := os.MkdirAll(p.ClaudeDir, 0o700); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := cmdHooksWith(p, strings.NewReader(""), &out, []string{"install"}); err != nil {
			t.Fatalf("hooks install: %v", err)
		}
		if !strings.Contains(out.String(), p.ClaudeSettings) {
			t.Errorf("stdout = %q, want the settings path", out.String())
		}
		out.Reset()
		if err := cmdHooksWith(p, strings.NewReader(""), &out, []string{"status"}); err != nil {
			t.Fatalf("hooks status: %v", err)
		}
		if !strings.Contains(out.String(), "settings:") {
			t.Errorf("stdout = %q, want a status report", out.String())
		}
		out.Reset()
		if err := cmdHooksWith(p, strings.NewReader(""), &out, []string{"uninstall"}); err != nil {
			t.Fatalf("hooks uninstall: %v", err)
		}
	})

	t.Run("handle never fails the caller", func(t *testing.T) {
		p := newTestPaths(t)
		if err := cmdHooksWith(p, strings.NewReader("not json"), &bytes.Buffer{}, []string{"handle"}); err != nil {
			t.Fatalf("hooks handle: %v", err)
		}
	})
}

func TestCmdPushWith(t *testing.T) {
	ctx := context.Background()

	t.Run("invalid --since errors", func(t *testing.T) {
		err := cmdPushWith(ctx, newTestPaths(t), &bytes.Buffer{}, []string{"--since", "not-a-date"})
		if err == nil {
			t.Fatal("expected an error for an invalid --since")
		}
	})

	t.Run("unknown flag errors", func(t *testing.T) {
		if err := cmdPushWith(ctx, newTestPaths(t), &bytes.Buffer{}, []string{"--bogus"}); err == nil {
			t.Fatal("expected an error for an unknown flag")
		}
	})

	t.Run("export disabled errors", func(t *testing.T) {
		if err := cmdPushWith(ctx, newTestPaths(t), &bytes.Buffer{}, nil); err == nil {
			t.Fatal("expected an error when export is disabled")
		}
	})

	t.Run("dry run prints the payload", func(t *testing.T) {
		p := newTestPaths(t)
		if err := p.EnsureDataDir(); err != nil {
			t.Fatal(err)
		}
		writeConfig(t, p, "[export]\nenabled = true\nendpoint = \"http://localhost:4318\"\n")
		var out bytes.Buffer
		if err := cmdPushWith(ctx, p, &out, []string{"--dry-run"}); err != nil {
			t.Fatalf("cmdPushWith: %v", err)
		}
		if out.Len() == 0 {
			t.Error("expected the dry-run payload on stdout")
		}
	})
}

func writeConfig(t *testing.T, p config.Paths, body string) {
	t.Helper()
	if err := os.MkdirAll(p.DataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.ConfigPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCmdOTelConfigWith(t *testing.T) {
	t.Run("missing subcommand errors", func(t *testing.T) {
		if err := cmdOTelConfigWith(newTestPaths(t), &bytes.Buffer{}, nil); err == nil {
			t.Fatal("expected an error for a missing subcommand")
		}
	})

	t.Run("unknown subcommand errors", func(t *testing.T) {
		if err := cmdOTelConfigWith(newTestPaths(t), &bytes.Buffer{}, []string{"nope"}); err == nil {
			t.Fatal("expected an error for an unknown subcommand")
		}
	})

	t.Run("apply while claude_otel is disabled errors", func(t *testing.T) {
		if err := cmdOTelConfigWith(newTestPaths(t), &bytes.Buffer{}, []string{"apply"}); err == nil {
			t.Fatal("expected an error when claude_otel is disabled")
		}
	})

	t.Run("apply then status then remove", func(t *testing.T) {
		p := newTestPaths(t)
		if err := os.MkdirAll(p.ClaudeDir, 0o700); err != nil {
			t.Fatal(err)
		}
		writeConfig(t, p, "[export.claude_otel]\nenabled = true\n")

		var out bytes.Buffer
		if err := cmdOTelConfigWith(p, &out, []string{"apply"}); err != nil {
			t.Fatalf("otel-config apply: %v", err)
		}
		if !strings.Contains(out.String(), p.ClaudeSettings) {
			t.Errorf("stdout = %q, want the settings path", out.String())
		}
		out.Reset()
		if err := cmdOTelConfigWith(p, &out, []string{"status"}); err != nil {
			t.Fatalf("otel-config status: %v", err)
		}
		if !strings.Contains(out.String(), "applied:") {
			t.Errorf("stdout = %q, want a status report", out.String())
		}
		out.Reset()
		if err := cmdOTelConfigWith(p, &out, []string{"remove"}); err != nil {
			t.Fatalf("otel-config remove: %v", err)
		}
	})
}

func TestResolveBinary(t *testing.T) {
	bin, err := resolveBinary()
	if err != nil {
		t.Fatalf("resolveBinary: %v", err)
	}
	if !filepath.IsAbs(bin) {
		t.Errorf("resolveBinary() = %q, want an absolute path", bin)
	}
}

func TestPrintHelpListsEverySubcommand(t *testing.T) {
	got := captureStdout(t, func() error {
		printHelp()
		return nil
	})
	for _, want := range []string{
		"task start", "task stop", "task list", "ingest", "reingest", "update",
		"hooks install", "hooks uninstall", "hooks status", "hooks handle",
		"push", "otel-config apply", "mcp", "version",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("help output is missing %q", want)
		}
	}
}

func TestBuildTUIModel(t *testing.T) {
	p := newTestPaths(t)
	c, err := openCoreAt(p)
	if err != nil {
		t.Fatalf("openCoreAt: %v", err)
	}
	defer c.close()

	m := buildTUIModel(p, config.DefaultSettings(), c)
	if m.ConfigPath != p.ConfigPath {
		t.Errorf("ConfigPath = %q, want %q", m.ConfigPath, p.ConfigPath)
	}
	if m.ProjectsRoot != p.ClaudeProjects {
		t.Errorf("ProjectsRoot = %q, want %q", m.ProjectsRoot, p.ClaudeProjects)
	}
	if m.Providers == nil {
		t.Error("expected a provider registry to be wired")
	}
}

func TestOpenCoreAtCreatesDataDir(t *testing.T) {
	p := newTestPaths(t)
	c, err := openCoreAt(p)
	if err != nil {
		t.Fatalf("openCoreAt: %v", err)
	}
	defer c.close()
	if _, err := os.Stat(p.DataDir); err != nil {
		t.Fatalf("expected data dir at %s: %v", p.DataDir, err)
	}
	if c.store == nil || c.calc == nil || c.tasks == nil {
		t.Fatal("expected store, calculator and tracker to be wired")
	}
}
