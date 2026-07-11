// Command claudeops is the entrypoint. With no subcommand it launches the
// TUI dashboard. Subcommands are: task start|stop|list, ingest, update, version.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"text/tabwriter"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/fullfran/claudeops-tui/internal/codex"
	"github.com/fullfran/claudeops-tui/internal/collector"
	"github.com/fullfran/claudeops-tui/internal/config"
	"github.com/fullfran/claudeops-tui/internal/export"
	"github.com/fullfran/claudeops-tui/internal/hooks"
	"github.com/fullfran/claudeops-tui/internal/mcpserver"
	ocingester "github.com/fullfran/claudeops-tui/internal/opencode"
	"github.com/fullfran/claudeops-tui/internal/parser"
	"github.com/fullfran/claudeops-tui/internal/pricing"
	"github.com/fullfran/claudeops-tui/internal/provider"
	"github.com/fullfran/claudeops-tui/internal/source"
	"github.com/fullfran/claudeops-tui/internal/store"
	"github.com/fullfran/claudeops-tui/internal/tasks"
	"github.com/fullfran/claudeops-tui/internal/tui"
	"github.com/fullfran/claudeops-tui/internal/usage"
)

const version = "0.7.0"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "claudeops:", err)
		os.Exit(1)
	}
}

var (
	runMCPCommand        = cmdMCP
	runTaskCommand       = cmdTask
	runIngestCommand     = cmdIngest
	runReingestCommand   = cmdReingest
	runUpdateCommand     = cmdUpdate
	runHooksCommand      = cmdHooks
	runPushCommand       = cmdPush
	runOTelConfigCommand = cmdOTelConfig
)

func run() error {
	return runArgs(os.Args[1:])
}

