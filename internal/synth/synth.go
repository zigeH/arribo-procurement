// Package synth generates a deterministic synthetic purchase history shaped
// like what MLOptic (茂莱光学, a custom precision-optics maker) *buys*.
//
// Every parameter is tagged [S#] (from a public source listed in
// docs/synthetic-data-sources.md) or [A] (our assumption, to be replaced by
// real data). Nothing here is MLOptic data.
//
// A handful of suppliers have a known "persona"; evals assert the scorer
// recovers it. The rest form a realistic long tail.
package synth

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"time"

	"github.com/zigeH/arribo-procurement/internal/domain"
)

// Category is a kind of purchased material.
type Category struct {
	Key        string
	Group      string // optical | mechanical | electronic  [S2: IPO 问询函 material groups]
	Desc       string
	Custom     bool    // custom (drawing-specific) vs standard [S2: ~87-97% of purchases are custom]
	Currency   string  // default currency for domestic sources
	PriceLo    float64 // unit price range in Currency
	PriceHi    float64
	QtyLo      int
	QtyHi      int
	LeadNewWk  [2]int  // weeks, first order of a part from a supplier
	LeadRepWk  [2]int  // weeks, repeat orders
	SecondSrcP float64 // probability a part has a second qualified source
	Weight     float64 // relative order frequency
}

// Categories of what a precision-optics maker buys.
var Categories = []Category{
	// Custom optical elements outsourced to processing shops (外协透镜/棱镜/平片).
	// Lead: new 4-6 wk / repeat ~2 wk [S4]; industry standard 8-10 wk [S5]. Price [A],
	// below catalog list prices for small qty (e.g. $62-$201 for 12.7-25.4mm lenses [S6]).
	{Key: "lens_element", Group: "optical", Desc: "custom lens element", Custom: true, Currency: "CNY", PriceLo: 60, PriceHi: 600, QtyLo: 5, QtyHi: 150, LeadNewWk: [2]int{6, 10}, LeadRepWk: [2]int{2, 6}, SecondSrcP: 0.35, Weight: 30},
	{Key: "prism", Group: "optical", Desc: "custom prism", Custom: true, Currency: "CNY", PriceLo: 150, PriceHi: 1200, QtyLo: 5, QtyHi: 80, LeadNewWk: [2]int{6, 10}, LeadRepWk: [2]int{3, 6}, SecondSrcP: 0.3, Weight: 12},
	{Key: "coated_plate", Group: "optical", Desc: "custom filter / coated plate", Custom: true, Currency: "CNY", PriceLo: 100, PriceHi: 1200, QtyLo: 5, QtyHi: 150, LeadNewWk: [2]int{5, 9}, LeadRepWk: [2]int{3, 6}, SecondSrcP: 0.3, Weight: 12},
	// Standard optical glass / fused silica blanks (块料), largely imported. [S2][S3] Price [A].
	{Key: "glass_blank", Group: "optical", Desc: "optical glass blank", Custom: false, Currency: "CNY", PriceLo: 150, PriceHi: 1200, QtyLo: 5, QtyHi: 60, LeadNewWk: [2]int{4, 10}, LeadRepWk: [2]int{4, 8}, SecondSrcP: 0.8, Weight: 10},
	// Polarizers bought from a German maker appear among top suppliers [S1]. Price [A].
	{Key: "polarizer", Group: "optical", Desc: "polarizer film / element", Custom: true, Currency: "EUR", PriceLo: 40, PriceHi: 220, QtyLo: 5, QtyHi: 60, LeadNewWk: [2]int{6, 12}, LeadRepWk: [2]int{6, 10}, SecondSrcP: 0.0, Weight: 3},
	// Machined housings / mounts (机械材料 16-20% of purchases [S2]). Price/lead [A].
	{Key: "machined_part", Group: "mechanical", Desc: "machined housing / mount", Custom: true, Currency: "CNY", PriceLo: 60, PriceHi: 1000, QtyLo: 5, QtyHi: 100, LeadNewWk: [2]int{3, 5}, LeadRepWk: [2]int{2, 4}, SecondSrcP: 0.4, Weight: 22},
	// Fasteners: standard mechanical items [S2]. Price/lead [A].
	{Key: "fastener", Group: "mechanical", Desc: "standard fastener", Custom: false, Currency: "CNY", PriceLo: 0.2, PriceHi: 5, QtyLo: 500, QtyHi: 5000, LeadNewWk: [2]int{1, 2}, LeadRepWk: [2]int{1, 2}, SecondSrcP: 0.9, Weight: 5},
	// Motion stages: a nano-positioning supplier is among top suppliers [S1]. Price/lead [A].
	{Key: "motion_stage", Group: "electronic", Desc: "precision motion stage", Custom: true, Currency: "CNY", PriceLo: 3000, PriceHi: 25000, QtyLo: 1, QtyHi: 3, LeadNewWk: [2]int{6, 14}, LeadRepWk: [2]int{6, 12}, SecondSrcP: 0.2, Weight: 2},
	// PCBA: average unit price 52-259 CNY over 2017-2020 [S2]. Lead [A].
	{Key: "pcba", Group: "electronic", Desc: "custom PCBA", Custom: true, Currency: "CNY", PriceLo: 52, PriceHi: 260, QtyLo: 10, QtyHi: 200, LeadNewWk: [2]int{3, 6}, LeadRepWk: [2]int{3, 5}, SecondSrcP: 0.3, Weight: 5},
	// Industrial cameras for optical test systems (custom camera systems were bought for
	// test stations [S2]). Standard catalog item. Price/lead [A].
	{Key: "camera", Group: "electronic", Desc: "industrial camera", Custom: false, Currency: "USD", PriceLo: 300, PriceHi: 3000, QtyLo: 1, QtyHi: 10, LeadNewWk: [2]int{4, 12}, LeadRepWk: [2]int{4, 10}, SecondSrcP: 0.5, Weight: 4},
}

