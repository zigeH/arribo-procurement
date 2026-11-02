// Package extract turns supplier messages into typed order events.
//
// Two implementations share the Extractor interface:
//   - Rules: deterministic zh/en baseline. Used in tests and evals; good on
//     common phrasings, brittle on anything unusual.
//   - LLM: Anthropic Messages API with JSON output. Used when configured;
//     never in CI.
//
// Stdlib only.
package extract

import (
	"context"
	"time"

	"github.com/zigeH/arribo-procurement/internal/inbox"
)

// Event is one thing a message says about an order.
type Event struct {
	MessageID  string          `json:"message_id"`
	Type       inbox.EventType `json:"type"`
	PONumber   string          `json:"po_number,omitempty"` // normalized digits
	LineNo     int             `json:"line_no,omitempty"`   // 0 = not stated
	PartNumber string          `json:"part_number,omitempty"`
	Date       *time.Time      `json:"date,omitempty"` // new delivery date (date_change)
	Qty        *float64        `json:"qty,omitempty"`  // shipped (partial_ship) or rejected (quality_issue)
	Price      *float64        `json:"price,omitempty"`
	Currency   string          `json:"currency,omitempty"`
	Confidence float64         `json:"confidence"`
	Quote      string          `json:"quote"` // the sentence the event came from
}

// Extractor reads one message.
type Extractor interface {
	Extract(ctx context.Context, m inbox.Message) ([]Event, error)
	Name() string
}