func runArgs(args []string) error {
	if len(args) == 0 {
		return cmdTUI()
	}
	switch args[0] {
	case "version", "-v", "--version":
		fmt.Println("claudeops", version)
		return nil
	case "mcp":
		return runMCPCommand()
	case "task":
		return runTaskCommand(args[1:])
	case "ingest":
		return runIngestCommand()
	case "reingest":
		return runReingestCommand(args[1:])
	case "update":
		return runUpdateCommand()
	case "hooks":
		return runHooksCommand(args[1:])
	case "push":
		return runPushCommand(args[1:])
	case "otel-config":
		return runOTelConfigCommand(args[1:])
	case "help", "-h", "--help":
		printHelp()
		return nil
	default:
		printHelp()
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func printHelp() {
	fmt.Println(`claudeops — local TUI for Claude Code usage tracking

Usage:
  claudeops                                     launch the dashboard TUI (default)
  claudeops task start "<name>"                 start a task
  claudeops task stop                           stop the current task
  claudeops task list                           list all tasks
  claudeops ingest                              one-shot ingest of existing JSONL files
  claudeops reingest [--yes]                    rebuild the event store from source files (corrects pre-0.4 inflated usage)
  claudeops update                              update the installed CLI
  claudeops hooks install                       register Claude Code hooks for live status
  claudeops hooks uninstall                     remove claudeops hooks from settings.json
  claudeops hooks status                        show which hooks are registered
  claudeops push [--dry-run] [--since RFC3339]  push metrics to OTLP endpoint
  claudeops otel-config apply                   configure Claude Code OTel telemetry
  claudeops otel-config status                  show OTel telemetry configuration
  claudeops otel-config remove                  remove OTel telemetry configuration
  claudeops mcp                                 start MCP server over stdio
  claudeops version                             print version

Files:
  ~/.claudeops/claudeops.db        local SQLite store
  ~/.claudeops/pricing.toml        editable pricing table
  ~/.claudeops/config.toml         dashboard widgets, thresholds, key bindings
  ~/.claudeops/current-task.json   sidecar for the active task
  ~/.claude/projects/              source data (read-only)`)
}

func cmdMCP() error {
	p, err := config.Default()
	if err != nil {
		return err
	}
	s, err := store.OpenReadOnly(p.DBPath)
	if err != nil {
		return err
	}
	defer s.Close()
	srv := mcpserver.New(s)
	return srv.Serve()
}

func openCore() (config.Paths, *store.Store, *pricing.Calculator, *tasks.Tracker, error) {
	p, err := config.Default()
	if err != nil {
		return p, nil, nil, nil, err
	}
	if err := p.EnsureDataDir(); err != nil {
		return p, nil, nil, nil, err
	}
	s, err := store.Open(p.DBPath)
	if err != nil {
		return p, nil, nil, nil, err
	}
	tbl, err := pricing.LoadOrSeed(p.PricingPath)
	if err != nil {
		_ = s.Close()
		return p, nil, nil, nil, err
	}
	calc := pricing.NewCalculator(tbl)
	tr := tasks.New(p.CurrentTaskPath, s)
	_ = tr.Load()
	return p, s, calc, tr, nil
}

// buildCollectors creates one Collector per enabled line-based source from the
// given config. claudeRoot is the fallback root for the claude source when
// SourceConfig.Root is empty. The opencode DB-poller source is handled separately
// by buildOpencodeIngester — it does NOT go through the Collector machinery.
func buildCollectors(sources []config.SourceConfig, sk source.Sink, claudeRoot string) []*collector.Collector {
	var cols []*collector.Collector
	for _, sc := range sources {
		if !sc.Enabled {
			continue
		}
		root := sc.Root
		switch sc.Name {
		case "claude":
			if root == "" {
				root = claudeRoot
			}
			lp := parser.ClaudeLineParser{}
			cols = append(cols, collector.NewWithSource(source.Claude, root, sk, lp, nil))
		case "codex":
			if root == "" {
				root = codex.CodexRoot()
			}
			lp := codex.NewParser()
			cols = append(cols, collector.NewWithSource(source.Codex, root, sk, lp, nil))
		case "opencode":
			// opencode is a DB-poller, not a Collector. Handled by buildOpencodeIngester.
		default:
			fmt.Fprintf(os.Stderr, "claudeops: source %q not yet implemented, skipping\n", sc.Name)
		}
	}
	return cols
}

// opencodeDefaultDBPath returns the conventional path to the opencode SQLite DB.
// It mirrors how opencode itself resolves its data directory.
func opencodeDefaultDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "opencode", "opencode.db")
}

// resolveSources returns the effective source list. When the user has not
// configured any sources explicitly, it auto-detects which tools are present on
// disk — claude always, plus codex and opencode when their data exists — so
// multi-tool usage shows up on the dashboard without manual config, the way
// CodexBar auto-detects providers. An explicit [[sources]] config always wins.
func resolveSources(settings config.Settings) []config.SourceConfig {
	return resolveSourcesWith(settings, codex.CodexRoot(), opencodeDefaultDBPath())
}

// resolveSourcesWith is the testable core of resolveSources: it probes the
// given codex sessions dir and opencode DB path for existence.
func resolveSourcesWith(settings config.Settings, codexRoot, opencodeDB string) []config.SourceConfig {
	if len(settings.Sources) > 0 {
		return settings.Sources
	}
	srcs := []config.SourceConfig{{Name: "claude", Enabled: true, Format: "jsonl"}}
	if dirExists(codexRoot) {
		srcs = append(srcs, config.SourceConfig{Name: "codex", Enabled: true, Format: "jsonl"})
	}
	if fileExists(opencodeDB) {
		srcs = append(srcs, config.SourceConfig{Name: "opencode", Enabled: true})
	}
	return srcs
}

