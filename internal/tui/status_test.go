package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/fullfran/claudeops-tui/internal/pricing"
)

func TestStatusMessageExpiresOnTick(t *testing.T) {
	cases := []struct {
		name    string
		age     time.Duration
		wantMsg bool
	}{
		{name: "fresh status survives a tick", age: time.Second, wantMsg: true},
		{name: "expired status is cleared", age: statusTTL + time.Second, wantMsg: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := sizedTestModel(t, 120, 30)
			mm, _ := m.Update(taskActionMsg{status: "task started: demo"})
			model := mm.(Model)
			if model.statusMsg == "" {
				t.Fatal("expected a status message")
			}
			// Age the message instead of sleeping.
			model.statusUntil = model.statusUntil.Add(-tc.age)
			mm, _ = model.Update(tickMsg{})
			if got := mm.(Model).statusMsg != ""; got != tc.wantMsg {
				t.Errorf("status present = %v, want %v (%q)", got, tc.wantMsg, mm.(Model).statusMsg)
			}
		})
	}
}

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
