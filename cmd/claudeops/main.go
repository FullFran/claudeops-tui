// Command claudeops is the entrypoint. With no subcommand it launches the TUI
// dashboard. Subcommands are: task start|stop|list, ingest, reingest, update,
// hooks install|uninstall|status|handle, push, otel-config apply|status|remove,
// mcp and version.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
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
  claudeops hooks handle                        handle a Claude Code hook event on stdin (invoked by Claude Code)
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
	return cmdMCPWith(p, func(s *store.Store) error {
		return mcpserver.New(s).Serve()
	})
}

// cmdMCPWith opens the store read-only and hands it to serve. The data dir and
// the database file are provisioned first: read-only mode cannot create the
// file, so registering the MCP server before ever launching the TUI would
// otherwise fail with "unable to open database file".
func cmdMCPWith(p config.Paths, serve func(*store.Store) error) error {
	if err := ensureStore(p); err != nil {
		return err
	}
	s, err := store.OpenReadOnly(p.DBPath)
	if err != nil {
		return err
	}
	defer s.Close()
	return serve(s)
}

// ensureStore creates the data dir and a migrated (possibly empty) database.
func ensureStore(p config.Paths) error {
	if err := p.EnsureDataDir(); err != nil {
		return err
	}
	s, err := store.Open(p.DBPath)
	if err != nil {
		return err
	}
	return s.Close()
}

// core bundles the dependencies every store-backed command needs.
type core struct {
	store *store.Store
	calc  *pricing.Calculator
	tasks *tasks.Tracker
}

func (c *core) close() {
	if c != nil && c.store != nil {
		_ = c.store.Close()
	}
}

func openCore() (config.Paths, *core, error) {
	p, err := config.Default()
	if err != nil {
		return p, nil, err
	}
	c, err := openCoreAt(p)
	return p, c, err
}

// openCoreAt is the testable core opener: everything it touches lives under p.
func openCoreAt(p config.Paths) (*core, error) {
	if err := p.EnsureDataDir(); err != nil {
		return nil, err
	}
	s, err := store.Open(p.DBPath)
	if err != nil {
		return nil, err
	}
	tbl, err := pricing.LoadOrSeed(p.PricingPath)
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	tr := tasks.New(p.CurrentTaskPath, s)
	_ = tr.Load()
	return &core{store: s, calc: pricing.NewCalculator(tbl), tasks: tr}, nil
}

// loadSettings reads config.toml, falling back to defaults so a broken config
// never stops a command from running.
func loadSettings(p config.Paths, errOut io.Writer) config.Settings {
	settings, err := config.LoadOrCreate(p.ConfigPath)
	if err != nil {
		fmt.Fprintln(errOut, "claudeops: config:", err)
		return config.DefaultSettings()
	}
	return settings
}

// namedCollector pairs a Collector with the source name that produced it, so
// failures and counters can be reported per source.
type namedCollector struct {
	name string
	col  *collector.Collector
}

// buildCollectors creates one Collector per enabled line-based source from the
// given config. claudeRoot is the fallback root for the claude source when
// SourceConfig.Root is empty. The opencode DB-poller source is handled separately
// by buildOpencodeIngester — it does NOT go through the Collector machinery.
func buildCollectors(sources []config.SourceConfig, sk source.Sink, claudeRoot string) []namedCollector {
	var cols []namedCollector
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
			cols = append(cols, namedCollector{sc.Name, collector.NewWithSource(source.Claude, root, sk, lp, nil)})
		case "codex":
			if root == "" {
				root = codex.CodexRoot()
			}
			lp := codex.NewParser()
			cols = append(cols, namedCollector{sc.Name, collector.NewWithSource(source.Codex, root, sk, lp, nil)})
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
	p, c, err := openCore()
	if err != nil {
		return err
	}
	defer c.close()

	settings := loadSettings(p, os.Stderr)

	// Build multi-collector using source seam. Auto-detects codex/opencode
	// when present (unless the user configured sources explicitly).
	sink := source.NewStoreSinkWithTasks(c.store, c.calc, c.tasks)
	srcs := resolveSources(settings)
	cols := collectorsFor(srcs, sink, p, c)

	// opencode DB-poller: independent of the Collector loop.
	ocIng := buildOpencodeIngester(srcs, c.store, sink)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, nc := range cols {
		nc := nc // capture
		go func() {
			// Best-effort: cold ingest, then watch.
			_ = nc.col.Watch(ctx)
		}()
	}
	if ocIng != nil {
		go func() {
			_ = ocIng.Watch(ctx)
		}()
	}

	prog := tea.NewProgram(buildTUIModel(p, settings, c), tea.WithAltScreen())
	_, err = prog.Run()
	return err
}