func fileExists(p string) bool {
	if p == "" {
		return false
	}
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func dirExists(p string) bool {
	if p == "" {
		return false
	}
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// buildOpencodeIngester builds an opencode Ingester when the opencode source is
// enabled in sources, or returns nil if it is absent or disabled.
// s is the claudeops store (used for watermark persistence).
func buildOpencodeIngester(sources []config.SourceConfig, s *store.Store, sk source.Sink) source.Ingester {
	for _, sc := range sources {
		if sc.Name != "opencode" {
			continue
		}
		if !sc.Enabled {
			return nil
		}
		dbPath := sc.Root
		if dbPath == "" {
			dbPath = opencodeDefaultDBPath()
		}
		return ocingester.NewIngester(dbPath, s, sk)
	}
	return nil
}

func cmdTUI() error {
	p, s, calc, tr, err := openCore()
	if err != nil {
		return err
	}
	defer s.Close()

	settings, err := config.LoadOrCreate(p.ConfigPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "claudeops: config:", err)
		// Fall back to defaults rather than refuse to start.
		settings = config.DefaultSettings()
	}

	uClient := usage.New(p.ClaudeCreds)
	if settings.Usage.CacheTTLSeconds > 0 {
		uClient.CacheTTL = time.Duration(settings.Usage.CacheTTLSeconds) * time.Second
	}

	// Build multi-collector using source seam. Auto-detects codex/opencode
	// when present (unless the user configured sources explicitly).
	sink := source.NewStoreSinkWithTasks(s, calc, tr)
	srcs := resolveSources(settings)
	cols := buildCollectors(srcs, sink, p.ClaudeProjects)
	// Fallback to legacy collector if no source-seam collectors were built.
	if len(cols) == 0 {
		cols = append(cols, collector.New(p.ClaudeProjects, s, calc, tr))
	}

	// opencode DB-poller: independent of the Collector loop.
	ocIng := buildOpencodeIngester(srcs, s, sink)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, col := range cols {
		col := col // capture
		go func() {
			// Best-effort: cold ingest, then watch.
			_ = col.Watch(ctx)
		}()
	}
	if ocIng != nil {
		go func() {
			_ = ocIng.Watch(ctx)
		}()
	}

	model := tui.NewWithSettings(s, uClient, tr, settings, calc.Updated(), version)
	model.ConfigPath = p.ConfigPath
	model.ProjectsRoot = p.ClaudeProjects
	model.LiveDir = p.LiveDir
	// Additional live-quota providers beyond the bespoke Anthropic block.
	// Each is skipped silently when its credentials are absent.
	model.Providers = provider.NewRegistry(
		provider.NewCodex(),
		provider.NewCopilot(),
		provider.NewGemini(),
	)
	// User-defined providers: any service with a token + HTTP endpoint can be
	// tracked via ~/.claudeops/providers.toml without a code change.
	if gens, err := provider.LoadGeneric(filepath.Join(p.DataDir, "providers.toml")); err == nil {
		for _, g := range gens {
			model.Providers.Register(g)
		}
	}
	prog := tea.NewProgram(model, tea.WithAltScreen())
	_, err = prog.Run()
	return err
}

func cmdIngest() error {
	p, s, calc, tr, err := openCore()
	if err != nil {
		return err
	}
	defer s.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	ing, unk, perr := ingestAllSources(ctx, p, s, calc, tr)
	fmt.Printf("ingested: %d   unknown: %d   parse_errors: %d\n", ing, unk, perr)
	return nil
}

// cmdReingest clears the derived event store and rebuilds it from the source
// files. Pre-0.4 installs stored one inflated row per assistant content block;
// re-ingesting under the corrected dedup key produces accurate all-time usage.
// Tasks and config are preserved. Destructive to the event store, so it
// confirms first unless --yes is passed.
func cmdReingest(args []string) error {
	fs := flag.NewFlagSet("reingest", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "skip the confirmation prompt")
	if err := fs.Parse(args); err != nil {
		return err
	}

	p, s, calc, tr, err := openCore()
	if err != nil {
		return err
	}
	defer s.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if !*yes {
		fmt.Print("This clears the local event store and rebuilds it from source files\n" +
			"(tasks and config are kept). Continue? [y/N] ")
		var resp string
		_, _ = fmt.Scanln(&resp)
		if resp != "y" && resp != "Y" && resp != "yes" {
			fmt.Println("aborted")
			return nil
		}
	}

	if err := s.ResetIngestedData(ctx); err != nil {
		return fmt.Errorf("reset: %w", err)
	}
	ing, unk, perr := ingestAllSources(ctx, p, s, calc, tr)
	fmt.Printf("reingested: %d   unknown: %d   parse_errors: %d\n", ing, unk, perr)
	return nil
}

// ingestAllSources runs a one-shot cold ingest of every enabled source
// (claude + codex collectors, plus the opencode DB poller) and returns the
// aggregate ingested / unknown / parse-error counts. It mirrors the cold-ingest
// pass cmdTUI runs on startup, so CLI ingest and the TUI agree on coverage.
func ingestAllSources(ctx context.Context, p config.Paths, s *store.Store, calc *pricing.Calculator, tr *tasks.Tracker) (ingested, unknown, parseErrors int64) {
	sink := source.NewStoreSinkWithTasks(s, calc, tr)
	srcs := resolveSources(config.DefaultSettings())
	if settings, err := config.LoadOrCreate(p.ConfigPath); err == nil {
		srcs = resolveSources(settings)
	}
	cols := buildCollectors(srcs, sink, p.ClaudeProjects)
	if len(cols) == 0 {
		cols = append(cols, collector.New(p.ClaudeProjects, s, calc, tr))
	}
	for _, col := range cols {
		if err := col.IngestExisting(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "claudeops: ingest: %v\n", err)
		}
		ingested += col.IngestedCount()
		unknown += col.UnknownCount()
		parseErrors += col.ParseErrorCount()
	}
	if ocIng := buildOpencodeIngester(srcs, s, sink); ocIng != nil {
		if err := ocIng.IngestExisting(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "claudeops: opencode ingest: %v\n", err)
		}
	}
	return ingested, unknown, parseErrors
}

