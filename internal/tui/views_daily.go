package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/fullfran/claudeops-tui/internal/store"
)

var (
	daySelectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("12"))
	breadcrumbStyle  = lipgloss.NewStyle().Faint(true)
	breadcrumbActive = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	previewBox       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("8")).Padding(0, 1)
)

// renderDayBrowse draws the day browser: a scrollable list of the last 30 days
// with cost, events, sessions per day, sparkline context, and a preview card
// for the selected day.
func renderDayBrowse(m Model) string {
	var sb strings.Builder
	th := m.Settings.Dashboard.Thresholds

	// Breadcrumb.
	sb.WriteString(breadcrumbStyle.Render("Dashboard") + dimStyle.Render(" > ") + breadcrumbActive.Render("Daily breakdown") + "\n\n")

	// Sparkline at the top for context, with pointer to selected day.
	if len(m.Daily) > 0 {
		series := lastN(m.Daily, 30)
		sb.WriteString("  " + sparkline(series, th) + "\n")

		// Pointer line: show a caret under the selected day's position.
		offset := m.dayCursor - (len(m.Daily) - len(series))
		if offset >= 0 && offset < len(series) {
			pointer := strings.Repeat(" ", offset) + "^"
			sb.WriteString("  " + dimStyle.Render(pointer) + "\n")
		}

		first, last := series[0].Date.Format("01-02"), series[len(series)-1].Date.Format("01-02")
		sb.WriteString("  " + dimStyle.Render(first+" "+strings.Repeat("·", len(series)-4)+" "+last) + "\n\n")
	}

	// Preview card for the selected day.
	if m.dayCursor >= 0 && m.dayCursor < len(m.Daily) {
		sel := m.Daily[m.dayCursor]
		isToday := isLocalToday(sel.Date)
		dateLabel := sel.Date.Format("Monday, 2006-01-02")
		if isToday {
			dateLabel += " (today)"
		}
		costStr := colorForSpend(sel.CostEUR, th).Render(fmt.Sprintf("€%.4f", sel.CostEUR))
		preview := headerStyle.Render(dateLabel) + "\n"
		if sel.Events == 0 {
			preview += dimStyle.Render("no activity")
		} else {
			preview += fmt.Sprintf("%s   %d events   %d sessions", costStr, sel.Events, sel.Sessions)
			preview += "\n" + dimStyle.Render("press enter for hourly + model + session detail")
		}
		sb.WriteString(previewBox.Render(preview) + "\n\n")
	}

	// Column header.
	header := fmt.Sprintf("  %-14s  %8s  %8s  %10s  %s", "DATE", "EVENTS", "SESSIONS", "COST", "")
	sb.WriteString(dimStyle.Render(header) + "\n")
	sb.WriteString("  " + dimStyle.Render(strings.Repeat("─", 60)) + "\n")

	// List days newest-first for natural browsing.
	for i := len(m.Daily) - 1; i >= 0; i-- {
		d := m.Daily[i]
		costStr := fmt.Sprintf("€%.4f", d.CostEUR)
		costColored := colorForSpend(d.CostEUR, th).Render(costStr)

		// Mini bar for visual weight.
		barStr := miniBar(d.CostEUR, maxCostInDaily(m.Daily), 12)

		dateLabel := d.Date.Format("2006-01-02 Mon")
		todayTag := ""
		if isLocalToday(d.Date) {
			todayTag = " *"
		}

		if i == m.dayCursor {
			line := fmt.Sprintf(" > %-14s%s  %8d  %8d  %10s  %s",
				dateLabel, todayTag, d.Events, d.Sessions, costStr, barStr)
			sb.WriteString(cursorLineMarker + daySelectedStyle.Render(line) + "\n")
		} else {
			if d.Events == 0 {
				// Dim empty days.
				sb.WriteString(dimStyle.Render(fmt.Sprintf("   %-14s%s  %8d  %8d  %10s  %s",
					dateLabel, todayTag, d.Events, d.Sessions, costStr, barStr)) + "\n")
			} else {
				sb.WriteString(fmt.Sprintf("   %-14s%s  %8d  %8d  %10s  %s\n",
					dateLabel, todayTag, d.Events, d.Sessions, costColored, barStr))
			}
		}
	}

	sb.WriteString("\n")
	return sb.String()
}