// Persona is the ground-truth behavior of a supplier.
type Persona struct {
	ID          string
	Name        string
	Country     string
	Currency    string
	Key         string   // stable key used by evals ("" for long-tail suppliers)
	Categories  []string // what they supply
	Size        float64  // relative share of orders [S1: top-5 suppliers only 17-22% of spend → long tail]
	PriceFactor float64
	OnTimeRate  float64 // vs first confirmed date [S7: ~95% target, <85% high risk]
	LateDaysMax int
	MoveDateP   float64 // probability a late line gets its promised date moved first [A]
	LeadFactor  float64
	RejectRate  float64 // [A] no public source found
	AckHours    float64
	NoAckRate   float64
	Terms       string // domestic 月结 terms [A]; overseas Net/T-T [A]
	Incoterm    string
	NewOnly     bool // only a couple of recent orders
}

// Personas: suppliers whose behavior the evals check. IDs are stable.
var Personas = []Persona{
	{ID: "SUP-01", Name: "Supplier Alpha (alias)", Country: "CN", Currency: "CNY", Key: "reliable_premium", Categories: []string{"lens_element", "prism"}, Size: 3, PriceFactor: 1.08, OnTimeRate: 0.96, LateDaysMax: 5, MoveDateP: 0.2, LeadFactor: 1.0, RejectRate: 0.004, AckHours: 6, Terms: "月结60天"},
	{ID: "SUP-02", Name: "Supplier Bravo (alias)", Country: "CN", Currency: "CNY", Key: "cheap_late", Categories: []string{"lens_element", "coated_plate"}, Size: 3, PriceFactor: 0.88, OnTimeRate: 0.55, LateDaysMax: 25, MoveDateP: 0.6, LeadFactor: 0.95, RejectRate: 0.015, AckHours: 30, Terms: "月结60天"},
	{ID: "SUP-03", Name: "Supplier Charlie (alias)", Country: "CN", Currency: "CNY", Key: "balanced", Categories: []string{"lens_element", "prism", "coated_plate"}, Size: 3.5, PriceFactor: 1.0, OnTimeRate: 0.93, LateDaysMax: 7, MoveDateP: 0.3, LeadFactor: 1.0, RejectRate: 0.007, AckHours: 14, Terms: "月结90天"},
	{ID: "SUP-04", Name: "Supplier Delta (alias)", Country: "CN", Currency: "CNY", Key: "quality_problem", Categories: []string{"coated_plate", "lens_element"}, Size: 2, PriceFactor: 0.95, OnTimeRate: 0.88, LateDaysMax: 8, MoveDateP: 0.3, LeadFactor: 1.05, RejectRate: 0.065, AckHours: 20, Terms: "月结60天"},
	{ID: "SUP-05", Name: "Supplier Echo (alias)", Country: "CN", Currency: "CNY", Key: "slow_responder", Categories: []string{"prism", "lens_element"}, Size: 1.5, PriceFactor: 1.02, OnTimeRate: 0.80, LateDaysMax: 12, MoveDateP: 0.4, LeadFactor: 1.45, RejectRate: 0.01, AckHours: 130, NoAckRate: 0.15, Terms: "月结60天"},
	{ID: "SUP-06", Name: "Supplier Foxtrot (alias)", Country: "CN", Currency: "CNY", Key: "overpriced", Categories: []string{"lens_element", "prism"}, Size: 1.5, PriceFactor: 1.32, OnTimeRate: 0.94, LateDaysMax: 5, MoveDateP: 0.2, LeadFactor: 1.0, RejectRate: 0.005, AckHours: 10, Terms: "月结60天"},
	{ID: "SUP-07", Name: "Supplier Golf (alias)", Country: "CN", Currency: "CNY", Key: "new_supplier", Categories: []string{"machined_part"}, Size: 1, PriceFactor: 0.97, OnTimeRate: 0.9, LateDaysMax: 5, LeadFactor: 1.0, RejectRate: 0.01, AckHours: 20, Terms: "月结60天", NewOnly: true},
}

