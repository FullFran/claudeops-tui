package tui

import (
	"strings"
	"testing"

	"github.com/fullfran/claudeops-tui/internal/config"
	"github.com/fullfran/claudeops-tui/internal/update"
)

func TestUpdateHeaderNotification(t *testing.T) {
	m := sizedTestModel(t, 120, 40)
	m.Version = "0.16.0"

	// Initially no update
	viewWithout := m.View()
	if strings.Contains(viewWithout, "available ('claudeops update')") {
		t.Errorf("expected no update hint in header, got:\n%s", viewWithout)
	}

	// When update is available
	m.UpdateAvailable = "0.17.0"
	viewWith := m.View()
	if !strings.Contains(viewWith, "↑ 0.17.0 available ('claudeops update')") {
		t.Errorf("expected update hint in header, got:\n%s", viewWith)
	}
}

func TestUpdateCheckMsgHandling(t *testing.T) {
	m := sizedTestModel(t, 120, 40)
	m.Version = "0.16.0"

	// Message with available update
	msgUpdate := updateCheckMsg{
		result: update.CheckResult{
			CurrentVersion: "0.16.0",
			LatestVersion:  "0.17.0",
			UpToDate:       false,
			Downgrade:      false,
		},
	}
	mm, _ := m.Update(msgUpdate)
	m1 := mm.(Model)
	if m1.UpdateAvailable != "0.17.0" {
		t.Errorf("UpdateAvailable = %q, want 0.17.0", m1.UpdateAvailable)
	}
	if !strings.Contains(m1.UpdateStatus, "0.17.0") {
		t.Errorf("UpdateStatus = %q, want mention of 0.17.0", m1.UpdateStatus)
	}

	// Message with up to date
	msgUpToDate := updateCheckMsg{
		result: update.CheckResult{
			CurrentVersion: "0.17.0",
			LatestVersion:  "0.17.0",
			UpToDate:       true,
		},
	}
	mm, _ = m1.Update(msgUpToDate)
	m2 := mm.(Model)
	if m2.UpdateAvailable != "" {
		t.Errorf("expected UpdateAvailable to be cleared, got %q", m2.UpdateAvailable)
	}
	if m2.UpdateStatus != "up to date" {
		t.Errorf("UpdateStatus = %q, want 'up to date'", m2.UpdateStatus)
	}

	// Message with successful auto-update
	msgAuto := updateCheckMsg{
		result: update.CheckResult{
			LatestVersion: "0.18.0",
		},
		autoRan: true,
		autoErr: nil,
	}
	mm, _ = m2.Update(msgAuto)
	m3 := mm.(Model)
	if !strings.Contains(m3.statusMsg, "updated to 0.18.0") {
		t.Errorf("statusMsg = %q, want mention of update success", m3.statusMsg)
	}
}

func TestSettingsUpdatesSection(t *testing.T) {
	items := settingsItems()
	var foundUpdates bool
	var checkItem, autoItem, actionItem *settingsItem

	for i := range items {
		item := &items[i]
		if item.section && item.label == "Updates" {
			foundUpdates = true
		}
		if item.label == "Check on launch" {
			checkItem = item
		}
		if item.label == "Auto-install" {
			autoItem = item
		}
		if item.label == "Check now" {
			actionItem = item
		}
	}

	if !foundUpdates {
		t.Fatal("Updates section not found in settings")
	}
	if checkItem == nil || autoItem == nil || actionItem == nil {
		t.Fatal("expected Check on launch, Auto-install, and Check now items in settings")
	}

	cfg := config.DefaultSettings()
	if !checkItem.get(cfg) {
		t.Errorf("Check on launch should default to true")
	}
	if autoItem.get(cfg) {
		t.Errorf("Auto-install should default to false")
	}

	// Test toggle
	checkItem.toggle(&cfg)
	if checkItem.get(cfg) {
		t.Errorf("expected Check on launch to toggle to false")
	}
	autoItem.toggle(&cfg)
	if !autoItem.get(cfg) {
		t.Errorf("expected Auto-install to toggle to true")
	}

	if actionItem.actionKey != "check_update_now" {
		t.Errorf("actionKey = %q, want 'check_update_now'", actionItem.actionKey)
	}
}
