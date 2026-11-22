package dav

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/patrickryan01/halyard/internal/mailtypes"
)

func TestVCardRoundTrip(t *testing.T) {
	c := mailtypes.Contact{Name: "Ana Maria Ruiz", Email: "ana@y.test", Phone: "+1 555 0100", Org: "Acme, Inc."}
	got := ParseVCard(BuildVCard(c, "u1"))
	if got.ID != "u1" || got.Name != c.Name || got.Email != c.Email || got.Phone != c.Phone || got.Org != c.Org {
		t.Fatalf("%+v", got)
	}
	// a card with only N, and folded lines
	g := ParseVCard("BEGIN:VCARD\r\nVERSION:3.0\r\nN:Ruiz;Ana;;;\r\nitem1.EMAIL;type=INTERNET:ana\r\n @y.test\r\nEND:VCARD\r\n")
	if g.Name != "Ana Ruiz" || g.Email != "ana@y.test" {
		t.Fatalf("%+v", g)
	}
}

// a small CalDAV and CardDAV server: enough of the protocol to prove the client's requests and parsing
func fakeServer(t *testing.T) (*httptest.Server, *sync.Map) {
	store := &sync.Map{}
	mux := http.NewServeMux()
	ms := func(w http.ResponseWriter, body string) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(207)
		io.WriteString(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav" xmlns:card="urn:ietf:params:xml:ns:carddav" xmlns:cs="http://calendarserver.org/ns/">`+body+`</d:multistatus>`)
	}
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if u, p, ok := r.BasicAuth(); !ok || u != "ana" || p != "pw" {
				w.WriteHeader(401)
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("/.well-known/caldav", auth(func(w http.ResponseWriter, r *http.Request) {
		ms(w, `<d:response><d:href>/dav/principal/</d:href><d:propstat><d:prop><d:current-user-principal><d:href>/dav/principal/ana/</d:href></d:current-user-principal></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`)
	}))
	mux.HandleFunc("/.well-known/carddav", func(w http.ResponseWriter, r *http.Request) { mux.ServeHTTP(w, withPath(r, "/.well-known/caldav")) })
	mux.HandleFunc("/dav/principal/ana/", auth(func(w http.ResponseWriter, r *http.Request) {
		ms(w, `<d:response><d:href>/dav/principal/ana/</d:href><d:propstat><d:prop><c:calendar-home-set><d:href>/dav/cal/ana/</d:href></c:calendar-home-set><card:addressbook-home-set><d:href>/dav/card/ana/</d:href></card:addressbook-home-set></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`)
	}))
	mux.HandleFunc("/dav/cal/ana/", auth(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "PROPFIND":
			ms(w, `<d:response><d:href>/dav/cal/ana/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`+
				`<d:response><d:href>/dav/cal/ana/work/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/><c:calendar/></d:resourcetype><d:displayname>Work</d:displayname><c:supported-calendar-component-set><c:comp name="VEVENT"/></c:supported-calendar-component-set></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`+
				`<d:response><d:href>/dav/cal/ana/tasks/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/><c:calendar/></d:resourcetype><d:displayname>Tasks</d:displayname><c:supported-calendar-component-set><c:comp name="VTODO"/></c:supported-calendar-component-set></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`)
		}
	}))
	mux.HandleFunc("/dav/cal/ana/work/", auth(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "REPORT":
			var b strings.Builder
			store.Range(func(k, v any) bool {
				if strings.HasPrefix(k.(string), "/dav/cal/") {
					b.WriteString(`<d:response><d:href>` + k.(string) + `</d:href><d:propstat><d:prop><d:getetag>"e1"</d:getetag><c:calendar-data><![CDATA[` + v.(string) + `]]></c:calendar-data></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`)
				}
				return true
			})
			ms(w, b.String())
		case "PUT":
			b, _ := io.ReadAll(r.Body)
			store.Store(r.URL.Path, string(b))
			w.Header().Set("ETag", `"e1"`)
			w.WriteHeader(201)
		case "DELETE":
			store.Delete(r.URL.Path)
			w.WriteHeader(204)
		}
	}))
	mux.HandleFunc("/dav/card/ana/", auth(func(w http.ResponseWriter, r *http.Request) {
		ms(w, `<d:response><d:href>/dav/card/ana/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`+
			`<d:response><d:href>/dav/card/ana/main/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/><card:addressbook/></d:resourcetype><d:displayname>Contacts</d:displayname></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`)
	}))
	mux.HandleFunc("/dav/card/ana/main/", auth(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "REPORT":
			var b strings.Builder
			store.Range(func(k, v any) bool {
				if strings.HasPrefix(k.(string), "/dav/card/") {
					b.WriteString(`<d:response><d:href>` + k.(string) + `</d:href><d:propstat><d:prop><d:getetag>"c1"</d:getetag><card:address-data><![CDATA[` + v.(string) + `]]></card:address-data></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`)
				}
				return true
			})
			ms(w, b.String())
		case "PUT":
			b, _ := io.ReadAll(r.Body)
			store.Store(r.URL.Path, string(b))
			w.WriteHeader(201)
		case "DELETE":
			store.Delete(r.URL.Path)
			w.WriteHeader(204)
		}
	}))
	return httptest.NewServer(mux), store
}

