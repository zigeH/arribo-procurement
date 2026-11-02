package synth

// Synthetic ERP export (SAP Business One Service Layer shape) and supplier
// email corpus with golden labels, derived from the synthetic dataset.
// Everything here is synthetic. Templates and rates are assumptions [A].

import (
	"fmt"
	"math"
	"math/rand"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zigeH/arribo-procurement/internal/connectors/erp"
	"github.com/zigeH/arribo-procurement/internal/domain"
	"github.com/zigeH/arribo-procurement/internal/inbox"
)

// B1Export is what the Service Layer would return for the dataset.
type B1Export struct {
	BusinessPartners      []erp.B1BusinessPartner
	PurchaseOrders        []erp.B1Document
	PurchaseDeliveryNotes []erp.B1Document
}

var poNumRe = regexp.MustCompile(`(\d{6})$`)

func docNum(po domain.PurchaseOrder) int {
	m := poNumRe.FindStringSubmatch(po.Number)
	n, _ := strconv.Atoi(m[1])
	return n
}

func docEntry(po domain.PurchaseOrder) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(po.ID, "po-"))
	return n
}

func shortName(s domain.Supplier) string {
	n := strings.TrimSuffix(s.Name, " (alias)")
	return strings.TrimPrefix(n, "Supplier ")
}

func supplierDomain(s domain.Supplier) string {
	slug := strings.ToLower(strings.Fields(shortName(s))[0])
	tld := map[string]string{"CN": "cn", "DE": "de", "JP": "jp", "US": "com"}[s.Country]
	if tld == "" {
		tld = "com"
	}
	return fmt.Sprintf("%s-%s.example.%s", slug, strings.ToLower(strings.TrimPrefix(s.ID, "SUP-")), tld)
}

type lineRef struct {
	po   domain.PurchaseOrder
	line domain.POLine
}

