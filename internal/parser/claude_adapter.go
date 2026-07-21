package parser

import (
	"github.com/fullfran/claudeops-tui/internal/source"
)

// ClaudeLineParser wraps the existing ParseLine function and implements
// source.LineParser. This is the Claude adapter for the source seam.
// Zero behavioral change: ParseLine is called byte-for-byte identically;
// the result is mapped to source.Record values.
type ClaudeLineParser struct{}

// ParseLine implements source.LineParser.
// Returns (nil, nil) for unknown events (no usage), (nil, err) only on bad JSON.
func (ClaudeLineParser) ParseLine(line []byte, _ source.LineContext) ([]source.Record, error) {
	ev, err := ParseLine(line)
	if err != nil {
		return nil, err
	}
	switch e := ev.(type) {
	case AssistantEvent:
		r := source.Record{
			Source:        source.Claude,
			UUID:          e.DedupUUID(),
			SessionID:     e.Session,
			CWD:           e.CWD,
			Type:          "assistant",
			Model:         e.Model,
			TS:            e.TS,
			In:            e.InTokens,
			Out:           e.OutTokens,
			CacheRead:     e.CacheReadTokens,
			CacheCreate:   e.CacheCreateTokens,
			CacheCreate1h: e.CacheCreate1hTokens,
		}
		return []source.Record{r}, nil
	case UserEvent:
		r := source.Record{
			Source:    source.Claude,
			UUID:      e.UUID,
			SessionID: e.Session,
			CWD:       e.CWD,
			Type:      "user",
			TS:        e.TS,
		}
		return []source.Record{r}, nil
	default:
		// UnknownEvent and any future types: return nil, nil (soft-fail contract).
		return nil, nil
	}
}