// longTail: realistic suppliers without a test persona.
// Overseas share of raw-material purchases rose from ~19% to ~39% [S3] → ~30% overseas here.
var longTail = []Persona{
	{ID: "SUP-08", Name: "Glass maker DE (alias)", Country: "DE", Currency: "EUR", Categories: []string{"glass_blank"}, Size: 4, Terms: "Net 30", Incoterm: "FCA"},
	{ID: "SUP-09", Name: "Glass maker JP (alias)", Country: "JP", Currency: "USD", Categories: []string{"glass_blank"}, Size: 3, Terms: "Net 30", Incoterm: "FCA"},
	{ID: "SUP-10", Name: "Glass distributor CN (alias)", Country: "CN", Currency: "CNY", Categories: []string{"glass_blank"}, Size: 3, Terms: "月结30天"},
	{ID: "SUP-11", Name: "Polarizer maker DE (alias)", Country: "DE", Currency: "EUR", Categories: []string{"polarizer"}, Size: 2, Terms: "Net 30", Incoterm: "DAP"},
	{ID: "SUP-12", Name: "Optics shop Jiangsu A (alias)", Country: "CN", Currency: "CNY", Categories: []string{"lens_element"}, Size: 5, Terms: "月结90天"},
	{ID: "SUP-13", Name: "Optics shop Jiangsu B (alias)", Country: "CN", Currency: "CNY", Categories: []string{"lens_element", "coated_plate"}, Size: 4, Terms: "月结60天"},
	{ID: "SUP-14", Name: "Optics shop Hubei (alias)", Country: "CN", Currency: "CNY", Categories: []string{"prism", "coated_plate"}, Size: 3, Terms: "月结60天"},
	{ID: "SUP-15", Name: "Coating house Shanghai (alias)", Country: "CN", Currency: "CNY", Categories: []string{"coated_plate"}, Size: 3, Terms: "月结60天"},
	{ID: "SUP-16", Name: "CNC shop Nanjing A (alias)", Country: "CN", Currency: "CNY", Categories: []string{"machined_part"}, Size: 6, Terms: "月结60天"},
	{ID: "SUP-17", Name: "CNC shop Nanjing B (alias)", Country: "CN", Currency: "CNY", Categories: []string{"machined_part"}, Size: 5, Terms: "月结90天"},
	{ID: "SUP-18", Name: "CNC shop Suzhou (alias)", Country: "CN", Currency: "CNY", Categories: []string{"machined_part"}, Size: 3, Terms: "月结60天"},
	{ID: "SUP-19", Name: "Fastener distributor A (alias)", Country: "CN", Currency: "CNY", Categories: []string{"fastener"}, Size: 2, Terms: "月结30天"},
	{ID: "SUP-20", Name: "Fastener distributor B (alias)", Country: "CN", Currency: "CNY", Categories: []string{"fastener"}, Size: 1, Terms: "月结30天"},
	{ID: "SUP-21", Name: "Nano-positioning CN (alias)", Country: "CN", Currency: "CNY", Categories: []string{"motion_stage"}, Size: 2, Terms: "30% deposit, balance 月结60天"},
	{ID: "SUP-22", Name: "Motion control US (alias)", Country: "US", Currency: "USD", Categories: []string{"motion_stage"}, Size: 1, Terms: "Net 30", Incoterm: "EXW"},
	{ID: "SUP-23", Name: "PCBA EMS Shenzhen (alias)", Country: "CN", Currency: "CNY", Categories: []string{"pcba"}, Size: 3, Terms: "月结60天"},
	{ID: "SUP-24", Name: "PCBA EMS Nanjing (alias)", Country: "CN", Currency: "CNY", Categories: []string{"pcba"}, Size: 2, Terms: "月结60天"},
	{ID: "SUP-25", Name: "Camera vendor US (alias)", Country: "US", Currency: "USD", Categories: []string{"camera"}, Size: 2, Terms: "Net 30", Incoterm: "EXW"},
	{ID: "SUP-26", Name: "Camera vendor DE (alias)", Country: "DE", Currency: "USD", Categories: []string{"camera"}, Size: 1, Terms: "Net 30", Incoterm: "DAP"},
}

