package usage

import (
	"sort"
	"time"
)

// Forecasting quota exhaustion needs a rate, and a rate needs two readings.
// The snapshot only carries the current utilisation, and the €/hour burn rate
// the dashboard already computes measures something else entirely — local spend
// against an editable price table, not Anthropic's own accounting of the window.
// Deriving one from the other would mean guessing the exchange rate between
// them, which is the estimation this project explicitly refuses to do.
//
// So measure it. The shared cache is written on every refresh anyway; keeping a
// short history of what was actually observed turns that into a real slope.

// maxSamples bounds the history. A 5h window sampled every few minutes needs
// far fewer than this; the cap exists so a long-running machine cannot grow the
// cache file without limit.
const maxSamples = 64

// minForecastSpan is the shortest observation window that yields a rate worth
// showing. Quota moves in steps as requests land, so a slope measured across
// two readings a minute apart is mostly noise.
const minForecastSpan = 10 * time.Minute

// Sample is one observation of a window's utilisation.
type Sample struct {
	At          time.Time `json:"at"`
	Utilization float64   `json:"utilization"`
}

// History holds recent observations per window label.
type History struct {
	Windows map[string][]Sample `json:"windows,omitempty"`
}

// Record appends the current utilisations, dropping anything older than the
// window's own reset — a rate measured across a reset is meaningless, because
// utilisation jumps back to zero.
func (h *History) Record(snap Snapshot, now time.Time) {
	if h.Windows == nil {
		h.Windows = map[string][]Sample{}
	}
	add := func(label string, b *Bucket) {
		if b == nil {
			return
		}
		prev := h.Windows[label]
		// A drop means the window reset. Everything before it describes a
		// different window and would flatten or invert the slope.
		if n := len(prev); n > 0 && b.Utilization < prev[n-1].Utilization {
			prev = nil
		}
		prev = append(prev, Sample{At: now, Utilization: b.Utilization})
		if len(prev) > maxSamples {
			prev = prev[len(prev)-maxSamples:]
		}
		h.Windows[label] = prev
	}
	add("5h", snap.FiveHour)
	add("7d", snap.SevenDay)
	for _, nb := range snap.PerModelBuckets() {
		b := nb.Bucket
		add(nb.Label, &b)
	}
}

// Forecast is a projection for one window.
type Forecast struct {
	Label string
	// RatePerHour is the observed change in utilisation points per hour.
	RatePerHour float64
	// ExhaustedIn is how long until 100% at the current rate.
	ExhaustedIn time.Duration
	// BeforeReset is true when exhaustion lands before the window resets,
	// which is the only case worth warning about.
	BeforeReset bool
}

// Forecasts projects each window that has enough signal.
//
// Returns nothing rather than guessing: too few samples, too short a span, or a
// flat or falling rate all yield no forecast. A number that appears only when
// it is grounded is worth more than one that is always there.
func (h History) Forecasts(snap Snapshot, now time.Time) []Forecast {
	current := map[string]*Bucket{}
	if snap.FiveHour != nil {
		current["5h"] = snap.FiveHour
	}
	if snap.SevenDay != nil {
		current["7d"] = snap.SevenDay
	}
	for _, nb := range snap.PerModelBuckets() {
		b := nb.Bucket
		current[nb.Label] = &b
	}

	labels := make([]string, 0, len(h.Windows))
	for l := range h.Windows {
		labels = append(labels, l)
	}
	sort.Strings(labels) // deterministic order; map iteration is not

	var out []Forecast
	for _, label := range labels {
		bucket, ok := current[label]
		if !ok || bucket == nil {
			continue
		}
		samples := h.Windows[label]
		if len(samples) < 2 {
			continue
		}
		first, last := samples[0], samples[len(samples)-1]
		span := last.At.Sub(first.At)
		if span < minForecastSpan {
			continue
		}
		delta := last.Utilization - first.Utilization
		if delta <= 0 {
			continue // flat or reset; nothing to project
		}
		rate := delta / span.Hours()
		if rate <= 0 {
			continue
		}
		remaining := 100 - bucket.Utilization
		if remaining <= 0 {
			continue // already exhausted; the percentage says so
		}
		until := time.Duration(remaining / rate * float64(time.Hour))

		f := Forecast{Label: label, RatePerHour: rate, ExhaustedIn: until}
		if !bucket.ResetsAt.IsZero() {
			f.BeforeReset = now.Add(until).Before(bucket.ResetsAt)
		}
		out = append(out, f)
	}
	return out
}