// buildTUIModel wires the dashboard model: usage client, provider registry and
// the paths the TUI needs to read config and live session sidecars.
func buildTUIModel(p config.Paths, settings config.Settings, c *core) tui.Model {
	uClient := usage.New(p.ClaudeCreds)
	if settings.Usage.CacheTTLSeconds > 0 {
		uClient.CacheTTL = time.Duration(settings.Usage.CacheTTLSeconds) * time.Second
	}
	model := tui.NewWithSettings(c.store, uClient, c.tasks, settings, c.calc.Updated(), version)
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
	return model
}

// collectorsFor builds the enabled line-based collectors, falling back to the
// legacy claude-only collector when no source produced one.
func collectorsFor(srcs []config.SourceConfig, sink source.Sink, p config.Paths, c *core) []namedCollector {
	cols := buildCollectors(srcs, sink, p.ClaudeProjects)
	if len(cols) == 0 {
		cols = append(cols, namedCollector{"claude", collector.New(p.ClaudeProjects, c.store, c.calc, c.tasks)})
	}
	return cols
}

func cmdIngest() error {
	p, err := config.Default()
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	return cmdIngestWith(ctx, p, buildIngestUnits, os.Stdout, os.Stderr)
}

func cmdIngestWith(ctx context.Context, p config.Paths, build unitsBuilder, out, errOut io.Writer) error {
	c, err := openCoreAt(p)
	if err != nil {
		return err
	}
	defer c.close()
	res := runIngestUnits(ctx, build(p, c), errOut)
	fmt.Fprintf(out, "ingested: %d   unknown: %d   parse_errors: %d\n",
		res.ingested, res.unknown, res.parseErrors)
	return res.err()
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

	p, err := config.Default()
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	return cmdReingestWith(ctx, p, buildIngestUnits, os.Stdin, os.Stdout, os.Stderr, *yes)
}

func cmdReingestWith(ctx context.Context, p config.Paths, build unitsBuilder,
	in io.Reader, out, errOut io.Writer, yes bool) error {
	if !yes {
		fmt.Fprint(out, "This clears the local event store and rebuilds it from source files\n"+
			"(tasks and config are kept). Continue? [y/N] ")
		sc := bufio.NewScanner(in)
		resp := ""
		if sc.Scan() {
			resp = strings.TrimSpace(sc.Text())
		}
		if resp != "y" && resp != "Y" && resp != "yes" {
			fmt.Fprintln(out, "aborted")
			return nil
		}
	}

	c, err := openCoreAt(p)
	if err != nil {
		return err
	}
	defer c.close()

	if err := c.store.ResetIngestedData(ctx); err != nil {
		return fmt.Errorf("reset: %w", err)
	}
	res := runIngestUnits(ctx, build(p, c), errOut)
	fmt.Fprintf(out, "reingested: %d   unknown: %d   parse_errors: %d\n",
		res.ingested, res.unknown, res.parseErrors)
	return res.err()
}

// ingestUnit is one named cold-ingest step: a source to drain plus accessors
// for the counters it accumulated.
type ingestUnit struct {
	name   string
	ingest func(context.Context) error
	counts func() (ingested, unknown, parseErrors int64)
}

// unitsBuilder produces the cold-ingest steps for the enabled sources.
type unitsBuilder func(config.Paths, *core) []ingestUnit