// part is one purchasable item. Custom parts carry a drawing-specific number.
type part struct {
	number  string
	cat     Category
	price   float64 // base price in the category currency (domestic) or the supplier's currency
	sources []string
	desc    string
	ordered map[string]bool // supplier -> has been ordered before (new vs repeat lead)
}

// Generate builds the dataset with order dates in the 18 months before end.
func Generate(seed int64, end time.Time) domain.Dataset {
	rng := rand.New(rand.NewSource(seed))
	sups := allSuppliers(rng)
	byCat := map[string][]Persona{}
	for _, s := range sups {
		for _, c := range s.Categories {
			byCat[c] = append(byCat[c], s)
		}
	}

	// Build parts: each part has a primary source and maybe a second source.
	var parts []*part
	partsBySup := map[string][]*part{}
	for _, c := range Categories {
		cands := byCat[c.Key]
		if len(cands) == 0 {
			continue
		}
		n := int(c.Weight * 2.2)
		for i := 0; i < n; i++ {
			p := &part{cat: c, ordered: map[string]bool{}}
			p.price = c.PriceLo * math.Pow(c.PriceHi/c.PriceLo, rng.Float64()) // log-uniform
			if c.Custom {
				p.number = fmt.Sprintf("%s-%05d-%c", prefix(c.Key), 10000+rng.Intn(89999), 'A'+rune(rng.Intn(3)))
				p.desc = fmt.Sprintf("%s per drawing rev %c", c.Desc, 'A'+rune(rng.Intn(3)))
			} else {
				p.number = fmt.Sprintf("%s-STD-%03d", prefix(c.Key), i+1)
				p.desc = fmt.Sprintf("%s, standard spec %d", c.Desc, i+1)
			}
			primary := weightedPick(rng, cands)
			p.sources = []string{primary.ID}
			if len(cands) > 1 && rng.Float64() < c.SecondSrcP {
				for tries := 0; tries < 5; tries++ {
					s2 := weightedPick(rng, cands)
					if s2.ID != primary.ID {
						p.sources = append(p.sources, s2.ID)
						break
					}
				}
			}
			parts = append(parts, p)
			for _, s := range p.sources {
				partsBySup[s] = append(partsBySup[s], p)
			}
		}
	}
	// Personas whose evals compare against peers need shared parts with "balanced".
	ensureShared(rng, parts, partsBySup, "SUP-03", []string{"SUP-01", "SUP-02", "SUP-04", "SUP-05", "SUP-06"})

	ds := domain.Dataset{
		Customer:  "mloptic-synthetic-v2",
		Generated: end,
		Note:      "Synthetic purchase history shaped like a custom precision-optics maker's buying. Parameters cite public sources or are labeled assumptions (docs/synthetic-data-sources.md). Supplier names are aliases. Not real data.",
	}
	for _, s := range sups {
		ds.Suppliers = append(ds.Suppliers, domain.Supplier{ID: s.ID, Name: s.Name, Country: s.Country})
	}

	start := end.AddDate(0, -18, 0)
	const posPerMonthPerSize = 0.35 // [A] volume of this synthetic slice, not MLOptic's real volume
	seq := 0
	for _, s := range sups {
		var orderDates []time.Time
		if s.NewOnly {
			orderDates = []time.Time{end.AddDate(0, 0, -55), end.AddDate(0, 0, -25)}
		} else {
			rate := s.Size * posPerMonthPerSize
			for m := start; m.Before(end); m = m.AddDate(0, 1, 0) {
				n := int(rate)
				if rng.Float64() < rate-float64(n) {
					n++
				}
				for i := 0; i < n; i++ {
					orderDates = append(orderDates, m.AddDate(0, 0, rng.Intn(28)).Add(time.Duration(8+rng.Intn(9))*time.Hour))
				}
			}
		}
		sort.Slice(orderDates, func(i, j int) bool { return orderDates[i].Before(orderDates[j]) })
		myParts := partsBySup[s.ID]
		if len(myParts) == 0 {
			continue
		}
		for _, od := range orderDates {
			if od.After(end) {
				continue
			}
			seq++
			po := domain.PurchaseOrder{
				ID:           fmt.Sprintf("po-%05d", seq),
				Number:       fmt.Sprintf("MLO-PO-%06d", 250000+seq),
				SupplierID:   s.ID,
				OrderDate:    od,
				SentAt:       od.Add(time.Duration(rng.Intn(4)) * time.Hour),
				Currency:     s.Currency,
				PaymentTerms: s.Terms,
				Incoterm:     s.Incoterm,
			}
			if rng.Float64() >= s.NoAckRate {
				h := math.Max(1, s.AckHours*(0.5+rng.Float64()))
				if s.Country != "CN" {
					h += 12 // time-zone gap [A]
				}
				ack := po.SentAt.Add(time.Duration(h * float64(time.Hour)))
				po.AcknowledgedAt = &ack
			}
			nLines := 1 + rng.Intn(min(4, len(myParts)))
			used := map[*part]bool{}
			for li := 1; li <= nLines; li++ {
				p := myParts[rng.Intn(len(myParts))]
				if used[p] {
					continue
				}
				used[p] = true
				po.Lines = append(po.Lines, makeLine(rng, s, p, po, len(po.Lines)+1, end))
			}
			ds.POs = append(ds.POs, po)
		}
	}
	sort.Slice(ds.POs, func(i, j int) bool { return ds.POs[i].ID < ds.POs[j].ID })
	return ds
}