func cmdTask(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("task: missing subcommand (start|stop|list)")
	}
	_, s, _, tr, err := openCore()
	if err != nil {
		return err
	}
	defer s.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	switch args[0] {
	case "start":
		if len(args) < 2 {
			return fmt.Errorf("task start: missing name")
		}
		t, err := tr.Start(ctx, args[1])
		if err != nil {
			return err
		}
		fmt.Printf("started task %s (%s)\n", t.Name, t.ID)
		return nil
	case "stop":
		if err := tr.Stop(ctx); err != nil {
			return err
		}
		fmt.Println("stopped current task")
		return nil
	case "list":
		ts, err := s.TaskAggregates(ctx)
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tSTARTED\tENDED\tEVENTS\t€")
		for _, t := range ts {
			ended := "—"
			if t.EndedAt != nil {
				ended = t.EndedAt.Format(time.RFC3339)
			}
			id := t.ID
			if len(id) > 8 {
				id = id[:8]
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%.4f\n",
				id, t.Name, t.StartedAt.Format(time.RFC3339), ended, t.Events, t.CostEUR)
		}
		return w.Flush()
	default:
		return fmt.Errorf("task: unknown subcommand %q", args[0])
	}
}

func cmdHooks(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("hooks: missing subcommand (install|uninstall|status|handle)")
	}
	p, err := config.Default()
	if err != nil {
		return err
	}
	switch args[0] {
	case "install":
		bin, err := resolveBinary()
		if err != nil {
			return err
		}
		if err := hooks.Install(p.ClaudeSettings, bin); err != nil {
			return err
		}
		fmt.Printf("installed claudeops hooks into %s\n", p.ClaudeSettings)
		fmt.Printf("binary: %s\n", bin)
		return nil
	case "uninstall":
		if err := hooks.Uninstall(p.ClaudeSettings); err != nil {
			return err
		}
		fmt.Printf("removed claudeops hooks from %s\n", p.ClaudeSettings)
		return nil
	case "status":
		bin, _ := resolveBinary()
		r, err := hooks.Status(p.ClaudeSettings, bin)
		if err != nil {
			return err
		}
		fmt.Printf("settings: %s\n", r.SettingsPath)
		fmt.Printf("binary:   %s (exists: %v)\n", r.Binary, r.BinaryExists)
		for _, ev := range hooks.ManagedEvents {
			mark := "✗"
			if r.Events[ev] {
				mark = "✓"
			}
			fmt.Printf("  %s %s\n", mark, ev)
		}
		return nil
	case "handle":
		// Invoked by Claude Code itself. Stay silent on success, log to stderr
		// on failure, always exit 0 so we never block the user's session.
		if err := hooks.Handle(os.Stdin, p.LiveDir); err != nil {
			fmt.Fprintln(os.Stderr, "claudeops: hook handle:", err)
		}
		return nil
	default:
		return fmt.Errorf("hooks: unknown subcommand %q", args[0])
	}
}

