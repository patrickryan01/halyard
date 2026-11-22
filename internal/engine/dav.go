package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/patrickryan01/halyard/internal/dav"
	"github.com/patrickryan01/halyard/internal/ical"
	"github.com/patrickryan01/halyard/internal/mailtypes"
)

// Settings keys for an account's calendar and contacts server.
const (
	SDAVURL  = "dav_url"
	SDAVUser = "dav_user"
	sDAVSkip = "insecure_skip_verify"
)

func davSecretID(accountID string) string { return accountID + ".dav" }

// SetDAV points an account at a CalDAV and CardDAV server. An empty password keeps the one saved; an empty URL removes
// the setting.
func (e *Engine) SetDAV(ctx context.Context, accountID, rawURL, user, password string) error {
	a, err := e.St.Account(accountID)
	if err != nil {
		return errors.New("unknown account")
	}
	if a.Settings == nil {
		a.Settings = map[string]string{}
	}
	if strings.TrimSpace(rawURL) == "" {
		delete(a.Settings, SDAVURL)
		delete(a.Settings, SDAVUser)
		e.Sec.Delete(davSecretID(accountID))
		return e.St.PutAccount(a)
	}
	if u, err := url.Parse(rawURL); err != nil || u.Host == "" {
		return errors.New("the server address must look like https://mail.example.com")
	}
	a.Settings[SDAVURL], a.Settings[SDAVUser] = rawURL, user
	if password != "" {
		if err := e.Sec.Put(davSecretID(accountID), []byte(password)); err != nil {
			return err
		}
	}
	c, err := e.davFor(a)
	if err != nil {
		return err
	}
	if _, err := c.Collections(ctx, "calendar"); err != nil {
		if _, err2 := c.Collections(ctx, "addressbook"); err2 != nil {
			return err
		}
	}
	return e.St.PutAccount(a)
}

// davFor builds the client for an account that has a server set.
func (e *Engine) davFor(a mailtypes.Account) (*dav.Client, error) {
	base := a.Settings[SDAVURL]
	if base == "" {
		return nil, errors.New("this account has no calendar and contacts server set: add one in its settings")
	}
	user := a.Settings[SDAVUser]
	if user == "" {
		user = a.Settings["username"]
	}
	if user == "" {
		user = a.Email
	}
	pw, err := e.Sec.Get(davSecretID(a.ID))
	if err != nil || len(pw) == 0 {
		pw, err = e.Sec.Get(a.ID) // the mail password, when the same
		if err != nil {
			return nil, err
		}
	}
	return dav.New(base, user, string(pw), "", a.Settings[sDAVSkip] == "true"), nil
}

func (e *Engine) davClient(accountID string) (*dav.Client, mailtypes.Account, error) {
	a, err := e.St.Account(accountID)
	if err != nil {
		return nil, a, errors.New("unknown account")
	}
	c, err := e.davFor(a)
	return c, a, err
}

// Calendars lists the calendars of an account's server.
func (e *Engine) Calendars(ctx context.Context, accountID string) ([]dav.Collection, error) {
	c, _, err := e.davClient(accountID)
	if err != nil {
		return nil, err
	}
	return c.Collections(ctx, "calendar")
}

// CalEvent is an event with where it lives, so it can be changed or removed.
type CalEvent struct {
	ical.Event
	AccountID string `json:"accountId"`
	Calendar  string `json:"calendar"`
	URL       string `json:"url"`
	ETag      string `json:"etag"`
}