func makeLine(rng *rand.Rand, s Persona, p *part, po domain.PurchaseOrder, lineNo int, end time.Time) domain.POLine {
	c := p.cat
	qty := float64(c.QtyLo + rng.Intn(c.QtyHi-c.QtyLo+1))
	if c.QtyHi >= 500 {
		qty = math.Round(qty/100) * 100
	}
	price := p.price * fx(c.Currency, s.Currency) * s.PriceFactor * (0.97 + 0.06*rng.Float64())

	wk := c.LeadRepWk
	if !p.ordered[s.ID] {
		wk = c.LeadNewWk
		p.ordered[s.ID] = true
	}
	leadDays := float64(7*wk[0]+rng.Intn(7*(wk[1]-wk[0])+1)) * s.LeadFactor
	commit := truncDay(po.OrderDate).AddDate(0, 0, int(math.Round(leadDays)))

	line := domain.POLine{
		ID:           fmt.Sprintf("%s-%d", po.ID, lineNo),
		LineNo:       lineNo,
		PartNumber:   p.number,
		Category:     c.Key,
		Description:  p.desc,
		Qty:          qty,
		UnitPrice:    math.Round(price*100) / 100,
		PromisedDate: commit,
	}

	// Delivery outcome vs the first confirmed date.
	late := rng.Intn(3) - 1 // -1..+1 day jitter
	isLate := rng.Float64() > s.OnTimeRate
	if isLate {
		late = 3 + rng.Intn(max(1, s.LateDaysMax-2))
		if rng.Float64() < s.MoveDateP { // supplier pushes the date before it's due
			moved := commit.AddDate(0, 0, late-rng.Intn(3))
			line.OriginalPromisedDate = &commit
			line.PromisedDate = moved
		}
	}
	delivered := commit.AddDate(0, 0, late)
	if delivered.After(end) {
		return line // still open
	}
	rej := math.Round(qty * s.RejectRate * (0.5 + rng.Float64()))
	if qty >= 40 && rng.Float64() < 0.3 { // partial shipment
		first := delivered.AddDate(0, 0, -7)
		if first.Before(po.OrderDate) {
			first = po.OrderDate.AddDate(0, 0, 1)
		}
		half := math.Floor(qty / 2)
		line.Receipts = []domain.Receipt{
			{Date: first, QtyReceived: half, QtyRejected: math.Floor(rej / 2)},
			{Date: delivered, QtyReceived: qty - half, QtyRejected: rej - math.Floor(rej/2)},
		}
	} else {
		line.Receipts = []domain.Receipt{{Date: delivered, QtyReceived: qty, QtyRejected: rej}}
	}
	return line
}

