// Package inbox defines supplier communications as Arribo ingests them
// (emails today; files and chat later). Stdlib only.
package inbox

import (
	"encoding/json"
	"os"
	"strings"
	"time"
)

// Message is one inbound supplier message.
type Message struct {
	ID          string    `json:"id"`
	Channel     string    `json:"channel"` // "email" (later: "file", "wecom")
	From        string    `json:"from"`    // address
	FromName    string    `json:"from_name,omitempty"`
	To          string    `json:"to,omitempty"`
	Subject     string    `json:"subject"`
	Body        string    `json:"body"`
	Date        time.Time `json:"date"`
	Attachments []string  `json:"attachments,omitempty"`
}

// Domain returns the lowercased sender domain.
func (m Message) Domain() string {
	at := strings.LastIndex(m.From, "@")
	if at < 0 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(m.From[at+1:]))
}

// Text is subject + body, for extraction.
func (m Message) Text() string { return m.Subject + "\n" + m.Body }

// EventType is what a message tells us about an order.
type EventType string

const (
	Ack          EventType = "ack"           // supplier confirmed the PO
	DateChange   EventType = "date_change"   // supplier gives a new delivery date
	PartialShip  EventType = "partial_ship"  // supplier ships part of the quantity
	PriceChange  EventType = "price_change"  // supplier changes the unit price
	QualityIssue EventType = "quality_issue" // defects, returns, rework
	Other        EventType = "other"         // not about a specific order
)

// Label is the ground truth for one message (synthetic corpus only).
type Label struct {
	MessageID string      `json:"message_id"`
	Events    []GoldEvent `json:"events"`
}

// GoldEvent is one expected event.
type GoldEvent struct {
	Type       EventType `json:"type"`
	PONumber   string    `json:"po_number,omitempty"`
	LineNo     int       `json:"line_no,omitempty"` // 0 = whole PO
	PartNumber string    `json:"part_number,omitempty"`
	Date       string    `json:"date,omitempty"` // YYYY-MM-DD
	Qty        float64   `json:"qty,omitempty"`
	Price      float64   `json:"price,omitempty"`
	// Expected resolution in the ERP.
	POID   string `json:"po_id,omitempty"`
	LineID string `json:"line_id,omitempty"`
}

// ExpectedDiscrepancy is a discrepancy the hub must (or must not) raise.
type ExpectedDiscrepancy struct {
	AsOf   time.Time `json:"as_of"`
	POID   string    `json:"po_id"`
	LineID string    `json:"line_id"`
	Kind   string    `json:"kind"`
	Expect bool      `json:"expect"` // false = must NOT be raised (ERP already updated)
}

// Corpus is the synthetic email corpus with labels.
type Corpus struct {
	Note          string                `json:"note"`
	Messages      []Message             `json:"messages"`
	Labels        []Label               `json:"labels"`
	Discrepancies []ExpectedDiscrepancy `json:"discrepancies"`
}

// LoadCorpus reads a corpus JSON file.
func LoadCorpus(path string) (Corpus, error) {
	var c Corpus
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	err = json.Unmarshal(b, &c)
	return c, err
}
