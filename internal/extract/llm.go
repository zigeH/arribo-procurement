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
	"unicode"

	"github.com/zigeH/arribo-procurement/internal/inbox"
)

// LLM extracts events with the Anthropic Messages API.
//
// Env: ANTHROPIC_API_KEY and ARRIBO_LLM_APPROVED (both required),
// ARRIBO_EXTRACT_MODEL (optional). Never used in CI; evals run the Rules
// baseline. Only send data the customer has approved in writing for LLM
// processing (CLAUDE.md hard rule 4): ARRIBO_LLM_APPROVED records that
// approval (e.g. "mloptic 2026-11-15 email from <name>").
//
// Model output is not trusted blindly (hard rule 3): self-reported confidence
// is capped at LLMMaxConfidence, and an event whose quote is not found in the
// email is pushed below the review threshold.
type LLM struct {
	APIKey  string
	Model   string
	BaseURL string // default https://api.anthropic.com
	HTTP    *http.Client
}

// LLMMaxConfidence caps the model's self-reported confidence.
const LLMMaxConfidence = 0.8

// unverifiedConfidence is used when the quote is missing from the email; it is
// below match.ReviewThreshold so the event goes to the review queue.
const unverifiedConfidence = 0.3

// ErrLLMNotApproved means ARRIBO_LLM_APPROVED is not set.
var ErrLLMNotApproved = fmt.Errorf("LLM extraction needs ARRIBO_LLM_APPROVED (customer's written approval to send email to the LLM provider)")

// NewLLMFromEnv returns nil, err if no API key is configured or the customer's
// approval is not recorded in ARRIBO_LLM_APPROVED.
func NewLLMFromEnv() (*LLM, error) {
	if os.Getenv("ARRIBO_LLM_APPROVED") == "" {
		return nil, ErrLLMNotApproved
	}
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("LLM extraction needs ANTHROPIC_API_KEY")
	}
	model := os.Getenv("ARRIBO_EXTRACT_MODEL")
	if model == "" {
		model = "claude-sonnet-5-5"
	}
	return &LLM{APIKey: key, Model: model}, nil
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
	return parseLLM(m, text)
}

// squash lowercases and drops whitespace, so a quote matches the email even if
// the model re-wrapped lines or changed spacing.
func squash(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if !unicode.IsSpace(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func parseLLM(m inbox.Message, text string) ([]Event, error) {
	msgID := m.ID
	haystack := squash(m.Subject + "\n" + m.Body)
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
		e.Confidence = min(e.Confidence, LLMMaxConfidence)
		if e.Type != inbox.Other {
			if q := squash(e.Quote); q == "" || !strings.Contains(haystack, q) {
				e.Confidence = min(e.Confidence, unverifiedConfidence)
			}
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
