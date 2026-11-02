package evals

// PO data hub evals: extraction, matching and discrepancy detection on the
// synthetic SAP-B1-shaped ERP export + supplier email corpus (golden labels).
// Thresholds are what the rules baseline must keep meeting; the LLM extractor
// is evaluated separately (not in CI). Corpus templates are synthetic, so
// these numbers overstate real-world accuracy for the rules baseline.

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/zigeH/arribo-procurement/internal/connectors/erp"
	"github.com/zigeH/arribo-procurement/internal/extract"
	"github.com/zigeH/arribo-procurement/internal/inbox"
	"github.com/zigeH/arribo-procurement/internal/timeline"
)

type hubFixture struct {
	corpus inbox.Corpus
	hub    *timeline.Hub
	gold   map[string]inbox.GoldEvent // message id -> event
}

func loadHub(t *testing.T) hubFixture {
	t.Helper()
	c, err := inbox.LoadCorpus("data/emails.json")
	if err != nil {
		t.Fatal(err)
	}
	h, err := timeline.Load(context.Background(), erp.FixtureReader{Dir: "data/b1"}, c.Messages, extract.Rules{})
	if err != nil {
		t.Fatal(err)
	}
	g := map[string]inbox.GoldEvent{}
	for _, l := range c.Labels {
		if len(l.Events) != 1 {
			t.Fatalf("%s: corpus currently has one event per message", l.MessageID)
		}
		g[l.MessageID] = l.Events[0]
	}
	return hubFixture{c, h, g}
}

func TestHubExtraction(t *testing.T) {
	f := loadHub(t)
	types := []inbox.EventType{inbox.Ack, inbox.DateChange, inbox.PartialShip, inbox.PriceChange, inbox.QualityIssue, inbox.Other}
	tp, fp, fn := map[inbox.EventType]int{}, map[inbox.EventType]int{}, map[inbox.EventType]int{}
	fieldOK, fieldN := 0, 0
	var misses []string
	for _, m := range f.corpus.Messages {
		evs, _ := extract.Rules{}.Extract(context.Background(), m)
		got, want := evs[0], f.gold[m.ID]
		if got.Type == want.Type {
			tp[want.Type]++
		} else {
			fp[got.Type]++
			fn[want.Type]++
			misses = append(misses, fmt.Sprintf("%s: want %s got %s | %s", m.ID, want.Type, got.Type, m.Subject))
			continue
		}
		switch want.Type {
		case inbox.DateChange:
			fieldN++
			if got.Date != nil && got.Date.Format("2006-01-02") == want.Date {
				fieldOK++
			} else {
				misses = append(misses, fmt.Sprintf("%s: date want %s got %v", m.ID, want.Date, got.Date))
			}
		case inbox.PartialShip, inbox.QualityIssue:
			fieldN++
			if got.Qty != nil && *got.Qty == want.Qty {
				fieldOK++
			} else {
				misses = append(misses, fmt.Sprintf("%s: qty want %v got %v", m.ID, want.Qty, got.Qty))
			}
		case inbox.PriceChange:
			fieldN++
			if got.Price != nil && *got.Price == want.Price {
				fieldOK++
			} else {
				misses = append(misses, fmt.Sprintf("%s: price want %v got %v", m.ID, want.Price, got.Price))
			}
		}
	}
	var rows []string
	for _, ty := range types {
		p := ratio(tp[ty], tp[ty]+fp[ty])
		r := ratio(tp[ty], tp[ty]+fn[ty])
		rows = append(rows, fmt.Sprintf("  %-14s precision %.2f recall %.2f (n=%d)", ty, p, r, tp[ty]+fn[ty]))
		if r < 0.9 || p < 0.9 {
			t.Errorf("%s below 0.90 precision/recall", ty)
		}
	}
	fa := ratio(fieldOK, fieldN)
	t.Logf("extraction (%d messages):\n%s\n  field accuracy (date/qty/price) %.2f (%d/%d)", len(f.corpus.Messages), strings.Join(rows, "\n"), fa, fieldOK, fieldN)
	if fa < 0.9 {
		t.Errorf("field accuracy %.2f < 0.90", fa)
	}
	for _, m := range misses {
		t.Log("  miss: " + m)
	}
}