// smallShops adds many small suppliers: the real base is fragmented
// (top-5 suppliers only 17-22% of spend [S1]).
func smallShops(rng *rand.Rand) []Persona {
	type kind struct {
		cats            []string
		country, cur    string
		terms, incoterm string
		weight          float64
	}
	kinds := []kind{
		{[]string{"lens_element"}, "CN", "CNY", "月结60天", "", 6},
		{[]string{"prism", "coated_plate"}, "CN", "CNY", "月结60天", "", 4},
		{[]string{"coated_plate"}, "CN", "CNY", "月结90天", "", 3},
		{[]string{"machined_part"}, "CN", "CNY", "月结60天", "", 7},
		{[]string{"pcba"}, "CN", "CNY", "月结60天", "", 2},
		{[]string{"glass_blank"}, "JP", "USD", "Net 30", "FCA", 2},
		{[]string{"glass_blank", "polarizer"}, "DE", "EUR", "Net 30", "DAP", 2},
		{[]string{"camera", "motion_stage"}, "US", "USD", "Net 30", "EXW", 2},
		{[]string{"lens_element", "prism"}, "US", "USD", "Net 30", "FCA", 1},
	}
	var tot float64
	for _, k := range kinds {
		tot += k.weight
	}
	var out []Persona
	for i := 0; i < 50; i++ {
		x := rng.Float64() * tot
		k := kinds[len(kinds)-1]
		for _, kk := range kinds {
			if x -= kk.weight; x <= 0 {
				k = kk
				break
			}
		}
		out = append(out, Persona{
			ID: fmt.Sprintf("SUP-%02d", 27+i), Name: fmt.Sprintf("Small supplier %s-%02d (alias)", k.country, i+1),
			Country: k.country, Currency: k.cur, Categories: k.cats, Size: 0.6 + 0.9*rng.Float64(),
			Terms: k.terms, Incoterm: k.incoterm,
		})
	}
	return out
}

func allSuppliers(rng *rand.Rand) []Persona {
	out := append([]Persona(nil), Personas...)
	for _, p := range append(append([]Persona(nil), longTail...), smallShops(rng)...) {
		// Typical supplier: OTD 85-97% around the ~95% target [S7]; other traits [A].
		p.PriceFactor = 0.95 + 0.1*rng.Float64()
		p.OnTimeRate = 0.85 + 0.12*rng.Float64()
		p.LateDaysMax = 5 + rng.Intn(10)
		p.MoveDateP = 0.2 + 0.3*rng.Float64()
		p.LeadFactor = 0.95 + 0.1*rng.Float64()
		p.RejectRate = 0.003 + 0.012*rng.Float64()
		p.AckHours = 6 + 40*rng.Float64()
		out = append(out, p)
	}
	return out
}

func ensureShared(rng *rand.Rand, parts []*part, bySup map[string][]*part, anchor string, others []string) {
	for _, o := range others {
		shared := 0
		for _, p := range bySup[o] {
			if contains(p.sources, anchor) {
				shared++
			}
		}
		for _, p := range bySup[o] {
			if shared >= 4 {
				break
			}
			if !contains(p.sources, anchor) && p.cat.Group == "optical" && rng.Float64() < 0.8 {
				p.sources = append(p.sources, anchor)
				bySup[anchor] = append(bySup[anchor], p)
				shared++
			}
		}
	}
	_ = parts
}

// fx converts a category's base price into the supplier's currency. Rates are
// rough fixed assumptions [A]; scoring never compares across currencies.
func fx(from, to string) float64 {
	cny := map[string]float64{"CNY": 1, "USD": 7.1, "EUR": 7.7}
	return cny[from] / cny[to]
}

func weightedPick(rng *rand.Rand, ps []Persona) Persona {
	var tot float64
	for _, p := range ps {
		tot += p.Size
	}
	x := rng.Float64() * tot
	for _, p := range ps {
		x -= p.Size
		if x <= 0 {
			return p
		}
	}
	return ps[len(ps)-1]
}

func prefix(cat string) string {
	return map[string]string{"lens_element": "LNS", "prism": "PRS", "coated_plate": "PLT", "glass_blank": "BLK", "polarizer": "POL",
		"machined_part": "MEC", "fastener": "FST", "motion_stage": "STG", "pcba": "PCB", "camera": "CAM"}[cat]
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func truncDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}
