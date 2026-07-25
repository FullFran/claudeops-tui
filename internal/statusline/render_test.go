package statusline

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/fullfran/claudeops-tui/internal/usage"
)

var fixedNow = time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)

func bucket(util float64, resetsIn time.Duration) *usage.Bucket {
	return &usage.Bucket{Utilization: util, ResetsAt: fixedNow.Add(resetsIn)}
}

func TestRenderCompact(t *testing.T) {
	cases := []struct {
		name string
		snap usage.Snapshot
		opts Options
		want string
	}{
		{
			name: "five hour only",
			snap: usage.Snapshot{FiveHour: bucket(42, time.Hour)},
			want: "5h 42%",
		},
		{
			name: "five hour and seven day",
			snap: usage.Snapshot{FiveHour: bucket(42, time.Hour), SevenDay: bucket(18, 0)},
			want: "5h 42% · 7d 18%",
		},
		{
			// Plans without a quota get null from the API. Nothing should be
			// invented for a bucket the account does not have.
			name: "no buckets renders empty",
			snap: usage.Snapshot{},
			want: "",
		},
		{
			name: "per model buckets follow the shared order",
			snap: usage.Snapshot{
				FiveHour:       bucket(10, time.Hour),
				SevenDayOpus:   bucket(70, 0),
				SevenDaySonnet: bucket(30, 0),
			},
			want: "5h 10% · 7d (opus) 70% · 7d (sonnet) 30%",
		},
		{
			name: "utilisation rounds to whole percent",
			snap: usage.Snapshot{FiveHour: bucket(42.6, time.Hour)},
			want: "5h 43%",
		},
		{
			name: "reset appends time left in the five hour window",
			snap: usage.Snapshot{FiveHour: bucket(42, 2*time.Hour+12*time.Minute)},
			opts: Options{Reset: true},
			want: "5h 42% · ↻2h12m",
		},
		{
			// A window that already elapsed must not render a negative countdown.
			name: "expired reset is omitted",
			snap: usage.Snapshot{FiveHour: bucket(42, -time.Hour)},
			opts: Options{Reset: true},
			want: "5h 42%",
		},
		{
			name: "extra usage appears when enabled",
			snap: usage.Snapshot{
				FiveHour:   bucket(10, time.Hour),
				ExtraUsage: &usage.ExtraUsage{IsEnabled: true, Utilization: ptr(25.0)},
			},
			want: "5h 10% · extra 25%",
		},
		{
			name: "extra usage hidden when disabled",
			snap: usage.Snapshot{
				FiveHour:   bucket(10, time.Hour),
				ExtraUsage: &usage.ExtraUsage{IsEnabled: false, Utilization: ptr(25.0)},
			},
			want: "5h 10%",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.opts.Now = fixedNow
			got, err := Render(tc.snap, nil, tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestRenderCompactColour(t *testing.T) {
	cases := []struct {
		util   float64
		colour string
	}{
		{10, colourOK},
		{59.9, colourOK},
		{60, colourWarn},
		{84.9, colourWarn},
		{85, colourCrit},
		{100, colourCrit},
	}
	for _, tc := range cases {
		snap := usage.Snapshot{FiveHour: bucket(tc.util, time.Hour)}
		got, err := Render(snap, nil, Options{Color: true, Now: fixedNow})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, tc.colour) {
			t.Errorf("util %.1f: got %q want colour %s", tc.util, got, tc.colour)
		}
		if !strings.HasSuffix(got, colourOff) {
			t.Errorf("util %.1f: %q does not reset colour", tc.util, got)
		}
	}
}

func TestRenderCompactCustomThresholds(t *testing.T) {
	snap := usage.Snapshot{FiveHour: bucket(50, time.Hour)}
	got, err := Render(snap, nil, Options{Color: true, WarnAt: 40, CritAt: 90, Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, colourWarn) {
		t.Errorf("50%% with warn-at 40 should be amber, got %q", got)
	}
}

func TestRenderPlain(t *testing.T) {
	snap := usage.Snapshot{
		FiveHour: bucket(42, 90*time.Minute),
		SevenDay: bucket(18, 0),
		ExtraUsage: &usage.ExtraUsage{
			IsEnabled:    true,
			Utilization:  ptr(25.0),
			UsedCredits:  ptr(12.5),
			MonthlyLimit: ptr(50.0),
		},
	}
	got, err := Render(snap, nil, Options{Format: FormatPlain, Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"5h", "42.0%", "resets in 1h30m", "7d", "18.0%", "$12.50 of $50.00"} {
		if !strings.Contains(got, want) {
			t.Errorf("plain output missing %q:\n%s", want, got)
		}
	}
}

func TestRenderJSONRoundTrips(t *testing.T) {
	snap := usage.Snapshot{FiveHour: bucket(42, time.Hour), SevenDay: bucket(18, 0)}
	got, err := Render(snap, nil, Options{Format: FormatJSON, Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	var back []Group
	if err := json.Unmarshal([]byte(got), &back); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if len(back) != 1 || back[0].Provider != ClaudeProvider {
		t.Fatalf("expected one claude group, got %s", got)
	}
	if len(back[0].Windows) != 2 || back[0].Windows[0].Label != "5h" || back[0].Windows[0].Utilization != 42 {
		t.Errorf("windows did not survive the round trip: %s", got)
	}
	// The JSON contract is snake_case and must not drift with internal types.
	for _, want := range []string{`"provider"`, `"windows"`, `"label"`, `"utilization"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing key %s in %s", want, got)
		}
	}
}

func TestShortDuration(t *testing.T) {
	cases := map[time.Duration]string{
		30 * time.Second:             "<1m",
		time.Minute:                  "1m",
		45 * time.Minute:             "45m",
		time.Hour:                    "1h00m",
		2*time.Hour + 12*time.Minute: "2h12m",
		4*time.Hour + 5*time.Minute:  "4h05m",
		time.Hour + 59*time.Minute + 30*time.Second: "2h00m", // rounds to the minute
		23*time.Hour + 59*time.Minute:               "23h59m",
		24 * time.Hour:                              "1d00h",
		// The seven day window: "108h04m" is accurate and unreadable.
		108*time.Hour + 4*time.Minute: "4d12h",
		167 * time.Hour:               "6d23h",
	}
	for d, want := range cases {
		if got := shortDuration(d); got != want {
			t.Errorf("%v: got %q want %q", d, got, want)
		}
	}
}

func ptr[T any](v T) *T { return &v }