func resolveBinary() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return exe, nil
	}
	return resolved, nil
}

func cmdPush(args []string) error {
	fs := flag.NewFlagSet("push", flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "build payload and print to stdout, do not send")
	since := fs.String("since", "", "override window start (RFC3339)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	var sinceTime *time.Time
	if *since != "" {
		t, err := time.Parse(time.RFC3339, *since)
		if err != nil {
			return fmt.Errorf("push: --since: invalid RFC3339 time %q: %w", *since, err)
		}
		sinceTime = &t
	}

	p, s, _, _, err := openCore()
	if err != nil {
		return err
	}
	defer s.Close()

	settings, err := config.Load(p.ConfigPath)
	if err != nil {
		return fmt.Errorf("push: load config: %w", err)
	}

	credsPath := filepath.Join(os.Getenv("HOME"), ".claude", ".credentials.json")
	pusher := export.New(s, settings.Export, export.NewFileCredReader(credsPath),
		&http.Client{Timeout: 30 * time.Second}, os.Stdout)

	ctx := context.Background()
	result, err := pusher.Push(ctx, export.PushOptions{DryRun: *dryRun, Since: sinceTime})
	if err != nil {
		return err
	}
	if !result.DryRun {
		fmt.Fprintf(os.Stdout, "pushed %d data points — window %s → %s\n",
			result.DataPoints,
			result.PeriodFrom.Format(time.RFC3339),
			result.PeriodTo.Format(time.RFC3339))
	}
	return nil
}

func cmdOTelConfig(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: claudeops otel-config apply|status|remove")
	}

	p, err := config.Default()
	if err != nil {
		return err
	}
	cfg, err := config.Load(p.ConfigPath)
	if err != nil {
		return fmt.Errorf("otel-config: load config: %w", err)
	}

	settingsJSONPath := filepath.Join(os.Getenv("HOME"), ".claude", "settings.json")

	switch args[0] {
	case "apply":
		if !cfg.Export.ClaudeOTel.Enabled {
			return fmt.Errorf("claude_otel is disabled — set [export.claude_otel] enabled = true in config.toml")
		}
		if err := export.ApplyOTelConfig(settingsJSONPath, cfg.Export); err != nil {
			return err
		}
		fmt.Printf("applied OTel config to %s\n", settingsJSONPath)
		return nil
	case "status":
		status, err := export.StatusOTelConfig(settingsJSONPath)
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "applied:\t%v\n", status.Applied)
		for _, k := range export.ManagedOTelKeys {
			if v, ok := status.Values[k]; ok {
				fmt.Fprintf(w, "%s:\t%s\n", k, v)
			}
		}
		return w.Flush()
	case "remove":
		if err := export.RemoveOTelConfig(settingsJSONPath); err != nil {
			return err
		}
		fmt.Printf("removed OTel config from %s\n", settingsJSONPath)
		return nil
	default:
		return fmt.Errorf("otel-config: unknown subcommand %q", args[0])
	}
}