// GenerateHub derives the ERP export and email corpus. end is the dataset date.
func GenerateHub(seed int64, ds domain.Dataset) (B1Export, inbox.Corpus) {
	rng := rand.New(rand.NewSource(seed + 7))
	end := ds.Generated
	start := end.AddDate(0, 0, -150)
	sups := map[string]domain.Supplier{}
	for _, s := range ds.Suppliers {
		sups[s.ID] = s
	}

	var exp B1Export
	for _, s := range ds.Suppliers {
		d := supplierDomain(s)
		exp.BusinessPartners = append(exp.BusinessPartners, erp.B1BusinessPartner{
			CardCode: s.ID, CardName: s.Name, CardType: "cSupplier", Country: s.Country,
			EmailAddress: "sales@" + d + "; po@" + d,
		})
	}

	// Decide which supplier-side changes the buyer already typed into the ERP.
	erpDateUpdated := map[string]bool{}  // line id
	priceChange := map[string]float64{}  // line id -> new price
	erpPriceUpdated := map[string]bool{} // line id
	var recentOpen []lineRef
	for _, po := range ds.POs {
		for _, l := range po.Lines {
			if l.OriginalPromisedDate != nil {
				erpDateUpdated[l.ID] = rng.Float64() < 0.3
			}
			if !po.OrderDate.Before(start) && po.OrderDate.Before(end.AddDate(0, 0, -10)) {
				if _, done := l.CompletedAt(); !done {
					recentOpen = append(recentOpen, lineRef{po, l})
				}
			}
		}
	}
	rng.Shuffle(len(recentOpen), func(i, j int) { recentOpen[i], recentOpen[j] = recentOpen[j], recentOpen[i] })
	for i, r := range recentOpen {
		if i >= 9 {
			break
		}
		f := 1.04 + 0.08*rng.Float64()
		priceChange[r.line.ID] = math.Round(r.line.UnitPrice*f*100) / 100
		erpPriceUpdated[r.line.ID] = rng.Float64() < 0.2
	}

	for _, po := range ds.POs {
		d := erp.B1Document{
			DocEntry: docEntry(po), DocNum: docNum(po), DocDate: po.OrderDate.Format("2006-01-02"),
			CardCode: po.SupplierID, CardName: sups[po.SupplierID].Name, DocCurrency: po.Currency, PaymentTerms: po.PaymentTerms,
		}
		for _, l := range po.Lines {
			ship := l.CommitDate()
			if erpDateUpdated[l.ID] {
				ship = l.PromisedDate
			}
			price := l.UnitPrice
			if p, ok := priceChange[l.ID]; ok && erpPriceUpdated[l.ID] {
				price = p
			}
			d.DocumentLines = append(d.DocumentLines, erp.B1DocLine{
				LineNum: l.LineNo - 1, ItemCode: l.PartNumber, ItemDescription: l.Description,
				Quantity: l.Qty, Price: price, Currency: po.Currency, ShipDate: ship.Format("2006-01-02"), ItemGroup: l.Category,
			})
			for ri, r := range l.Receipts {
				exp.PurchaseDeliveryNotes = append(exp.PurchaseDeliveryNotes, erp.B1Document{
					DocEntry: 100000 + docEntry(po)*10 + l.LineNo*3 + ri, DocNum: 900000 + docEntry(po)*10 + l.LineNo*3 + ri,
					DocDate: r.Date.Format("2006-01-02"), CardCode: po.SupplierID, DocCurrency: po.Currency,
					DocumentLines: []erp.B1DocLine{{LineNum: 0, ItemCode: l.PartNumber, Quantity: r.QtyReceived,
						BaseType: 22, BaseEntry: docEntry(po), BaseLine: l.LineNo - 1}},
				})
			}
		}
		exp.PurchaseOrders = append(exp.PurchaseOrders, d)
	}
	sort.Slice(exp.PurchaseDeliveryNotes, func(i, j int) bool {
		return exp.PurchaseDeliveryNotes[i].DocEntry < exp.PurchaseDeliveryNotes[j].DocEntry
	})

	g := &mailGen{rng: rng, sups: sups}
	inWin := func(t time.Time) bool { return !t.Before(start) && !t.After(end) }
	ackBudget := 15
	for _, po := range ds.POs {
		if po.OrderDate.Before(start) {
			continue
		}
		if ackBudget > 0 && rng.Float64() < 0.2 {
			t := po.OrderDate.Add(time.Duration(6+rng.Intn(40)) * time.Hour)
			if inWin(t) {
				ackBudget--
				g.ack(po, t)
			}
		}
		for _, l := range po.Lines {
			if l.OriginalPromisedDate != nil {
				t := l.OriginalPromisedDate.AddDate(0, 0, -(2 + rng.Intn(6)))
				if l.PromiseChangedAt != nil {
					t = *l.PromiseChangedAt // the email is how the buyer learned of the move
				}
				if t.Before(po.OrderDate) {
					t = po.OrderDate.AddDate(0, 0, 1)
				}
				t = t.Add(time.Duration(9+rng.Intn(8)) * time.Hour)
				if inWin(t) {
					id := g.dateChange(po, l, t)
					day := truncDay(t).AddDate(0, 0, 1).Add(23 * time.Hour)
					g.corpus.Discrepancies = append(g.corpus.Discrepancies, inbox.ExpectedDiscrepancy{
						AsOf: day, POID: erp.POID(docEntry(po)), LineID: b1LineID(po, l), Kind: "date_mismatch", Expect: !erpDateUpdated[l.ID]})
					_ = id
				}
			}
			// Late lines whose date was never formally moved: the supplier
			// still emails a new date, and nobody updates the ERP.
			if done, ok := l.CompletedAt(); ok && l.OriginalPromisedDate == nil && done.After(l.PromisedDate.AddDate(0, 0, 2)) && rng.Float64() < 0.5 {
				t := l.PromisedDate.AddDate(0, 0, -(1 + rng.Intn(4))).Add(time.Duration(9+rng.Intn(8)) * time.Hour)
				if inWin(t) && t.After(po.OrderDate) {
					ll := l
					orig := l.PromisedDate
					ll.OriginalPromisedDate, ll.PromisedDate = &orig, truncDay(done)
					g.dateChange(po, ll, t)
					day := truncDay(t).AddDate(0, 0, 1).Add(23 * time.Hour)
					g.corpus.Discrepancies = append(g.corpus.Discrepancies, inbox.ExpectedDiscrepancy{
						AsOf: day, POID: erp.POID(docEntry(po)), LineID: b1LineID(po, l), Kind: "date_mismatch", Expect: true})
				}
			}
			if len(l.Receipts) == 2 && rng.Float64() < 0.3 {
				t := l.Receipts[0].Date.AddDate(0, 0, -2).Add(10 * time.Hour)
				if inWin(t) && t.After(po.OrderDate) {
					g.partial(po, l, l.Receipts[0].QtyReceived, t)
				}
			}
			if rej := l.RejectedQty(); rej > 0 && rng.Float64() < 0.12 {
				last := l.Receipts[len(l.Receipts)-1].Date
				t := last.AddDate(0, 0, 3+rng.Intn(4)).Add(14 * time.Hour)
				if inWin(t) {
					g.quality(po, l, rej, t)
				}
			}
			if p, ok := priceChange[l.ID]; ok {
				t := po.OrderDate.AddDate(0, 0, 3+rng.Intn(7)).Add(11 * time.Hour)
				if t.After(end) {
					t = end.Add(-time.Hour)
				}
				g.price(po, l, p, t)
				day := truncDay(t).AddDate(0, 0, 1).Add(23 * time.Hour)
				g.corpus.Discrepancies = append(g.corpus.Discrepancies, inbox.ExpectedDiscrepancy{
					AsOf: day, POID: erp.POID(docEntry(po)), LineID: b1LineID(po, l), Kind: "price_mismatch", Expect: !erpPriceUpdated[l.ID]})
			}
		}
	}
	g.noise(start, end)

	// Stable order and ids by date.
	type pair struct {
		m inbox.Message
		l inbox.Label
	}
	ps := make([]pair, len(g.corpus.Messages))
	for i := range ps {
		ps[i] = pair{g.corpus.Messages[i], g.corpus.Labels[i]}
	}
	sort.SliceStable(ps, func(i, j int) bool { return ps[i].m.Date.Before(ps[j].m.Date) })
	g.corpus.Messages, g.corpus.Labels = nil, nil
	for i, p := range ps {
		id := fmt.Sprintf("msg-%04d", i+1)
		p.m.ID, p.l.MessageID = id, id
		g.corpus.Messages = append(g.corpus.Messages, p.m)
		g.corpus.Labels = append(g.corpus.Labels, p.l)
	}
	sort.SliceStable(g.corpus.Discrepancies, func(i, j int) bool { return g.corpus.Discrepancies[i].AsOf.Before(g.corpus.Discrepancies[j].AsOf) })
	g.corpus.Note = "SYNTHETIC supplier emails generated from synthetic data. Not real correspondence. Labels are ground truth for evals."
	return exp, g.corpus
}