// renderDayDetail draws the full breakdown for a single day.
func renderDayDetail(m Model) string {
	if m.dayDetail == nil {
		return dimStyle.Render("loading…")
	}
	var sb strings.Builder
	d := m.dayDetail
	th := m.Settings.Dashboard.Thresholds

	// Breadcrumb.
	sb.WriteString(breadcrumbStyle.Render("Dashboard") +
		dimStyle.Render(" > ") + breadcrumbStyle.Render("Daily") +
		dimStyle.Render(" > ") + breadcrumbActive.Render(d.Date.Format("Mon 2006-01-02")) + "\n\n")

	// Summary card.
	costStyled := colorForSpend(d.Agg.CostEUR, th).Render(fmt.Sprintf("€%.4f", d.Agg.CostEUR))
	summary := headerStyle.Render(d.Date.Format("Monday, 2006-01-02"))
	if isLocalToday(d.Date) {
		summary += dimStyle.Render(" (today)")
	}
	summary += "\n" + fmt.Sprintf("%s   %d events   %d sessions", costStyled, d.Agg.Events, d.Agg.Sessions)
	sb.WriteString(previewBox.Render(summary) + "\n\n")

	// Hourly activity.
	sb.WriteString("  " + headerStyle.Render("Hourly activity") + "\n")
	sb.WriteString("  " + dimStyle.Render(strings.Repeat("─", 50)) + "\n")
	if len(d.Hourly) > 0 {
		maxH := 0.0
		for _, h := range d.Hourly {
			if h.CostEUR > maxH {
				maxH = h.CostEUR
			}
		}
		hourMap := make(map[int]struct {
			cost   float64
			events int64
		})
		for _, h := range d.Hourly {
			hourMap[h.Hour] = struct {
				cost   float64
				events int64
			}{h.CostEUR, h.Events}
		}
		for h := 0; h < 24; h++ {
			data := hourMap[h]
			hourLabel := fmt.Sprintf("  %02d:00", h)
			if data.events == 0 {
				sb.WriteString(dimStyle.Render(fmt.Sprintf("%s  %s", hourLabel, strings.Repeat("·", 20))) + "\n")
			} else {
				bar := hourlyBar(data.cost, maxH, 20)
				costStr := colorForSpend(data.cost, th).Render(fmt.Sprintf("€%.4f", data.cost))
				sb.WriteString(fmt.Sprintf("%s  %s  %s  %d ev\n", hourLabel, bar, costStr, data.events))
			}
		}
	} else {
		sb.WriteString(dimStyle.Render("  no activity") + "\n")
	}
	sb.WriteString("\n")

	// Per-model breakdown.
	sb.WriteString("  " + headerStyle.Render("Models") + "\n")
	sb.WriteString("  " + dimStyle.Render(strings.Repeat("─", 50)) + "\n")
	if len(d.Models) > 0 {
		for _, pm := range d.Models {
			costStr := colorForSpend(pm.CostEUR, th).Render(fmt.Sprintf("€%.4f", pm.CostEUR))
			pct := ""
			if d.Agg.CostEUR > 0 {
				pct = dimStyle.Render(fmt.Sprintf("  (%.0f%%)", pm.CostEUR/d.Agg.CostEUR*100))
			}
			sb.WriteString(fmt.Sprintf("  %-32s  %s  %d ev%s\n", truncate(pm.Model, 32), costStr, pm.Events, pct))
		}
	} else {
		sb.WriteString(dimStyle.Render("  no data") + "\n")
	}
	sb.WriteString("\n")

	// Sessions for the day.
	sb.WriteString("  " + headerStyle.Render("Sessions") + "\n")
	sb.WriteString("  " + dimStyle.Render(strings.Repeat("─", 50)) + "\n")
	if len(d.Sessions) > 0 {
		for _, s := range d.Sessions {
			id := truncate(s.SessionID, 10)
			costStr := colorForSpend(s.CostEUR, th).Render(fmt.Sprintf("€%.4f", s.CostEUR))
			sb.WriteString(fmt.Sprintf("  %-10s  %-26s  %s\n", id, truncate(s.ProjectName, 26), costStr))
		}
	} else {
		sb.WriteString(dimStyle.Render("  no sessions") + "\n")
	}
	sb.WriteString("\n")

	return sb.String()
}

// miniBar renders a small proportional bar using block characters.
func miniBar(value, max float64, width int) string {
	if max <= 0 || value <= 0 {
		return dimStyle.Render(strings.Repeat("░", width))
	}
	filled := int(value / max * float64(width))
	if filled < 1 {
		filled = 1
	}
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + dimStyle.Render(strings.Repeat("░", width-filled))
}

// hourlyBar renders a colored bar for a single hour.
func hourlyBar(value, max float64, width int) string {
	if max <= 0 || value <= 0 {
		return dimStyle.Render(strings.Repeat("░", width))
	}
	filled := int(value / max * float64(width))
	if filled < 1 {
		filled = 1
	}
	if filled > width {
		filled = width
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Render(strings.Repeat("█", filled)) +
		dimStyle.Render(strings.Repeat("░", width-filled))
}

// maxCostInDaily returns the max cost across all days.
func maxCostInDaily(days []store.DailyAgg) float64 {
	max := 0.0
	for _, d := range days {
		if d.CostEUR > max {
			max = d.CostEUR
		}
	}
	return max
}

// isLocalToday returns true if the given date matches today in local time.
func isLocalToday(d time.Time) bool {
	now := time.Now()
	return d.Year() == now.Year() && d.Month() == now.Month() && d.Day() == now.Day()
}
