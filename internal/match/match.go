// Package match resolves extracted events to ERP purchase-order lines:
// supplier (sender domain, then name in signature), PO (normalized number),
// line (line number, part number, or the only line). Anything below the
// review threshold goes to a human review queue instead of the timeline.
//
// Stdlib only.
package match

import (
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/zigeH/arribo-procurement/internal/connectors/erp"
	"github.com/zigeH/arribo-procurement/internal/domain"
	"github.com/zigeH/arribo-procurement/internal/extract"
	"github.com/zigeH/arribo-procurement/internal/inbox"
)

// ReviewThreshold: matches below this confidence need a human.
const ReviewThreshold = 0.5

// Match is the resolution of one event.
type Match struct {
	Event      extract.Event `json:"event"`
	SupplierID string        `json:"supplier_id,omitempty"`
	POID       string        `json:"po_id,omitempty"`
	PONumber   string        `json:"po_number,omitempty"`
	LineID     string        `json:"line_id,omitempty"` // empty for PO-level events (ack)
	Confidence float64       `json:"confidence"`
	Reasons    []string      `json:"reasons"`
	Candidates []string      `json:"candidates,omitempty"` // line ids, when ambiguous
}

// NeedsReview reports whether a human should confirm the match.
func (m Match) NeedsReview() bool {
	if m.Event.Type == inbox.Other {
		return false
	}
	return m.POID == "" || m.Confidence < ReviewThreshold || (m.LineID == "" && m.Event.Type != inbox.Ack)
}

// Matcher holds the ERP view used for resolution.
type Matcher struct {
	pos      []domain.PurchaseOrder
	byNumber map[string]int
	byDomain map[string]string
	aliases  []alias
}

type alias struct {
	name, supplierID string
}

var genericDomains = map[string]bool{"qq.com": true, "163.com": true, "126.com": true, "gmail.com": true, "outlook.com": true, "hotmail.com": true, "foxmail.com": true, "sina.com": true, "yahoo.com": true}

var nonDigit = regexp.MustCompile(`\D`)

// New builds a matcher from ERP suppliers and POs.
func New(sups []erp.Supplier, pos []domain.PurchaseOrder) *Matcher {
	m := &Matcher{pos: pos, byNumber: map[string]int{}, byDomain: map[string]string{}}
	for i, p := range pos {
		m.byNumber[nonDigit.ReplaceAllString(p.Number, "")] = i
	}
	for _, s := range sups {
		for _, e := range s.Emails {
			if at := strings.LastIndex(e, "@"); at >= 0 {
				d := strings.ToLower(e[at+1:])
				if !genericDomains[d] {
					m.byDomain[d] = s.ID
				}
			}
		}
		n := strings.TrimSpace(strings.TrimSuffix(s.Name, " (alias)"))
		n = strings.TrimPrefix(n, "Supplier ")
		if len([]rune(n)) >= 3 {
			m.aliases = append(m.aliases, alias{strings.ToLower(n), s.ID})
		}
	}
	// Longest names first so "CNC shop Nanjing B" wins over shorter overlaps.
	sort.Slice(m.aliases, func(i, j int) bool { return len(m.aliases[i].name) > len(m.aliases[j].name) })
	return m
}

func (m *Matcher) supplier(msg inbox.Message) (string, float64, string) {
	if id, ok := m.byDomain[msg.Domain()]; ok {
		return id, 0.95, "supplier by sender domain " + msg.Domain()
	}
	text := strings.ToLower(msg.FromName + "\n" + msg.Body)
	for _, a := range m.aliases {
		if idx := strings.Index(text, a.name); idx >= 0 {
			// Require a word boundary after the alias ("Small supplier CN-07" must not match "CN-071").
			end := idx + len(a.name)
			if end == len(text) || !isWordByte(text[end]) {
				return a.supplierID, 0.75, "supplier by name in signature: " + a.name
			}
		}
	}
	return "", 0, "supplier not recognized from sender " + msg.From
}

func isWordByte(b byte) bool {
	return b == '_' || b == '-' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z')
}

func openAt(l domain.POLine, t time.Time) bool {
	var got float64
	for _, r := range l.Receipts {
		if !r.Date.After(t) {
			got += r.QtyReceived
		}
	}
	return got < l.Qty
}

