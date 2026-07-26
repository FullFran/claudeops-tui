package usage

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)

func snapWith(util float64, resetsIn time.Duration) Snapshot {
	return Snapshot{FiveHour: &Bucket{Utilization: util, ResetsAt: t0.Add(resetsIn)}}
}

func historyOf(samples ...Sample) History {
	return History{Windows: map[string][]Sample{"5h": samples}}
}

func TestForecastNeedsTwoSamples(t *testing.T) {
	h := historyOf(Sample{At: t0, Utilization: 10})
	if f := h.Forecasts(snapWith(10, time.Hour), t0); len(f) != 0 {
		t.Errorf("one sample is not a rate, got %+v", f)
	}
}

func TestForecastNeedsAMeaningfulSpan(t *testing.T) {
	// Quota moves in steps as requests land. A slope across two readings a
	// minute apart is noise, and projecting from it produces a confident
	// number that is wrong.
	h := historyOf(
		Sample{At: t0, Utilization: 10},
		Sample{At: t0.Add(time.Minute), Utilization: 12},
	)
	if f := h.Forecasts(snapWith(12, 4*time.Hour), t0.Add(time.Minute)); len(f) != 0 {
		t.Errorf("a one-minute span should not forecast, got %+v", f)
	}
}

func TestForecastProjectsFromTheObservedRate(t *testing.T) {
	// 20 points in one hour → 20/h. At 30% used, 70 remain → 3.5 hours.
	h := historyOf(
		Sample{At: t0, Utilization: 10},
		Sample{At: t0.Add(time.Hour), Utilization: 30},
	)
	now := t0.Add(time.Hour)
	f := h.Forecasts(snapWith(30, 5*time.Hour), now)
	if len(f) != 1 {
		t.Fatalf("expected one forecast, got %+v", f)
	}
	if got := f[0].RatePerHour; got < 19.9 || got > 20.1 {
		t.Errorf("rate = %.2f want ~20", got)
	}
	want := 3*time.Hour + 30*time.Minute
	if d := f[0].ExhaustedIn - want; d > time.Minute || d < -time.Minute {
		t.Errorf("ExhaustedIn = %v want ~%v", f[0].ExhaustedIn, want)
	}
}

func TestForecastFlagsOnlyExhaustionBeforeTheReset(t *testing.T) {
	h := historyOf(
		Sample{At: t0, Utilization: 10},
		Sample{At: t0.Add(time.Hour), Utilization: 30},
	)
	now := t0.Add(time.Hour)

	// Runs out in 3.5h, window resets in 5h → you have a problem.
	f := h.Forecasts(snapWith(30, 5*time.Hour), now)
	if !f[0].BeforeReset {
		t.Error("exhaustion before the reset should be flagged")
	}

	// Same rate, but the window resets in an hour → you do not.
	f = h.Forecasts(snapWith(30, time.Hour), now)
	if f[0].BeforeReset {
		t.Error("a reset that lands first is not a problem")
	}
}

func TestForecastIgnoresFlatAndFallingUsage(t *testing.T) {
	now := t0.Add(time.Hour)
	flat := historyOf(
		Sample{At: t0, Utilization: 30},
		Sample{At: now, Utilization: 30},
	)
	if f := flat.Forecasts(snapWith(30, 4*time.Hour), now); len(f) != 0 {
		t.Errorf("flat usage never exhausts, got %+v", f)
	}
	falling := historyOf(
		Sample{At: t0, Utilization: 40},
		Sample{At: now, Utilization: 30},
	)
	if f := falling.Forecasts(snapWith(30, 4*time.Hour), now); len(f) != 0 {
		t.Errorf("falling usage should not forecast, got %+v", f)
	}
}

func TestRecordDropsHistoryAcrossAReset(t *testing.T) {
	// Utilisation jumping back down means a new window. Keeping the old samples
	// would flatten the slope and under-report the rate on the new one.
	var h History
	h.Record(snapWith(80, time.Hour), t0)
	h.Record(snapWith(90, time.Hour), t0.Add(30*time.Minute))
	h.Record(snapWith(5, 5*time.Hour), t0.Add(time.Hour)) // reset happened

	got := h.Windows["5h"]
	if len(got) != 1 {
		t.Fatalf("history should restart at the reset, got %+v", got)
	}
	if got[0].Utilization != 5 {
		t.Errorf("kept the wrong sample: %+v", got[0])
	}
}

func TestRecordIsBounded(t *testing.T) {
	// A long-running machine must not grow the cache file without limit.
	var h History
	for i := range maxSamples * 2 {
		h.Record(snapWith(float64(i%100), time.Hour), t0.Add(time.Duration(i)*time.Minute))
	}
	if n := len(h.Windows["5h"]); n > maxSamples {
		t.Errorf("history unbounded: %d samples", n)
	}
}

func TestForecastSkipsAnExhaustedWindow(t *testing.T) {
	// At 100% the percentage already says everything; a countdown to a point
	// already passed would be nonsense.
	h := historyOf(
		Sample{At: t0, Utilization: 80},
		Sample{At: t0.Add(time.Hour), Utilization: 100},
	)
	if f := h.Forecasts(snapWith(100, time.Hour), t0.Add(time.Hour)); len(f) != 0 {
		t.Errorf("a full window needs no forecast, got %+v", f)
	}
}

func TestForecastIgnoresWindowsThePlanNoLongerHas(t *testing.T) {
	// History can outlive a bucket if the plan changes; projecting one the
	// snapshot no longer reports would invent a window.
	h := historyOf(
		Sample{At: t0, Utilization: 10},
		Sample{At: t0.Add(time.Hour), Utilization: 30},
	)
	if f := h.Forecasts(Snapshot{}, t0.Add(time.Hour)); len(f) != 0 {
		t.Errorf("no bucket, no forecast, got %+v", f)
	}
}