func b1LineID(po domain.PurchaseOrder, l domain.POLine) string {
	return fmt.Sprintf("%s-%d", erp.POID(docEntry(po)), l.LineNo-1)
}

type mailGen struct {
	rng    *rand.Rand
	sups   map[string]domain.Supplier
	corpus inbox.Corpus
}

func (g *mailGen) pick(xs ...string) string { return xs[g.rng.Intn(len(xs))] }

func (g *mailGen) zh(s domain.Supplier) bool { return s.Country == "CN" }

// sender returns from address, display name and signature.
func (g *mailGen) sender(s domain.Supplier) (string, string, string) {
	short := shortName(s)
	if g.zh(s) {
		person := g.pick("王", "李", "张", "刘", "陈") + g.pick("经理", "工", "")
		if g.rng.Float64() < 0.2 { // personal mailbox: only the signature identifies the supplier
			return fmt.Sprintf("%d@qq.com", 100000000+g.rng.Intn(800000000)), person, fmt.Sprintf("%s\n%s 销售部", person, short)
		}
		return g.pick("sales", "po", "service") + "@" + supplierDomain(s), person, fmt.Sprintf("%s\n%s", person, short)
	}
	person := g.pick("Anna", "Ken", "Lukas", "Maria", "Tom")
	if g.rng.Float64() < 0.15 {
		return strings.ToLower(person) + fmt.Sprintf(".%d@gmail.com", g.rng.Intn(1000)), person, fmt.Sprintf("%s\n%s", person, short)
	}
	return strings.ToLower(person) + "@" + supplierDomain(s), person, fmt.Sprintf("Best regards,\n%s\n%s", person, short)
}