func withPath(r *http.Request, p string) *http.Request {
	r2 := r.Clone(r.Context())
	r2.URL.Path = p
	return r2
}

func TestCalDAVAndCardDAV(t *testing.T) {
	srv, _ := fakeServer(t)
	defer srv.Close()
	ctx := context.Background()
	if _, err := New(srv.URL, "ana", "wrong", "", false).Collections(ctx, "calendar"); err == nil {
		t.Fatal("a wrong password was accepted")
	}
	c := New(srv.URL, "ana", "pw", "", false)
	cals, err := c.Collections(ctx, "calendar")
	if err != nil || len(cals) != 1 || cals[0].Name != "Work" {
		t.Fatalf("calendars %+v %v (a task-only calendar must not be listed)", cals, err)
	}
	ev := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:e1\r\nDTSTART:20261203T140000Z\r\nDTEND:20261203T150000Z\r\nSUMMARY:Plan\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	if _, err := c.Put(ctx, ObjectURL(cals[0].URL, "e1", ".ics"), "text/calendar; charset=utf-8", ev, ""); err != nil {
		t.Fatal(err)
	}
	objs, err := c.Events(ctx, cals[0].URL, time.Now(), time.Now().AddDate(1, 0, 0))
	if err != nil || len(objs) != 1 || !strings.Contains(objs[0].Data, "SUMMARY:Plan") || objs[0].ETag != "e1" {
		t.Fatalf("events %+v %v", objs, err)
	}
	if err := c.Delete(ctx, objs[0].URL, ""); err != nil {
		t.Fatal(err)
	}
	if objs, _ := c.Events(ctx, cals[0].URL, time.Now(), time.Now().AddDate(1, 0, 0)); len(objs) != 0 {
		t.Fatal("the event is still there")
	}
	books, err := c.Collections(ctx, "addressbook")
	if err != nil || len(books) != 1 || books[0].Name != "Contacts" {
		t.Fatalf("books %+v %v", books, err)
	}
	if _, err := c.Put(ctx, ObjectURL(books[0].URL, "u1", ".vcf"), "text/vcard; charset=utf-8", BuildVCard(mailtypes.Contact{Name: "Bo Li", Email: "bo@y.test"}, "u1"), ""); err != nil {
		t.Fatal(err)
	}
	cards, err := c.Contacts(ctx, books[0].URL)
	if err != nil || len(cards) != 1 || ParseVCard(cards[0].Data).Email != "bo@y.test" {
		t.Fatalf("contacts %+v %v", cards, err)
	}
}

func TestGroupVCardRoundTrip(t *testing.T) {
	g := mailtypes.ContactGroup{Name: "Plant team", Members: []string{"a@x.test", "b@y.test"}}
	got, ok := ParseGroupVCard(BuildGroupVCard(g, "g1"))
	if !ok || got.Name != "Plant team" || len(got.Members) != 2 || got.Members[1] != "b@y.test" {
		t.Fatalf("%+v %v", got, ok)
	}
	if _, ok := ParseGroupVCard(BuildVCard(mailtypes.Contact{Name: "Bo", Email: "bo@y.test"}, "u")); ok {
		t.Fatal("a person is not a group")
	}
}
