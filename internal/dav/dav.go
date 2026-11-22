// Package dav speaks CalDAV (RFC 4791) and CardDAV (RFC 6352): it finds a person's calendars and address books on a
// server and reads and writes the events and contacts in them.
package dav

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

// Client talks to one DAV server as one person.
type Client struct {
	Base     string // the server address, or the full address of a principal or home set
	User     string
	Password string
	Token    string // a bearer token, used instead of the password when set
	HTTP     *http.Client
}

// New makes a client. skipVerify trusts a self-signed server certificate.
func New(base, user, password, token string, skipVerify bool) *Client {
	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: skipVerify, MinVersion: tls.VersionTLS12}, Proxy: http.ProxyFromEnvironment}
	// a redirect (the well known address answers with one) keeps the method and the body, which Go's client would drop
	return &Client{Base: base, User: user, Password: password, Token: token, HTTP: &http.Client{Transport: tr, Timeout: 40 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

type multistatus struct {
	Responses []response `xml:"response"`
}

type response struct {
	Href     string     `xml:"href"`
	Propstat []propstat `xml:"propstat"`
	Status   string     `xml:"status"`
}

type propstat struct {
	Status string `xml:"status"`
	Prop   prop   `xml:"prop"`
}

type href struct {
	Href string `xml:"href"`
}

type prop struct {
	DisplayName  string `xml:"displayname"`
	ResourceType struct {
		Inner []xml.Name `xml:",any"`
	} `xml:"resourcetype"`
	CurrentUser     href   `xml:"current-user-principal"`
	CalendarHome    href   `xml:"calendar-home-set"`
	AddressbookHome href   `xml:"addressbook-home-set"`
	CTag            string `xml:"getctag"`
	ETag            string `xml:"getetag"`
	CalendarData    string `xml:"calendar-data"`
	AddressData     string `xml:"address-data"`
	Color           string `xml:"calendar-color"`
	SupportedComp   struct {
		Comps []struct {
			Name string `xml:"name,attr"`
		} `xml:"comp"`
	} `xml:"supported-calendar-component-set"`
}

func (c *Client) abs(ref string) (string, error) {
	b, err := url.Parse(c.Base)
	if err != nil {
		return "", err
	}
	r, err := b.Parse(ref)
	if err != nil {
		return "", err
	}
	return r.String(), nil
}

func (c *Client) do(ctx context.Context, method, ref string, hdr map[string]string, body []byte) (*http.Response, []byte, error) {
	return c.doN(ctx, method, ref, hdr, body, 0)
}

func (c *Client) doN(ctx context.Context, method, ref string, hdr map[string]string, body []byte, redirects int) (*http.Response, []byte, error) {
	u, err := c.abs(ref)
	if err != nil {
		return nil, nil, err
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, nil, err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	} else if c.User != "" {
		req.SetBasicAuth(c.User, c.Password)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if redirects < 5 && (resp.StatusCode == 301 || resp.StatusCode == 302 || resp.StatusCode == 307 || resp.StatusCode == 308) {
		if loc := resp.Header.Get("Location"); loc != "" {
			next, err := resp.Request.URL.Parse(loc)
			if err == nil {
				return c.doN(ctx, method, next.String(), hdr, body, redirects+1)
			}
		}
	}
	return resp, b, err
}

func (c *Client) multi(ctx context.Context, method, ref, depth, body string) (*multistatus, error) {
	resp, b, err := c.do(ctx, method, ref, map[string]string{"Content-Type": "application/xml; charset=utf-8", "Depth": depth}, []byte(body))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, errors.New("the server refused the sign in for calendars and contacts")
	}
	if resp.StatusCode != 207 {
		return nil, fmt.Errorf("the server answered %d to %s", resp.StatusCode, method)
	}
	var ms multistatus
	if err := xml.Unmarshal(b, &ms); err != nil {
		return nil, err
	}
	return &ms, nil
}

func okProp(r response) prop {
	var out prop
	for _, ps := range r.Propstat {
		if strings.Contains(ps.Status, " 200") || ps.Status == "" {
			p := ps.Prop
			if p.DisplayName != "" {
				out.DisplayName = p.DisplayName
			}
			if len(p.ResourceType.Inner) > 0 {
				out.ResourceType = p.ResourceType
			}
			if p.CurrentUser.Href != "" {
				out.CurrentUser = p.CurrentUser
			}
			if p.CalendarHome.Href != "" {
				out.CalendarHome = p.CalendarHome
			}
			if p.AddressbookHome.Href != "" {
				out.AddressbookHome = p.AddressbookHome
			}
			if p.CTag != "" {
				out.CTag = p.CTag
			}
			if p.ETag != "" {
				out.ETag = p.ETag
			}
			if p.CalendarData != "" {
				out.CalendarData = p.CalendarData
			}
			if p.AddressData != "" {
				out.AddressData = p.AddressData
			}
			if p.Color != "" {
				out.Color = p.Color
			}
			if len(p.SupportedComp.Comps) > 0 {
				out.SupportedComp = p.SupportedComp
			}
		}
	}
	return out
}

func isType(p prop, local string) bool {
	for _, n := range p.ResourceType.Inner {
		if n.Local == local {
			return true
		}
	}
	return false
}

// Collection is one calendar or address book.
type Collection struct {
	URL   string `json:"url"`
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
	CTag  string `json:"ctag,omitempty"`
	Kind  string `json:"kind"` // calendar or addressbook
}

const propfindHome = `<?xml version="1.0"?><d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav" xmlns:card="urn:ietf:params:xml:ns:carddav"><d:prop><d:current-user-principal/><c:calendar-home-set/><card:addressbook-home-set/></d:prop></d:propfind>`
const propfindList = `<?xml version="1.0"?><d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav" xmlns:cs="http://calendarserver.org/ns/" xmlns:a="http://apple.com/ns/ical/"><d:prop><d:resourcetype/><d:displayname/><cs:getctag/><a:calendar-color/><c:supported-calendar-component-set/></d:prop></d:propfind>`

// home finds the calendar or address book home for the person: from the well known address, to the current user
// principal, to the home set.
func (c *Client) home(ctx context.Context, kind string) (string, error) {
	starts := []string{c.Base}
	wk := "/.well-known/caldav"
	if kind == "addressbook" {
		wk = "/.well-known/carddav"
	}
	starts = append([]string{wk}, starts...)
	var lastErr error
	for _, s := range starts {
		ms, err := c.multi(ctx, "PROPFIND", s, "0", propfindHome)
		if err != nil {
			lastErr = err
			continue
		}
		if len(ms.Responses) == 0 {
			continue
		}
		p := okProp(ms.Responses[0])
		h := p.CalendarHome.Href
		if kind == "addressbook" {
			h = p.AddressbookHome.Href
		}
		if h != "" {
			return h, nil
		}
		if p.CurrentUser.Href != "" {
			ms2, err := c.multi(ctx, "PROPFIND", p.CurrentUser.Href, "0", propfindHome)
			if err != nil {
				lastErr = err
				continue
			}
			if len(ms2.Responses) > 0 {
				p2 := okProp(ms2.Responses[0])
				h := p2.CalendarHome.Href
				if kind == "addressbook" {
					h = p2.AddressbookHome.Href
				}
				if h != "" {
					return h, nil
				}
			}
		}
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("the server has no %s for this account", map[string]string{"calendar": "calendars", "addressbook": "address books"}[kind])
}

// Collections lists the calendars (kind "calendar") or address books (kind "addressbook") of the person.
func (c *Client) Collections(ctx context.Context, kind string) ([]Collection, error) {
	h, err := c.home(ctx, kind)
	if err != nil {
		return nil, err
	}
	ms, err := c.multi(ctx, "PROPFIND", h, "1", propfindList)
	if err != nil {
		return nil, err
	}
	want := "calendar"
	if kind == "addressbook" {
		want = "addressbook"
	}
	var out []Collection
	for _, r := range ms.Responses {
		p := okProp(r)
		if !isType(p, want) {
			continue
		}
		if kind == "calendar" && len(p.SupportedComp.Comps) > 0 {
			ev := false
			for _, cp := range p.SupportedComp.Comps {
				if strings.EqualFold(cp.Name, "VEVENT") {
					ev = true
				}
			}
			if !ev {
				continue
			}
		}
		name := p.DisplayName
		if name == "" {
			name = path.Base(strings.TrimRight(r.Href, "/"))
		}
		out = append(out, Collection{URL: r.Href, Name: name, Color: p.Color, CTag: p.CTag, Kind: kind})
	}
	return out, nil
}

// Object is one event or contact as stored: its address, its version tag and its text.
type Object struct {
	URL  string `json:"url"`
	ETag string `json:"etag"`
	Data string `json:"data"`
}

func (c *Client) report(ctx context.Context, coll, body string) ([]Object, error) {
	ms, err := c.multi(ctx, "REPORT", coll, "1", body)
	if err != nil {
		return nil, err
	}
	var out []Object
	for _, r := range ms.Responses {
		p := okProp(r)
		d := p.CalendarData
		if d == "" {
			d = p.AddressData
		}
		if d == "" {
			continue
		}
		out = append(out, Object{URL: r.Href, ETag: strings.Trim(p.ETag, `"`), Data: d})
	}
	return out, nil
}

// Events returns the calendar objects of a calendar that overlap the time range.
func (c *Client) Events(ctx context.Context, cal string, from, to time.Time) ([]Object, error) {
	q := `<?xml version="1.0"?><c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:prop><d:getetag/><c:calendar-data/></d:prop>` +
		`<c:filter><c:comp-filter name="VCALENDAR"><c:comp-filter name="VEVENT"><c:time-range start="` + from.UTC().Format("20060102T150405Z") + `" end="` + to.UTC().Format("20060102T150405Z") + `"/></c:comp-filter></c:comp-filter></c:filter></c:calendar-query>`
	return c.report(ctx, cal, q)
}

// Contacts returns every vCard in an address book.
func (c *Client) Contacts(ctx context.Context, book string) ([]Object, error) {
	q := `<?xml version="1.0"?><card:addressbook-query xmlns:d="DAV:" xmlns:card="urn:ietf:params:xml:ns:carddav"><d:prop><d:getetag/><card:address-data/></d:prop></card:addressbook-query>`
	return c.report(ctx, book, q)
}

// Put writes an object. ref is the object's address; to make a new one pass a name inside the collection. etag, when
// given, makes it an update that fails if someone else changed it; "-" writes over whatever is there.
func (c *Client) Put(ctx context.Context, ref, contentType, data, etag string) (string, error) {
	h := map[string]string{"Content-Type": contentType}
	switch etag {
	case "-": // write over whatever is there
	case "":
		h["If-None-Match"] = "*"
	default:
		h["If-Match"] = `"` + strings.Trim(etag, `"`) + `"`
	}
	resp, b, err := c.do(ctx, "PUT", ref, h, []byte(data))
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("the server did not save it (%d): %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return strings.Trim(resp.Header.Get("ETag"), `"`), nil
}

// Delete removes an object.
func (c *Client) Delete(ctx context.Context, ref, etag string) error {
	h := map[string]string{}
	if etag != "" {
		h["If-Match"] = `"` + strings.Trim(etag, `"`) + `"`
	}
	resp, b, err := c.do(ctx, "DELETE", ref, h, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 && resp.StatusCode != 404 {
		return fmt.Errorf("the server did not delete it (%d): %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// ObjectURL is the address for a new object named name inside a collection.
func ObjectURL(coll, name, ext string) string {
	return strings.TrimRight(coll, "/") + "/" + url.PathEscape(name) + ext
}
