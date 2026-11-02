package erp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/zigeH/arribo-procurement/internal/domain"
)

// ServiceLayer is a read-only client for the SAP Business One Service Layer.
// It logs in (POST /Login), keeps the B1SESSION cookie, re-logs in once on
// 401, and follows OData next links for paging.
//
// BaseURL example: https://b1.example.com:50000/b1s/v1
type ServiceLayer struct {
	BaseURL   string
	CompanyDB string
	UserName  string
	Password  string
	PageSize  int // Prefer: odata.maxpagesize; default 200
	HTTP      *http.Client

	mu       sync.Mutex
	loggedIn bool
}

func (s *ServiceLayer) client() *http.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.HTTP == nil {
		jar, _ := cookiejar.New(nil)
		s.HTTP = &http.Client{Jar: jar, Timeout: 60 * time.Second}
	} else if s.HTTP.Jar == nil {
		jar, _ := cookiejar.New(nil)
		s.HTTP.Jar = jar
	}
	return s.HTTP
}

func (s *ServiceLayer) login(ctx context.Context) error {
	body, _ := json.Marshal(map[string]string{"CompanyDB": s.CompanyDB, "UserName": s.UserName, "Password": s.Password})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(s.BaseURL, "/")+"/Login", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := s.client().Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return fmt.Errorf("service layer login: %s: %s", res.Status, b)
	}
	s.mu.Lock()
	s.loggedIn = true
	s.mu.Unlock()
	return nil
}

var errUnauthorized = errors.New("unauthorized")

func (s *ServiceLayer) get(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	ps := s.PageSize
	if ps <= 0 {
		ps = 200
	}
	req.Header.Set("Prefer", fmt.Sprintf("odata.maxpagesize=%d", ps))
	res, err := s.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized {
		return nil, errUnauthorized
	}
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("service layer GET %s: %s: %.300s", rawURL, res.Status, b)
	}
	return b, nil
}

// getAll fetches every page of a collection.
func getAll[T any](ctx context.Context, s *ServiceLayer, path string, q url.Values) ([]T, error) {
	s.mu.Lock()
	needLogin := !s.loggedIn
	s.mu.Unlock()
	if needLogin {
		if err := s.login(ctx); err != nil {
			return nil, err
		}
	}
	base := strings.TrimRight(s.BaseURL, "/")
	next := base + "/" + path
	if len(q) > 0 {
		next += "?" + q.Encode()
	}
	var out []T
	relogged := false
	for next != "" {
		b, err := s.get(ctx, next)
		if errors.Is(err, errUnauthorized) && !relogged {
			relogged = true
			if err := s.login(ctx); err != nil {
				return nil, err
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		var p odataPage[T]
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, err
		}
		out = append(out, p.Value...)
		n := p.next()
		switch {
		case n == "":
			next = ""
		case strings.HasPrefix(n, "http"):
			next = n
		case strings.HasPrefix(n, "/"):
			u, _ := url.Parse(base)
			next = u.Scheme + "://" + u.Host + n
		default:
			next = base + "/" + n
		}
	}
	return out, nil
}

const docSelect = "DocEntry,DocNum,DocDate,DocDueDate,CardCode,CardName,DocCurrency,NumAtCard,DocumentLines"

func (s *ServiceLayer) Suppliers(ctx context.Context) ([]Supplier, error) {
	q := url.Values{"$select": {"CardCode,CardName,CardType,EmailAddress,Country"}, "$filter": {"CardType eq 'cSupplier'"}}
	bps, err := getAll[B1BusinessPartner](ctx, s, "BusinessPartners", q)
	if err != nil {
		return nil, err
	}
	return MapB1Suppliers(bps), nil
}

func (s *ServiceLayer) PurchaseOrders(ctx context.Context, since time.Time) ([]domain.PurchaseOrder, error) {
	f := fmt.Sprintf("DocDate ge '%s'", since.Format("2006-01-02"))
	q := url.Values{"$select": {docSelect}, "$filter": {f}, "$orderby": {"DocEntry"}}
	pos, err := getAll[B1Document](ctx, s, "PurchaseOrders", q)
	if err != nil {
		return nil, err
	}
	grs, err := getAll[B1Document](ctx, s, "PurchaseDeliveryNotes", q)
	if err != nil {
		return nil, err
	}
	return MapB1(pos, grs)
}