// Events lists the events of every calendar of an account in a time range, soonest first.
func (e *Engine) Events(ctx context.Context, accountID string, from, to time.Time) ([]CalEvent, error) {
	c, _, err := e.davClient(accountID)
	if err != nil {
		return nil, err
	}
	cals, err := c.Collections(ctx, "calendar")
	if err != nil {
		return nil, err
	}
	var out []CalEvent
	for _, cal := range cals {
		objs, err := c.Events(ctx, cal.URL, from, to)
		if err != nil {
			return nil, err
		}
		for _, o := range objs {
			evs, err := ical.Parse(o.Data)
			if err != nil {
				continue
			}
			for _, ev := range evs {
				out = append(out, CalEvent{Event: ev, AccountID: accountID, Calendar: cal.Name, URL: o.URL, ETag: o.ETag})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out, nil
}

func newUID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b) + "@halyard"
}

// SaveEvent creates an event (no URL) in the calendar, or updates the one at URL.
func (e *Engine) SaveEvent(ctx context.Context, accountID, calendarURL string, ev CalEvent) (CalEvent, error) {
	c, _, err := e.davClient(accountID)
	if err != nil {
		return ev, err
	}
	if strings.TrimSpace(ev.Summary) == "" {
		return ev, errors.New("an event needs a title")
	}
	if ev.End.Before(ev.Start) {
		return ev, errors.New("the event ends before it starts")
	}
	if ev.UID == "" {
		ev.UID = newUID()
	}
	if ev.URL != "" {
		ev.Sequence++
	}
	text := ical.Build(ev.Event, "")
	ref, etag := ev.URL, ev.ETag
	if ref == "" {
		if calendarURL == "" {
			cals, err := c.Collections(ctx, "calendar")
			if err != nil || len(cals) == 0 {
				return ev, errors.New("this server has no calendar to put the event in")
			}
			calendarURL = cals[0].URL
		}
		ref = dav.ObjectURL(calendarURL, ev.UID, ".ics")
		etag = ""
	}
	tag, err := c.Put(ctx, ref, "text/calendar; charset=utf-8", text, etag)
	if err != nil {
		return ev, err
	}
	ev.URL, ev.ETag, ev.AccountID = ref, tag, accountID
	return ev, nil
}

func (e *Engine) DeleteEvent(ctx context.Context, accountID, eventURL, etag string) error {
	c, _, err := e.davClient(accountID)
	if err != nil {
		return err
	}
	return c.Delete(ctx, eventURL, etag)
}

// AddInviteToCalendar saves the event in a message's invitation to a calendar. Any account with a calendar server will
// do; calendarAccountID names it (empty means the message's own account).
func (e *Engine) AddInviteToCalendar(ctx context.Context, messageID, calendarAccountID string) (CalEvent, error) {
	m, err := e.St.Message(messageID)
	if err != nil {
		return CalEvent{}, errors.New("no such message")
	}
	inv, err := e.Invite(ctx, messageID)
	if err != nil {
		return CalEvent{}, err
	}
	if calendarAccountID == "" {
		calendarAccountID = m.AccountID
	}
	ev := inv.Events[0]
	ev.Method = ""
	return e.SaveEvent(ctx, calendarAccountID, "", CalEvent{Event: ev})
}

// ---- CardDAV contacts ----

type davContacts struct {
	e         *Engine
	c         *dav.Client
	accountID string
}

func (d davContacts) book(ctx context.Context) (string, error) {
	books, err := d.c.Collections(ctx, "addressbook")
	if err != nil || len(books) == 0 {
		return "", errors.New("this server has no address book")
	}
	return books[0].URL, nil
}

func (d davContacts) CreateContact(ctx context.Context, c mailtypes.Contact) (mailtypes.Contact, error) {
	b, err := d.book(ctx)
	if err != nil {
		return c, err
	}
	uid := newUID()
	ref := dav.ObjectURL(b, uid, ".vcf")
	if _, err := d.c.Put(ctx, ref, "text/vcard; charset=utf-8", dav.BuildVCard(c, uid), ""); err != nil {
		return c, err
	}
	c.ID = "dav:" + ref
	return c, nil
}

func (d davContacts) UpdateContact(ctx context.Context, c mailtypes.Contact) (mailtypes.Contact, error) {
	ref := strings.TrimPrefix(c.ID, "dav:")
	uid := ref[strings.LastIndex(ref, "/")+1:]
	uid = strings.TrimSuffix(uid, ".vcf")
	if _, err := d.c.Put(ctx, ref, "text/vcard; charset=utf-8", dav.BuildVCard(c, uid), "-"); err != nil {
		return c, err
	}
	return c, nil
}

func (d davContacts) DeleteContact(ctx context.Context, id string) error {
	return d.c.Delete(ctx, strings.TrimPrefix(id, "dav:"), "")
}

// syncDAVContacts reads the server's address book into the contact cache.
func (e *Engine) syncDAVContacts(ctx context.Context, a mailtypes.Account) {
	if a.Settings[SDAVURL] == "" {
		return
	}
	c, err := e.davFor(a)
	if err != nil {
		return
	}
	books, err := c.Collections(ctx, "addressbook")
	if err != nil {
		return
	}
	var cs []mailtypes.Contact
	for _, b := range books {
		objs, err := c.Contacts(ctx, b.URL)
		if err != nil {
			continue
		}
		for _, o := range objs {
			if g, ok := dav.ParseGroupVCard(o.Data); ok {
				g.ID = "dav:" + o.URL
				_, _ = e.St.PutGroup(g)
				continue
			}
			ct := dav.ParseVCard(o.Data)
			if ct.Email == "" {
				continue
			}
			ct.ID = "dav:" + o.URL
			cs = append(cs, ct)
		}
	}
	if len(cs) > 0 {
		_ = e.St.PutContacts(a.ID, cs)
	}
}

// AddressBook returns the contacts held on the account's CardDAV server.
func (e *Engine) AddressBook(ctx context.Context, accountID string) ([]mailtypes.Contact, error) {
	c, _, err := e.davClient(accountID)
	if err != nil {
		return nil, err
	}
	books, err := c.Collections(ctx, "addressbook")
	if err != nil {
		return nil, err
	}
	var out []mailtypes.Contact
	for _, b := range books {
		objs, err := c.Contacts(ctx, b.URL)
		if err != nil {
			return nil, err
		}
		for _, o := range objs {
			ct := dav.ParseVCard(o.Data)
			ct.ID = "dav:" + o.URL
			out = append(out, ct)
		}
	}
	return out, nil
}

// ---- contact groups ----

// SaveGroup saves a group on this computer and, when accountID has a CardDAV address book, there too as a group card, so
// the group follows the person to every program that reads that address book.
func (e *Engine) SaveGroup(ctx context.Context, accountID string, g mailtypes.ContactGroup) (mailtypes.ContactGroup, error) {
	if strings.TrimSpace(g.Name) == "" {
		return g, errors.New("a group needs a name")
	}
	if accountID != "" {
		if a, err := e.St.Account(accountID); err == nil && a.Settings[SDAVURL] != "" {
			c, err := e.davFor(a)
			if err != nil {
				return g, err
			}
			ref := strings.TrimPrefix(g.ID, "dav:")
			if !strings.HasPrefix(g.ID, "dav:") {
				books, err := c.Collections(ctx, "addressbook")
				if err != nil || len(books) == 0 {
					return g, errors.New("this server has no address book to keep the group in")
				}
				uid := newUID()
				ref = dav.ObjectURL(books[0].URL, uid, ".vcf")
				if g.ID != "" {
					_ = e.St.DeleteGroup(g.ID)
				}
				g.ID = "dav:" + ref
			}
			uid := strings.TrimSuffix(ref[strings.LastIndex(ref, "/")+1:], ".vcf")
			if _, err := c.Put(ctx, ref, "text/vcard; charset=utf-8", dav.BuildGroupVCard(g, uid), "-"); err != nil {
				return g, err
			}
		}
	}
	return e.St.PutGroup(g)
}

// DeleteGroup removes a group here and on the server it came from or was saved to.
func (e *Engine) DeleteGroup(ctx context.Context, accountID, id string) error {
	if strings.HasPrefix(id, "dav:") && accountID != "" {
		if c, _, err := e.davClient(accountID); err == nil {
			if err := c.Delete(ctx, strings.TrimPrefix(id, "dav:"), ""); err != nil {
				return err
			}
		}
	}
	return e.St.DeleteGroup(id)
}

// groupMembers replaces an address that is really a group's name (no @) with the group's members.
func (e *Engine) groupMembers(as []mailtypes.Address) []mailtypes.Address {
	var out []mailtypes.Address
	gs, _ := e.St.Groups()
	for _, a := range as {
		hit := false
		if !strings.Contains(a.Email, "@") {
			for _, g := range gs {
				if strings.EqualFold(strings.TrimSpace(a.Email), g.Name) || strings.EqualFold(strings.TrimSpace(a.Name), g.Name) && a.Email == "" {
					for _, m := range g.Members {
						out = append(out, mailtypes.Address{Email: m})
					}
					hit = true
					break
				}
			}
		}
		if !hit {
			out = append(out, a)
		}
	}
	return out
}
