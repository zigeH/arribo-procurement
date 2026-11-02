package extract

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zigeH/arribo-procurement/internal/inbox"
)

// Rules is the deterministic baseline extractor (Chinese + English).
type Rules struct{}

func (Rules) Name() string { return "rules-v0" }

var (
	rePO   = regexp.MustCompile(`(?i)(?:MLO-?PO-?|PO\s*#?\s*|订单号?\s*|采购订单\s*|order\s+#?)(\d{6})(?:\D|$)`)
	reLine = regexp.MustCompile(`(?i)第\s*(\d{1,3})\s*[项行]|\bline\s*(\d{1,3})\b|\bitem\s*(\d{1,3})\b`)
	rePart = regexp.MustCompile(`(?i)\b([A-Z]{3})-?(\d{5}|STD-?\d{3})(?:-?\s*(?:rev\s*)?([A-C]))?\b`)
	reQty  = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*(?:件|pcs|pieces|个|片|units)`)

	reDateYMDzh = regexp.MustCompile(`(\d{4})年(\d{1,2})月(\d{1,2})日`)
	reDateMDzh  = regexp.MustCompile(`(\d{1,2})月(\d{1,2})日`)
	reDateISO   = regexp.MustCompile(`(\d{4})[/-](\d{1,2})[/-](\d{1,2})`)
	reDateDMY   = regexp.MustCompile(`\b(\d{1,2})\.(\d{1,2})\.(\d{4})\b`)
	reDateEN    = regexp.MustCompile(`(?i)\b(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)[a-z]*\.?\s+(\d{1,2})(?:,?\s+(\d{4}))?`)

	kw = []struct {
		t  inbox.EventType
		re *regexp.Regexp
	}{
		{inbox.PriceChange, regexp.MustCompile(`(?i)调价|涨价|价格调整|单价.{0,20}(调整|上调)|price adjustment|price increase|unit price .{0,40}changes`)},
		{inbox.DateChange, regexp.MustCompile(`(?i)改期|延期|推迟|延到|延至|延后|顺延|提前到|交期调整|交期由|预计\s*\d|reschedul|delay|pushed? .{0,40} to|new delivery date|has to move|schedule update`)},
		{inbox.PartialShip, regexp.MustCompile(`(?i)分批|先发|先送|部分发货|已.{0,10}发出|partial|remaining .{0,10}pcs`)},
		{inbox.QualityIssue, regexp.MustCompile(`(?i)不良|退货|返工|reject|defect|RMA\b|replacement`)},
		{inbox.Ack, regexp.MustCompile(`(?i)已确认|确认按订单|已收到.{0,10}确认|都没问题|acknowledge|is confirmed|order confirmation`)},
	}
	newDateMarker = regexp.MustCompile(`(?i)(调整为|改为|推迟到|延到|延至|延后到|提前到|预计|new delivery date:?|move to|to|until|expect)\s*$`)
	newPriceRe    = regexp.MustCompile(`(?i)(?:调整为|改为|\bto)\s*(?:([A-Z]{3})\s*)?(\d+(?:\.\d+)?)\s*(元)?`)
	arrowPriceRe  = regexp.MustCompile(`(?m)^(.*?)(\d+(?:\.\d+)?)\s*(?:→|->|=>)\s*(\d+(?:\.\d+)?)`)
	bareNumRe     = regexp.MustCompile(`(?:^|[^\d])(\d{6})(?:[^\d]|$)`)
	shipDateWords = regexp.MustCompile(`(?i)出货|发货|一起发|交货|交$|ship|deliver`)
)

var months = map[string]time.Month{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6, "jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}

type dateHit struct {
	start int
	t     time.Time
}

// findDates returns all dates in text with their positions. Dates without a
// year take the message's year, rolled forward if that lands >60 days before
// the message (e.g. a January date mentioned in November).
func findDates(text string, ref time.Time) []dateHit {
	var out []dateHit
	used := make([]bool, len(text)+1)
	mk := func(y, m, d int) (time.Time, bool) {
		if m < 1 || m > 12 || d < 1 || d > 31 {
			return time.Time{}, false
		}
		return time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC), true
	}
	add := func(loc []int, t time.Time) {
		for i := loc[0]; i < loc[1]; i++ {
			if used[i] {
				return
			}
		}
		for i := loc[0]; i < loc[1]; i++ {
			used[i] = true
		}
		out = append(out, dateHit{loc[0], t})
	}
	atoi := func(s string) int { n, _ := strconv.Atoi(s); return n }
	noYear := func(m, d int) (time.Time, bool) {
		t, ok := mk(ref.Year(), m, d)
		if ok && t.Before(ref.AddDate(0, 0, -60)) {
			t = t.AddDate(1, 0, 0)
		}
		return t, ok
	}
	for _, loc := range reDateYMDzh.FindAllStringSubmatchIndex(text, -1) {
		if t, ok := mk(atoi(text[loc[2]:loc[3]]), atoi(text[loc[4]:loc[5]]), atoi(text[loc[6]:loc[7]])); ok {
			add(loc, t)
		}
	}
	for _, loc := range reDateISO.FindAllStringSubmatchIndex(text, -1) {
		if t, ok := mk(atoi(text[loc[2]:loc[3]]), atoi(text[loc[4]:loc[5]]), atoi(text[loc[6]:loc[7]])); ok {
			add(loc, t)
		}
	}
	for _, loc := range reDateDMY.FindAllStringSubmatchIndex(text, -1) {
		if t, ok := mk(atoi(text[loc[6]:loc[7]]), atoi(text[loc[4]:loc[5]]), atoi(text[loc[2]:loc[3]])); ok {
			add(loc, t)
		}
	}
	for _, loc := range reDateMDzh.FindAllStringSubmatchIndex(text, -1) {
		if t, ok := noYear(atoi(text[loc[2]:loc[3]]), atoi(text[loc[4]:loc[5]])); ok {
			add(loc, t)
		}
	}
	for _, loc := range reDateEN.FindAllStringSubmatchIndex(text, -1) {
		m := months[strings.ToLower(text[loc[2]:loc[2]+3])]
		d := atoi(text[loc[4]:loc[5]])
		var t time.Time
		var ok bool
		if loc[6] >= 0 {
			t, ok = mk(atoi(text[loc[6]:loc[7]]), int(m), d)
		} else {
			t, ok = noYear(int(m), d)
		}
		if ok {
			add(loc, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].start < out[j].start })
	return out
}

// NormalizePart canonicalizes part numbers: "lns81807a", "LNS-81807 rev A" -> "LNS-81807-A".
func NormalizePart(s string) string {
	m := rePart.FindStringSubmatch(s)
	if m == nil {
		return strings.ToUpper(strings.TrimSpace(s))
	}
	core := strings.ToUpper(m[2])
	if strings.HasPrefix(core, "STD") {
		core = "STD-" + strings.TrimLeft(strings.TrimPrefix(core, "STD"), "-")
	}
	out := strings.ToUpper(m[1]) + "-" + core
	if m[3] != "" {
		out += "-" + strings.ToUpper(m[3])
	}
	return out
}

func sentenceAround(text string, pos int) string {
	start := strings.LastIndexAny(text[:pos], "\n。.!！?？") + 1
	endRel := strings.IndexAny(text[pos:], "\n。!！?？")
	end := len(text)
	if endRel >= 0 {
		end = pos + endRel + 1
	}
	q := strings.TrimSpace(text[start:end])
	if len(q) > 240 {
		q = q[:240]
	}
	return q
}

type partHit struct {
	pos  int
	norm string
}

func findParts(text string) []partHit {
	var out []partHit
	for _, loc := range rePart.FindAllStringIndex(text, -1) {
		out = append(out, partHit{loc[0], NormalizePart(text[loc[0]:loc[1]])})
	}
	return out
}

// pickPart chooses the part number the event is about: the only one, or the
// one closest before the trigger keyword, or the first after it.
func pickPart(parts []partHit, kwPos int) (string, bool) {
	distinct := map[string]bool{}
	for _, p := range parts {
		distinct[p.norm] = true
	}
	if len(distinct) == 0 {
		return "", false
	}
	if len(distinct) == 1 {
		return parts[0].norm, false
	}
	best := ""
	for _, p := range parts {
		if p.pos <= kwPos {
			best = p.norm
		}
	}
	if best == "" {
		for _, p := range parts {
			if p.pos > kwPos {
				return p.norm, true
			}
		}
	}
	return best, true
}

func (Rules) Extract(_ context.Context, m inbox.Message) ([]Event, error) {
	text := m.Subject + "\n" + m.Body
	ev := Event{MessageID: m.ID, Type: inbox.Other, Confidence: 0.5}

	kwPos := -1
	for _, k := range kw {
		if loc := k.re.FindStringIndex(text); loc != nil {
			ev.Type, kwPos = k.t, loc[0]
			break
		}
	}
	barePO := false
	if mm := rePO.FindStringSubmatch(text); mm != nil {
		ev.PONumber = mm[1]
	} else {
		head := m.Subject + " " + firstN(m.Body, 40)
		if mm := bareNumRe.FindStringSubmatch(head); mm != nil {
			ev.PONumber, barePO = mm[1], true
		}
	}
	if mm := reLine.FindStringSubmatch(text); mm != nil {
		for _, g := range mm[1:] {
			if g != "" {
				ev.LineNo, _ = strconv.Atoi(g)
			}
		}
	}
	parts := findParts(text)
	if ev.Type == inbox.Other {
		return []Event{ev}, nil
	}
	if ev.PONumber == "" && len(parts) == 0 {
		// Order language without any order reference: not actionable.
		ev.Type = inbox.Other
		return []Event{ev}, nil
	}
	part, multiPart := pickPart(parts, kwPos)
	ev.PartNumber = part
	ev.Quote = sentenceAround(text, kwPos)
	dates := findDates(text, m.Date)

	// A quality email that also gives a new ship date is really a date change
	// ("返工后12月3日出货").
	if ev.Type == inbox.QualityIssue {
		for _, d := range dates {
			after := text[d.start:min(len(text), d.start+24)]
			if d.t.After(m.Date) && shipDateWords.MatchString(after) {
				ev.Type = inbox.DateChange
				break
			}
		}
	}

	switch ev.Type {
	case inbox.DateChange:
		var pick *dateHit
		for i := range dates {
			if newDateMarker.MatchString(text[:dates[i].start]) {
				pick = &dates[i]
			}
		}
		if pick == nil {
			for i := range dates {
				if dates[i].start > kwPos && dates[i].t.After(m.Date) {
					pick = &dates[i]
					break
				}
			}
		}
		if pick == nil {
			for i := range dates {
				if dates[i].t.After(m.Date) {
					pick = &dates[i]
					break
				}
			}
		}
		if pick != nil {
			t := pick.t
			ev.Date = &t
			ev.Quote = sentenceAround(text, pick.start)
		}
	case inbox.PartialShip, inbox.QualityIssue:
		var best *float64
		for _, loc := range reQty.FindAllStringSubmatchIndex(text, -1) {
			v, _ := strconv.ParseFloat(text[loc[2]:loc[3]], 64)
			pre := text[max(0, loc[0]-12):loc[0]]
			lp := strings.ToLower(pre)
			if strings.Contains(pre, "共") {
				continue // total quantity, not the shipped/rejected part
			}
			strong := strings.Contains(pre, "先发") || strings.Contains(pre, "先送") || strings.Contains(lp, "shipped") || strings.Contains(pre, "有") || strings.Contains(lp, "confirm")
			if best == nil || strong {
				vv := v
				best = &vv
				if strong {
					break
				}
			}
		}
		ev.Qty = best
	case inbox.PriceChange:
		// Table rows "PART  old → new": one event per row.
		var rows []Event
		for _, mm := range arrowPriceRe.FindAllStringSubmatch(text, -1) {
			ps := findParts(mm[0])
			if len(ps) == 0 {
				continue
			}
			v, _ := strconv.ParseFloat(mm[3], 64)
			r := ev
			r.PartNumber, r.LineNo = ps[0].norm, 0
			r.Price = &v
			r.Quote = strings.TrimSpace(mm[0])
			r.Confidence = 0.8
			if r.PONumber == "" {
				r.Confidence = 0.65
			}
			rows = append(rows, r)
		}
		if len(rows) > 0 {
			return rows, nil
		}
		if mm := newPriceRe.FindStringSubmatch(text); mm != nil {
			v, _ := strconv.ParseFloat(mm[2], 64)
			ev.Price = &v
			switch {
			case mm[1] != "":
				ev.Currency = strings.ToUpper(mm[1])
			case mm[3] != "":
				ev.Currency = "CNY"
			}
		}
	}

	c := 0.55
	if ev.PONumber != "" {
		c += 0.2
		if barePO {
			c -= 0.1
		}
	}
	if ev.LineNo > 0 || ev.PartNumber != "" {
		c += 0.1
	}
	if ev.Date != nil || ev.Qty != nil || ev.Price != nil || ev.Type == inbox.Ack {
		c += 0.1
	}
	if multiPart && ev.LineNo == 0 {
		c -= 0.15
	}
	ev.Confidence = min(c, 0.95)
	return []Event{ev}, nil
}

func firstN(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}
