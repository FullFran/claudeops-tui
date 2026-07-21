package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestWirePricingWarningsRoutesWarningsToTheProgram(t *testing.T) {
	p := newTestPaths(t)
	c, err := openCoreAt(p)
	if err != nil {
		t.Fatalf("openCoreAt: %v", err)
	}
	defer c.close()

	var sent []tea.Msg
	wirePricingWarnings(c, func(msg tea.Msg) { sent = append(sent, msg) })

	if c.calc.OnWarn == nil {
		t.Fatal("OnWarn was not wired")
	}
	// An id no price table can know must reach the TUI, not stderr.
	if cost := c.calc.CostFor("no-such-model-9000", 10, 10, 0, 0); cost != nil {
		t.Fatalf("cost for an unknown model = %v, want nil", *cost)
	}
	if len(sent) != 1 {
		t.Fatalf("messages sent = %d, want 1", len(sent))
	}
}

func TestWirePricingWarningsToleratesAMissingCalculator(t *testing.T) {
	wirePricingWarnings(nil, func(tea.Msg) {})
	wirePricingWarnings(&core{}, func(tea.Msg) {})
}
