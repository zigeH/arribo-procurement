package extract

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/zigeH/arribo-procurement/internal/inbox"
)

// LLM extracts events with the Anthropic Messages API.
//
// Env: ANTHROPIC_API_KEY (required), ARRIBO_EXTRACT_MODEL (optional).
// Never used in CI; evals run the Rules baseline. Only send data the
// customer has approved for LLM processing (see docs/data-spec.md).
type LLM struct {
	APIKey  string
	Model   string
	BaseURL string // default https://api.anthropic.com
	HTTP    *http.Client
}

// NewLLMFromEnv returns nil if no API key is configured.
func NewLLMFromEnv() *LLM {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return nil
	}
	model := os.Getenv("ARRIBO_EXTRACT_MODEL")
	if model == "" {
		model = "claude-sonnet-5-5"
	}
	return &LLM{APIKey: key, Model: model}
}

func (l *LLM) Name() string { return "llm:" + l.Model }

const llmSystem = `You extract purchasing events from supplier emails for a buyer's procurement team.
Return ONLY a JSON array (no prose). One object per event; most emails have exactly one.
Fields: "type" one of ack|date_change|partial_ship|price_change|quality_issue|other;
"po_number" digits only (e.g. "MLO-PO-250123" -> "250123"), "" if absent;
"line_no" integer line/item number as written by the supplier (0 if absent);
"part_number" as written, "" if absent;
"date" YYYY-MM-DD: for date_change the NEW delivery date (resolve missing years relative to the email date);
"qty": for partial_ship the quantity shipped now, for quality_issue the quantity rejected;
"price": for price_change the NEW unit price; "currency" ISO code if known;
"confidence" 0..1; "quote": the exact sentence supporting the event.
Use type "other" (with empty fields) for mail not about a specific order (holidays, invoices, marketing).
Never invent values that are not in the email.`

type llmEvent struct {
	Type       string   `json:"type"`
	PONumber   string   `json:"po_number"`
	LineNo     int      `json:"line_no"`
	PartNumber string   `json:"part_number"`
	Date       string   `json:"date"`
	Qty        *float64 `json:"qty"`
	Price      *float64 `json:"price"`
	Currency   string   `json:"currency"`
	Confidence float64  `json:"confidence"`
	Quote      string   `json:"quote"`
}

func (l *LLM) Extract(ctx context.Context, m inbox.Message) ([]Event, error) {
	base := l.BaseURL
	if base == "" {
		base = "https://api.anthropic.com"
	}
	user := fmt.Sprintf("Email date: %s\nFrom: %s <%s>\nSubject: %s\n\n%s", m.Date.Format("2006-01-02"), m.FromName, m.From, m.Subject, m.Body)
	reqBody, _ := json.Marshal(map[string]any{
		"model":      l.Model,
		"max_tokens": 1024,
		"system":     llmSystem,
		"messages":   []map[string]string{{"role": "user", "content": user}},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/v1/messages", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", l.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	hc := l.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 90 * time.Second}
	}
	res, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic: %s: %.300s", res.Status, b)
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	var text string
	for _, c := range out.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}
	return parseLLM(m.ID, text)
}

func parseLLM(msgID, text string) ([]Event, error) {
	i, j := strings.Index(text, "["), strings.LastIndex(text, "]")
	if i < 0 || j < i {
		return nil, fmt.Errorf("llm output has no JSON array: %.200s", text)
	}
	var raw []llmEvent
	if err := json.Unmarshal([]byte(text[i:j+1]), &raw); err != nil {
		return nil, err
	}
	var evs []Event
	for _, r := range raw {
		e := Event{MessageID: msgID, Type: inbox.EventType(r.Type), PONumber: digits(r.PONumber), LineNo: r.LineNo,
			Qty: r.Qty, Price: r.Price, Currency: strings.ToUpper(r.Currency), Confidence: r.Confidence, Quote: r.Quote}
		if r.PartNumber != "" {
			e.PartNumber = NormalizePart(r.PartNumber)
		}
		if r.Date != "" {
			if t, err := time.Parse("2006-01-02", r.Date); err == nil {
				e.Date = &t
			}
		}
		switch e.Type {
		case inbox.Ack, inbox.DateChange, inbox.PartialShip, inbox.PriceChange, inbox.QualityIssue, inbox.Other:
		default:
			e.Type = inbox.Other
		}
		evs = append(evs, e)
	}
	return evs, nil
}

func digits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
