package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/fullfran/claudeops-tui/internal/pricing"
)

func TestPricingWarningLandsInStatusLine(t *testing.T) {
	var sent []tea.Msg
	calc := pricing.Calculator{}
	// The sink must satisfy pricing.Calculator.OnWarn.
	calc.OnWarn = PricingWarnSink(func(msg tea.Msg) { sent = append(sent, msg) })
	calc.OnWarn("claude-fable-5-20260101")

	if len(sent) != 1 {
		t.Fatalf("expected 1 message, got %d", len(sent))
	}
	m := sizedTestModel(t, 120, 30)
	mm, _ := m.Update(sent[0])
	if !strings.Contains(mm.(Model).statusMsg, "claude-fable-5-20260101") {
		t.Fatalf("status line missing the model name: %q", mm.(Model).statusMsg)
	}
	if !strings.Contains(mm.(Model).View(), "claude-fable-5-20260101") {
		t.Error("pricing warning should be visible in the rendered view")
	}
}