func (g *mailGen) poRef(po domain.PurchaseOrder, zh bool) string {
	n := strconv.Itoa(docNum(po))
	if zh {
		return g.pick("PO "+n, "PO"+n, "订单号"+n, "MLO-PO-"+n, "采购订单 "+n)
	}
	return g.pick("PO "+n, "PO#"+n, "MLO-PO-"+n, "order "+n)
}

var partRe = regexp.MustCompile(`^([A-Z]{3})-(\d{5}|STD-\d{3})(?:-([A-C]))?$`)

func (g *mailGen) partRef(pn string) string {
	m := partRe.FindStringSubmatch(pn)
	if m == nil {
		return pn
	}
	switch g.rng.Intn(4) {
	case 0:
		return pn
	case 1:
		return strings.ReplaceAll(pn, "-", "")
	case 2:
		return strings.ToLower(pn)
	default:
		if m[3] != "" {
			return m[1] + "-" + m[2] + " rev " + m[3]
		}
		return pn
	}
}

func (g *mailGen) date(t time.Time, zh bool) string {
	if zh {
		switch g.rng.Intn(3) {
		case 0:
			return fmt.Sprintf("%d年%d月%d日", t.Year(), t.Month(), t.Day())
		case 1:
			return fmt.Sprintf("%d月%d日", t.Month(), t.Day())
		default:
			return t.Format("2006/01/02")
		}
	}
	switch g.rng.Intn(3) {
	case 0:
		return t.Format("Jan 2, 2006")
	case 1:
		return t.Format("2006-01-02")
	default:
		return t.Format("02.01.2006")
	}
}

func (g *mailGen) add(s domain.Supplier, t time.Time, subject, body, sig string, from, name string, ev ...inbox.GoldEvent) {
	g.corpus.Messages = append(g.corpus.Messages, inbox.Message{
		Channel: "email", From: from, FromName: name, To: "purchasing@mloptic.example", Subject: subject,
		Body: body + "\n\n" + sig, Date: t,
	})
	g.corpus.Labels = append(g.corpus.Labels, inbox.Label{Events: ev})
}

func gold(t inbox.EventType, po domain.PurchaseOrder, l *domain.POLine) inbox.GoldEvent {
	e := inbox.GoldEvent{Type: t, PONumber: strconv.Itoa(docNum(po)), POID: erp.POID(docEntry(po))}
	if l != nil {
		e.LineNo, e.PartNumber, e.LineID = l.LineNo, l.PartNumber, b1LineID(po, *l)
	}
	return e
}

