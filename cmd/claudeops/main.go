// Command claudeops is the entrypoint. With no subcommand it launches the
// TUI dashboard. Subcommands are: task start|stop|list, ingest, update, version.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"text/tabwriter"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/fullfran/claudeops-tui/internal/collector"
	"github.com/fullfran/claudeops-tui/internal/config"
	"github.com/fullfran/claudeops-tui/internal/hooks"
	"github.com/fullfran/claudeops-tui/internal/mcpserver"
	"github.com/fullfran/claudeops-tui/internal/pricing"
	"github.com/fullfran/claudeops-tui/internal/store"
	"github.com/fullfran/claudeops-tui/internal/tasks"
	"github.com/fullfran/claudeops-tui/internal/tui"
	"github.com/fullfran/claudeops-tui/internal/usage"
)

const version = "0.1.0"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "claudeops:", err)
		os.Exit(1)
	}
}

var (
	runMCPCommand    = cmdMCP
	runTaskCommand   = cmdTask
	runIngestCommand = cmdIngest
	runUpdateCommand = cmdUpdate
	runHooksCommand  = cmdHooks
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
	case "update":
		return runUpdateCommand()
	case "hooks":
		return runHooksCommand(args[1:])
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
  claudeops                       launch the dashboard TUI (default)
  claudeops task start "<name>"   start a task
  claudeops task stop             stop the current task
  claudeops task list             list all tasks
  claudeops ingest                one-shot ingest of existing JSONL files
  claudeops update                update the installed CLI
  claudeops hooks install         register Claude Code hooks for live status
  claudeops hooks uninstall       remove claudeops hooks from settings.json
  claudeops hooks status          show which hooks are registered
  claudeops mcp                   start MCP server over stdio
  claudeops version               print version

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

	// Embedded collector goroutine.
	col := collector.New(p.ClaudeProjects, s, calc, tr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		// Best-effort: cold ingest, then watch.
		_ = col.Watch(ctx)
	}()

	model := tui.NewWithSettings(s, uClient, tr, settings, calc.Updated(), version)
	model.ConfigPath = p.ConfigPath
	model.ProjectsRoot = p.ClaudeProjects
	model.LiveDir = p.LiveDir
	prog := tea.NewProgram(model, tea.WithAltScreen())
	_, err = prog.Run()
	return err
}

func cmdIngest() error {
	_, s, calc, tr, err := openCore()
	if err != nil {
		return err
	}
	defer s.Close()
	p, _ := config.Default()
	col := collector.New(p.ClaudeProjects, s, calc, tr)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := col.IngestExisting(ctx); err != nil {
		return err
	}
	fmt.Printf("ingested: %d   unknown: %d   parse_errors: %d\n",
		col.IngestedCount(), col.UnknownCount(), col.ParseErrorCount())
	return nil
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