// ingestResult aggregates the counters of a one-shot ingest plus the names of
// the sources that failed.
type ingestResult struct {
	ingested    int64
	unknown     int64
	parseErrors int64
	failed      []string
}

// err reports a non-nil error when any source failed, so ingest and reingest
// exit non-zero and cron jobs can detect the breakage.
func (r ingestResult) err() error {
	if len(r.failed) == 0 {
		return nil
	}
	return fmt.Errorf("ingest failed for %d of the configured sources: %s",
		len(r.failed), strings.Join(r.failed, ", "))
}

// buildIngestUnits assembles a one-shot cold ingest of every enabled source
// (claude + codex collectors, plus the opencode DB poller). It mirrors the
// cold-ingest pass cmdTUI runs on startup, so CLI ingest and the TUI agree on
// coverage.
func buildIngestUnits(p config.Paths, c *core) []ingestUnit {
	sink := source.NewStoreSinkWithTasks(c.store, c.calc, c.tasks)
	srcs := resolveSources(loadSettings(p, io.Discard))
	var units []ingestUnit
	for _, nc := range collectorsFor(srcs, sink, p, c) {
		col := nc.col
		units = append(units, ingestUnit{
			name:   nc.name,
			ingest: col.IngestExisting,
			counts: func() (int64, int64, int64) {
				return col.IngestedCount(), col.UnknownCount(), col.ParseErrorCount()
			},
		})
	}
	if ocIng := buildOpencodeIngester(srcs, c.store, sink); ocIng != nil {
		units = append(units, ingestUnit{name: "opencode", ingest: ocIng.IngestExisting})
	}
	return units
}

// runIngestUnits drains every unit, keeps going past a failing source, and
// reports which ones failed.
func runIngestUnits(ctx context.Context, units []ingestUnit, errOut io.Writer) ingestResult {
	var res ingestResult
	for _, u := range units {
		if err := u.ingest(ctx); err != nil {
			fmt.Fprintf(errOut, "claudeops: ingest: source %s: %v\n", u.name, err)
			res.failed = append(res.failed, u.name)
		}
		if u.counts == nil {
			continue
		}
		ing, unk, perr := u.counts()
		res.ingested += ing
		res.unknown += unk
		res.parseErrors += perr
	}
	return res
}

func cmdTask(args []string) error {
	p, err := config.Default()
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	return cmdTaskWith(ctx, p, os.Stdout, args)
}

func cmdTaskWith(ctx context.Context, p config.Paths, out io.Writer, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("task: missing subcommand (start|stop|list)")
	}
	if args[0] == "start" && len(args) < 2 {
		return fmt.Errorf("task start: missing name")
	}
	c, err := openCoreAt(p)
	if err != nil {
		return err
	}
	defer c.close()

	switch args[0] {
	case "start":
		t, err := c.tasks.Start(ctx, args[1])
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "started task %s (%s)\n", t.Name, t.ID)
		return nil
	case "stop":
		if err := c.tasks.Stop(ctx); err != nil {
			return err
		}
		fmt.Fprintln(out, "stopped current task")
		return nil
	case "list":
		ts, err := c.store.TaskAggregates(ctx)
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tSTARTED\tENDED\tDURATION\tEVENTS\tIN\tOUT\tCACHE R\tCACHE W\t€")
		now := time.Now().UTC()
		for _, t := range ts {
			ended := "—"
			end := now
			if t.EndedAt != nil {
				ended = t.EndedAt.Format(time.RFC3339)
				end = *t.EndedAt
			}
			id := t.ID
			if len(id) > 8 {
				id = id[:8]
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\t%d\t%d\t%d\t%d\t%.4f\n",
				id, t.Name, t.StartedAt.Format(time.RFC3339), ended,
				formatDur(end.Sub(t.StartedAt)), t.Events,
				t.InTokens, t.OutTokens, t.CacheReadTokens, t.CacheCreateTokens, t.CostEUR)
		}
		return w.Flush()
	default:
		return fmt.Errorf("task: unknown subcommand %q", args[0])
	}
}