func (g *mailGen) ack(po domain.PurchaseOrder, t time.Time) {
	s := g.sups[po.SupplierID]
	from, name, sig := g.sender(s)
	ref := g.poRef(po, g.zh(s))
	ev := gold(inbox.Ack, po, nil)
	if g.zh(s) {
		switch g.rng.Intn(2) {
		case 0:
			g.add(s, t, "Re: "+ref, "您好，\n\n贵司"+ref+"已收到，我司确认按订单交期安排生产。", sig, from, name, ev)
		default:
			g.add(s, t, "回复："+ref+"确认", "收到"+ref+"，已确认，谢谢！", sig, from, name, ev)
		}
		return
	}
	switch g.rng.Intn(2) {
	case 0:
		g.add(s, t, "Re: Purchase Order "+ref, "Hi,\n\nWe acknowledge receipt of "+ref+" and confirm the order.", sig, from, name, ev)
	default:
		g.add(s, t, "Order confirmation "+ref, "Dear buyer, "+ref+" is confirmed. Delivery as per PO.", sig, from, name, ev)
	}
}

func (g *mailGen) dateChange(po domain.PurchaseOrder, l domain.POLine, t time.Time) string {
	s := g.sups[po.SupplierID]
	from, name, sig := g.sender(s)
	zh := g.zh(s)
	ref, part := g.poRef(po, zh), g.partRef(l.PartNumber)
	newD := l.PromisedDate
	ev := gold(inbox.DateChange, po, &l)
	ev.Date = newD.Format("2006-01-02")
	omitLine := g.rng.Float64() < 0.25
	omitPO := !omitLine && g.rng.Float64() < 0.15
	if omitLine {
		ev.LineNo = 0
	}
	if omitPO {
		ev.PONumber = ""
	}
	if zh {
		reason := g.pick("镀膜返工", "原材料到货延迟", "设备检修", "排产冲突")
		switch {
		case omitPO:
			g.add(s, t, "交期延后", part+" 这批要延到"+g.date(newD, true)+"发货，抱歉。", sig, from, name, ev)
		case omitLine:
			g.add(s, t, "延期通知", "您好，"+ref+"的"+part+"要推迟到"+g.date(newD, true)+"交货，抱歉。", sig, from, name, ev)
		default:
			g.add(s, t, "关于"+ref+"交期调整", fmt.Sprintf("您好，因%s，贵司%s第%d项（%s）交期由%s调整为%s，敬请谅解。", reason, ref, l.LineNo, part, g.date(l.CommitDate(), true), g.date(newD, true)), sig, from, name, ev)
		}
		return ""
	}
	reason := g.pick("coating rework", "a delay in raw glass", "equipment maintenance")
	switch {
	case omitPO:
		g.add(s, t, "Schedule update", "Hi, the "+part+" batch has to move to "+g.date(newD, false)+". Sorry for the inconvenience.", sig, from, name, ev)
	case omitLine:
		g.add(s, t, ref+" reschedule", "Hi, we need to push "+part+" on "+ref+" to "+g.date(newD, false)+".", sig, from, name, ev)
	default:
		g.add(s, t, "Delivery date update - "+ref, fmt.Sprintf("Hello,\n\nDue to %s, line %d (%s) of %s will be delayed. New delivery date: %s.", reason, l.LineNo, part, ref, g.date(newD, false)), sig, from, name, ev)
	}
	return ""
}

func (g *mailGen) partial(po domain.PurchaseOrder, l domain.POLine, qty float64, t time.Time) {
	s := g.sups[po.SupplierID]
	from, name, sig := g.sender(s)
	zh := g.zh(s)
	ref, part := g.poRef(po, zh), g.partRef(l.PartNumber)
	ev := gold(inbox.PartialShip, po, &l)
	ev.Qty = qty
	rest := l.Qty - qty
	if zh {
		g.add(s, t, ref+" 分批发货通知", fmt.Sprintf("您好，%s第%d项 %s 共%.0f件，今天先发%.0f件，剩余%.0f件预计下周发出。", ref, l.LineNo, part, l.Qty, qty, rest), sig, from, name, ev)
		return
	}
	g.add(s, t, "Partial shipment - "+ref, fmt.Sprintf("Hi, we shipped %.0f pcs of %s (line %d) today under %s; the remaining %.0f pcs will follow next week.", qty, part, l.LineNo, ref, rest), sig, from, name, ev)
}