// Resolve matches one event from one message.
func (m *Matcher) Resolve(msg inbox.Message, e extract.Event) Match {
	out := Match{Event: e}
	if e.Type == inbox.Other {
		out.Reasons = []string{"not about a specific order"}
		return out
	}
	sid, sconf, why := m.supplier(msg)
	out.SupplierID = sid
	out.Reasons = append(out.Reasons, why)
	conf := e.Confidence

	var po *domain.PurchaseOrder
	if e.PONumber != "" {
		if i, ok := m.byNumber[e.PONumber]; ok {
			po = &m.pos[i]
			out.Reasons = append(out.Reasons, "PO number "+e.PONumber+" found in ERP")
			if sid != "" && po.SupplierID != sid {
				// Typo'd PO number or wrong thread: never auto-apply.
				out.POID, out.PONumber = po.ID, po.Number
				out.Reasons = append(out.Reasons, "PO belongs to supplier "+po.SupplierID+", not sender's "+sid)
				out.Confidence = 0.3
				return out
			}
		} else {
			out.Reasons = append(out.Reasons, "PO number "+e.PONumber+" not in ERP")
			out.Confidence = 0.2
			return out
		}
	} else {
		// No PO number: find open lines of this supplier for the part.
		if sid == "" || e.PartNumber == "" {
			out.Reasons = append(out.Reasons, "no PO number and no supplier+part to search by")
			out.Confidence = 0.2
			return out
		}
		type cand struct {
			pi, li int
		}
		var cands []cand
		for pi, p := range m.pos {
			if p.SupplierID != sid || p.OrderDate.After(msg.Date) {
				continue
			}
			for li, l := range p.Lines {
				if extract.NormalizePart(l.PartNumber) == e.PartNumber && openAt(l, msg.Date) {
					cands = append(cands, cand{pi, li})
				}
			}
		}
		if len(cands) == 0 {
			out.Reasons = append(out.Reasons, "no open line for part "+e.PartNumber+" with this supplier")
			out.Confidence = 0.2
			return out
		}
		sort.Slice(cands, func(i, j int) bool {
			a, b := m.pos[cands[i].pi].Lines[cands[i].li], m.pos[cands[j].pi].Lines[cands[j].li]
			return a.PromisedDate.Before(b.PromisedDate)
		})
		c := cands[0]
		po = &m.pos[c.pi]
		out.POID, out.PONumber, out.SupplierID = po.ID, po.Number, po.SupplierID
		out.LineID = po.Lines[c.li].ID
		if len(cands) == 1 {
			conf = min(conf, 0.7)
			out.Reasons = append(out.Reasons, "only open line for part "+e.PartNumber)
		} else {
			conf = 0.45
			out.Reasons = append(out.Reasons, "several open lines for part "+e.PartNumber+"; picked earliest due")
			for _, cc := range cands {
				out.Candidates = append(out.Candidates, m.pos[cc.pi].Lines[cc.li].ID)
			}
		}
		out.Confidence = min(conf, sconf)
		return out
	}

	out.POID, out.PONumber = po.ID, po.Number
	if out.SupplierID == "" {
		// Unknown sender quoting a valid PO (forwarded thread, colleague,
		// another supplier): a human confirms before it touches the timeline.
		out.SupplierID = po.SupplierID
		sconf = 0.45
		out.Reasons = append(out.Reasons, "sender not linked to the PO's supplier")
	}
	if e.Type == inbox.Ack {
		out.Confidence = min(conf, sconf)
		return out
	}
	var line *domain.POLine
	if e.LineNo > 0 {
		for i := range po.Lines {
			if po.Lines[i].LineNo == e.LineNo {
				line = &po.Lines[i]
			}
		}
		if line != nil && e.PartNumber != "" && extract.NormalizePart(line.PartNumber) != e.PartNumber {
			conf -= 0.3
			out.Reasons = append(out.Reasons, "line number and part number disagree")
		}
	}
	if line == nil && e.PartNumber != "" {
		for i := range po.Lines {
			if extract.NormalizePart(po.Lines[i].PartNumber) == e.PartNumber {
				line = &po.Lines[i]
				out.Reasons = append(out.Reasons, "line by part number "+e.PartNumber)
			}
		}
	}
	if line == nil && len(po.Lines) == 1 {
		line = &po.Lines[0]
		out.Reasons = append(out.Reasons, "PO has a single line")
	}
	if line == nil {
		out.Reasons = append(out.Reasons, "could not tell which line")
		for _, l := range po.Lines {
			out.Candidates = append(out.Candidates, l.ID)
		}
		out.Confidence = min(conf, 0.4)
		return out
	}
	out.LineID = line.ID
	out.Confidence = min(conf, sconf)
	return out
}