// formatDur renders a task duration compactly ("2d 2h", "3h 20m", "45m")
// instead of Go's default "119h59m0s".
func formatDur(d time.Duration) string {
	if d <= 0 {
		return "0m"
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}

func cmdHooks(args []string) error {
	p, err := config.Default()
	if err != nil {
		return err
	}
	return cmdHooksWith(p, os.Stdin, os.Stdout, args)
}

func cmdHooksWith(p config.Paths, in io.Reader, out io.Writer, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("hooks: missing subcommand (install|uninstall|status|handle)")
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
		fmt.Fprintf(out, "installed claudeops hooks into %s\n", p.ClaudeSettings)
		fmt.Fprintf(out, "binary: %s\n", bin)
		return nil
	case "uninstall":
		if err := hooks.Uninstall(p.ClaudeSettings); err != nil {
			return err
		}
		fmt.Fprintf(out, "removed claudeops hooks from %s\n", p.ClaudeSettings)
		return nil
	case "status":
		bin, _ := resolveBinary()
		r, err := hooks.Status(p.ClaudeSettings, bin)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "settings: %s\n", r.SettingsPath)
		fmt.Fprintf(out, "binary:   %s (exists: %v)\n", r.Binary, r.BinaryExists)
		for _, ev := range hooks.ManagedEvents {
			mark := "✗"
			if r.Events[ev] {
				mark = "✓"
			}
			fmt.Fprintf(out, "  %s %s\n", mark, ev)
		}
		return nil
	case "handle":
		// Invoked by Claude Code itself. Stay silent on success, log to stderr
		// on failure, always exit 0 so we never block the user's session.
		if err := hooks.Handle(in, p.LiveDir); err != nil {
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
	p, err := config.Default()
	if err != nil {
		return err
	}
	return cmdPushWith(context.Background(), p, os.Stdout, args)
}

func cmdPushWith(ctx context.Context, p config.Paths, out io.Writer, args []string) error {
	fs := flag.NewFlagSet("push", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
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

	c, err := openCoreAt(p)
	if err != nil {
		return err
	}
	defer c.close()

	settings, err := config.Load(p.ConfigPath)
	if err != nil {
		return fmt.Errorf("push: load config: %w", err)
	}

	pusher := export.New(c.store, settings.Export, export.NewFileCredReader(p.ClaudeCreds),
		&http.Client{Timeout: 30 * time.Second}, out)

	result, err := pusher.Push(ctx, export.PushOptions{DryRun: *dryRun, Since: sinceTime})
	if err != nil {
		return err
	}
	if !result.DryRun {
		fmt.Fprintf(out, "pushed %d data points — window %s → %s\n",
			result.DataPoints,
			result.PeriodFrom.Format(time.RFC3339),
			result.PeriodTo.Format(time.RFC3339))
	}
	return nil
}

func cmdOTelConfig(args []string) error {
	p, err := config.Default()
	if err != nil {
		return err
	}
	return cmdOTelConfigWith(p, os.Stdout, args)
}

func cmdOTelConfigWith(p config.Paths, out io.Writer, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: claudeops otel-config apply|status|remove")
	}

	cfg, err := config.Load(p.ConfigPath)
	if err != nil {
		return fmt.Errorf("otel-config: load config: %w", err)
	}

	settingsJSONPath := p.ClaudeSettings

	switch args[0] {
	case "apply":
		if !cfg.Export.ClaudeOTel.Enabled {
			return fmt.Errorf("claude_otel is disabled — set [export.claude_otel] enabled = true in config.toml")
		}
		if err := export.ApplyOTelConfig(settingsJSONPath, cfg.Export); err != nil {
			return err
		}
		fmt.Fprintf(out, "applied OTel config to %s\n", settingsJSONPath)
		return nil
	case "status":
		status, err := export.StatusOTelConfig(settingsJSONPath)
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
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
		fmt.Fprintf(out, "removed OTel config from %s\n", settingsJSONPath)
		return nil
	default:
		return fmt.Errorf("otel-config: unknown subcommand %q", args[0])
	}
}