func TestHubMatching(t *testing.T) {
	f := loadHub(t)
	resolved := map[string]string{} // message -> line id (or po id for ack)
	for _, it := range f.hub.Items {
		k := it.Match.LineID
		if it.Match.Event.Type == inbox.Ack {
			k = it.Match.POID
		}
		resolved[it.MessageID] = k
	}
	inReview := map[string]bool{}
	for _, it := range f.hub.Review {
		inReview[it.MessageID] = true
	}
	correct, wrong, review, total := 0, 0, 0, 0
	var notes []string
	for id, g := range f.gold {
		if g.Type == inbox.Other {
			continue
		}
		total++
		want := g.LineID
		if g.Type == inbox.Ack {
			want = g.POID
		}
		switch got, ok := resolved[id]; {
		case ok && got == want:
			correct++
		case ok:
			wrong++
			notes = append(notes, fmt.Sprintf("%s WRONG: want %s got %s", id, want, got))
		case inReview[id]:
			review++
		default:
			wrong++
			notes = append(notes, fmt.Sprintf("%s dropped (want %s)", id, want))
		}
	}
	sort.Strings(notes)
	t.Logf("matching (%d order events): auto-correct %.2f, sent to review %.2f, wrong/dropped %.2f",
		total, ratio(correct, total), ratio(review, total), ratio(wrong, total))
	for _, n := range notes {
		t.Log("  " + n)
	}
	if ratio(correct, total) < 0.85 {
		t.Errorf("auto-correct match rate %.2f < 0.85", ratio(correct, total))
	}
	if ratio(wrong, total) > 0.03 {
		t.Errorf("wrong/dropped %.2f > 0.03: confidently wrong matches are worse than review", ratio(wrong, total))
	}
}

func TestHubDiscrepancies(t *testing.T) {
	f := loadHub(t)
	hit, miss, falseAlarm, negatives := 0, 0, 0, 0
	for _, e := range f.corpus.Discrepancies {
		found := false
		for _, d := range f.hub.Discrepancies(e.AsOf) {
			if d.LineID == e.LineID && d.Kind == e.Kind {
				found = true
			}
		}
		switch {
		case e.Expect && found:
			hit++
		case e.Expect:
			miss++
			t.Logf("  missed %s %s as of %s", e.Kind, e.LineID, e.AsOf.Format("2006-01-02"))
		case found:
			falseAlarm++
			negatives++
			t.Logf("  false alarm %s %s (ERP already updated)", e.Kind, e.LineID)
		default:
			negatives++
		}
	}
	t.Logf("discrepancies: recall %.2f (%d/%d), false alarms on already-updated ERP %d/%d",
		ratio(hit, hit+miss), hit, hit+miss, falseAlarm, negatives)
	if ratio(hit, hit+miss) < 0.9 || falseAlarm > 0 {
		t.Error("discrepancy detection below bar")
	}
	end := f.corpus.Messages[len(f.corpus.Messages)-1].Date
	byKind := map[string]int{}
	for _, d := range f.hub.Discrepancies(end) {
		byKind[d.Kind]++
	}
	t.Logf("对不上清单 as of %s: %v; review queue %d; ignored %d", end.Format("2006-01-02"), byKind, len(f.hub.ReviewQueue(end)), f.hub.Ignored)
}

func ratio(a, b int) float64 {
	if b == 0 {
		return 1
	}
	return float64(a) / float64(b)
}

// TestHubHeldOutHard runs the hand-written held-out set. It reports how far
// the rules baseline is from real-world phrasing; the bar is only that it
// stays safe: few confidently wrong timeline entries.
func TestHubHeldOutHard(t *testing.T) {
	c, err := inbox.LoadCorpus("data/emails_hard.json")
	if err != nil {
		t.Fatal(err)
	}
	h, err := timeline.Load(context.Background(), erp.FixtureReader{Dir: "data/b1"}, c.Messages, extract.Rules{})
	if err != nil {
		t.Fatal(err)
	}
	type key struct {
		msg, typ, target string
	}
	gold := map[key]inbox.GoldEvent{}
	nGold := 0
	for _, l := range c.Labels {
		for _, e := range l.Events {
			if e.Type == inbox.Other {
				continue
			}
			tgt := e.LineID
			if e.Type == inbox.Ack {
				tgt = e.POID
			}
			gold[key{l.MessageID, string(e.Type), tgt}] = e
			nGold++
		}
	}
	found, wrong := 0, 0
	for _, it := range h.Items {
		tgt := it.Match.LineID
		if it.Match.Event.Type == inbox.Ack {
			tgt = it.Match.POID
		}
		g, ok := gold[key{it.MessageID, string(it.Match.Event.Type), tgt}]
		e := it.Match.Event
		fieldOK := ok && (g.Date == "" || (e.Date != nil && e.Date.Format("2006-01-02") == g.Date)) &&
			(g.Qty == 0 || (e.Qty != nil && *e.Qty == g.Qty)) && (g.Price == 0 || (e.Price != nil && *e.Price == g.Price))
		if fieldOK {
			found++
		} else {
			wrong++
			t.Logf("  WRONG in timeline: %s %s -> %s (%s)", it.MessageID, e.Type, tgt, strings.Join(it.Match.Reasons, "; "))
		}
	}
	t.Logf("held-out hard set (rules baseline, %d messages, %d events): recall %.2f (%d), confidently wrong %d, sent to review %d, ignored %d",
		len(c.Messages), nGold, ratio(found, nGold), found, wrong, len(h.Review), h.Ignored)
	if wrong > 2 {
		t.Errorf("rules baseline puts %d wrong events on timelines; tighten matching before shipping", wrong)
	}
}