func (g *mailGen) quality(po domain.PurchaseOrder, l domain.POLine, qty float64, t time.Time) {
	s := g.sups[po.SupplierID]
	from, name, sig := g.sender(s)
	zh := g.zh(s)
	ref, part := g.poRef(po, zh), g.partRef(l.PartNumber)
	ev := gold(inbox.QualityIssue, po, &l)
	ev.Qty = qty
	if zh {
		g.add(s, t, "关于"+ref+"不良品处理", fmt.Sprintf("收到贵司检验反馈，%s第%d项 %s 有%.0f件不良，我司安排补货，抱歉。", ref, l.LineNo, part, qty), sig, from, name, ev)
		return
	}
	g.add(s, t, "RE: Rejected parts "+ref, fmt.Sprintf("Thanks for the inspection report. We confirm %.0f pcs of %s on %s line %d were rejected; replacements will ship with the next batch.", qty, part, ref, l.LineNo), sig, from, name, ev)
}

func (g *mailGen) price(po domain.PurchaseOrder, l domain.POLine, p float64, t time.Time) {
	s := g.sups[po.SupplierID]
	from, name, sig := g.sender(s)
	zh := g.zh(s)
	ref, part := g.poRef(po, zh), g.partRef(l.PartNumber)
	ev := gold(inbox.PriceChange, po, &l)
	ev.Price = p
	if zh {
		g.add(s, t, "调价通知", fmt.Sprintf("您好，由于原材料涨价，贵司%s第%d项 %s 单价由 %.2f 元调整为 %.2f 元，请更新订单。", ref, l.LineNo, part, l.UnitPrice, p), sig, from, name, ev)
		return
	}
	g.add(s, t, "Price adjustment "+ref, fmt.Sprintf("Hello, due to glass cost increases the unit price for %s (%s, line %d) changes from %s %.2f to %s %.2f.", part, ref, l.LineNo, po.Currency, l.UnitPrice, po.Currency, p), sig, from, name, ev)
}

func (g *mailGen) noise(start, end time.Time) {
	var ids []string
	for id := range g.sups {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	tmpl := []struct{ subj, body string }{
		{"春节放假通知", "各位客户：我司2027年春节放假时间为2月5日至2月16日，节前订单请提前安排。"},
		{"10月对账单", "您好，附件为10月对账单，请核对，如无异议请盖章回传。"},
		{"New coating capabilities", "We are pleased to announce our new IBS coating line. Contact us for samples."},
		{"Meeting next week?", "Hi, would you have time for a short call next Tuesday to discuss next year's forecast?"},
		{"国庆节快乐", "祝贵司国庆节快乐！"},
		{"Invoice reminder", "Kindly note invoice INV-2026-1187 is due on Nov 15. Thank you."},
		{"展会邀请", "诚邀贵司参观我司在光博会的展位，展期9月10日至12日。"},
		{"Updated company certificate", "Please find attached our renewed ISO 9001 certificate."},
		{"Re: 样品寄送", "样品已于今日顺丰寄出，单号SF1234567890，请查收。"},
		{"Holiday schedule", "Our office will be closed Dec 24 - Jan 1."},
	}
	span := end.Sub(start)
	for i, tm := range tmpl {
		var s domain.Supplier
		for {
			s = g.sups[ids[g.rng.Intn(len(ids))]]
			if g.zh(s) == hasCJK(tm.subj+tm.body) {
				break
			}
		}
		from, name, sig := g.sender(s)
		t := start.Add(time.Duration(float64(span) * (float64(i) + 0.5) / float64(len(tmpl))))
		g.add(s, t, tm.subj, tm.body, sig, from, name, inbox.GoldEvent{Type: inbox.Other})
	}
}

func hasCJK(s string) bool {
	for _, r := range s {
		if r >= 0x4e00 && r <= 0x9fff {
			return true
		}
	}
	return false
}
