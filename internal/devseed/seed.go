//go:build demo

// Package devseed fills a store with made up mail so the UI can be rendered
// and photographed for design review. It is compiled only with -tags demo and
// never ships.
package devseed

import (
	"fmt"
	"time"

	"github.com/patrickryan01/halyard/internal/mailtypes"
	"github.com/patrickryan01/halyard/internal/plugin"
	"github.com/patrickryan01/halyard/internal/store"

	"context"
)

func init() {
	plugin.Register("demo", func(a mailtypes.Account, s plugin.Secrets) (plugin.Provider, error) { return demoProv{}, nil })
}

type demoProv struct{}

func (demoProv) Kind() string                                    { return "demo" }
func (demoProv) Profile(context.Context) (plugin.Profile, error) { return plugin.Profile{}, nil }
func (demoProv) Sync(_ context.Context, st string) (plugin.SyncResult, error) {
	return plugin.SyncResult{State: "demo"}, nil
}
func (demoProv) Send(context.Context, mailtypes.Outgoing) error { return nil }
func (demoProv) Apply(context.Context, mailtypes.Action) error  { return nil }
func (demoProv) Attachment(context.Context, string, string) ([]byte, string, error) {
	return []byte("demo"), "text/plain", nil
}
func (demoProv) Contacts(context.Context, string) ([]mailtypes.Contact, error) { return nil, nil }
func (demoProv) Watch(ctx context.Context, n func()) error                 { <-ctx.Done(); return nil }
func (demoProv) Close() error                                              { return nil }

type m struct {
	acct, folder string
	from, fromEm string
	subj, body   string
	html         string
	ago          time.Duration
	unread, star bool
	thread       string
	attach       []string
	to           string
}

// Seed adds the demo accounts and mail. accounts is how many of the three to add.
func Seed(st *store.Store, accounts int) error {
	now := time.Now()
	accts := []mailtypes.Account{
		{ID: "g1", Kind: "demo", Email: "alex@northwind.example", Name: "Alex Rivera", Color: "#4f7cff", Settings: map[string]string{}, Signature: "Alex Rivera\nOperations, Northwind Mills"},
		{ID: "m1", Kind: "demo", Email: "alex.rivera@harborlogistics.example", Name: "Alex Rivera", Color: "#30a46c", Settings: map[string]string{}},
		{ID: "i1", Kind: "demo", Email: "alex@riverafamily.example", Name: "Alex Rivera", Color: "#f5a524", Settings: map[string]string{}},
	}
	if accounts > 3 {
		accounts = 3
	}
	for _, a := range accts[:accounts] {
		if err := st.PutAccount(a); err != nil {
			return err
		}
		if err := st.SetSyncState(a.ID, "demo", ""); err != nil {
			return err
		}
		fs := []mailtypes.Folder{{RemoteID: "INBOX", Name: "Inbox", Role: mailtypes.RoleInbox}, {RemoteID: "SENT", Name: "Sent", Role: mailtypes.RoleSent},
			{RemoteID: "DRAFT", Name: "Drafts", Role: mailtypes.RoleDrafts}, {RemoteID: "ARCH", Name: "Archive", Role: mailtypes.RoleArchive},
			{RemoteID: "SPAM", Name: "Spam", Role: mailtypes.RoleSpam}, {RemoteID: "TRASH", Name: "Trash", Role: mailtypes.RoleTrash},
			{RemoteID: "L1", Name: "Suppliers", Role: mailtypes.RoleCustom, IsLabel: a.ID == "g1"}, {RemoteID: "L2", Name: "Projects", Role: mailtypes.RoleCustom, IsLabel: a.ID == "g1"}}
		if err := st.ReplaceFolders(a.ID, fs); err != nil {
			return err
		}
	}
	h := time.Hour
	list := []m{
		{"g1", "INBOX", "Dana Whitfield", "dana@millwork.example", "Q4 line 3 retrofit: schedule and quote", "Alex, the revised quote is attached. We can start the week of Dec 8 if the PO lands by Friday.", "<p>Alex,</p><p>The revised quote is attached. We can start the week of <b>Dec 8</b> if the PO lands by Friday.</p><p>Two things to flag:</p><ul><li>The VFD lead time moved to 3 weeks.</li><li>Install needs a 6 hour shutdown window.</li></ul><p>Thanks,<br>Dana</p>", 6 * h, false, true, "t1", []string{"Quote-4417-rev2.pdf", "Line3-layout.dwg"}, ""},
		{"g1", "INBOX", "Alex Rivera", "alex@northwind.example", "Re: Q4 line 3 retrofit: schedule and quote", "Thanks Dana. Can you confirm the 6 hour window works on a Saturday? Maintenance wants to cover it.", "<p>Thanks Dana. Can you confirm the 6 hour window works on a Saturday? Maintenance wants to cover it.</p>", 3 * h, false, false, "t1", nil, ""},
		{"g1", "INBOX", "Dana Whitfield", "dana@millwork.example", "Re: Q4 line 3 retrofit: schedule and quote", "Saturday works. We will bring two techs.", "<p>Saturday works. We will bring two techs.</p><div class=\"gmail_quote\"><blockquote>Thanks Dana. Can you confirm the 6 hour window works on a Saturday?</blockquote></div>", 25 * time.Minute, true, false, "t1", nil, ""},
		{"g1", "INBOX", "Priya Nair", "priya@northwind.example", "Weekly safety walk notes", "Notes from Tuesday. Guard on press 2 needs a new interlock switch, ordered.", "<p>Notes from Tuesday:</p><ol><li>Guard on press 2 needs a new interlock switch (ordered).</li><li>Floor markings at dock 4 are worn.</li></ol>", 2 * h, true, false, "t2", []string{"walk-notes.pdf"}, ""},
		{"g1", "INBOX", "Marcus Chen", "marcus@northwind.example", "Lunch Friday?", "Taqueria on Main, noon. Bring the invoice folder.", "<p>Taqueria on Main, noon. Bring the invoice folder.</p>", 5 * h, false, true, "t3", nil, ""},
		{"g1", "INBOX", "Billing", "billing@cloudhost.example", "Your invoice 20261104 is ready", "Invoice total 1,284.00 USD. Payment is due in 14 days.", "<p>Invoice total <b>1,284.00 USD</b>. Payment is due in 14 days.</p>", 9 * h, false, false, "t4", []string{"invoice-20261104.pdf"}, ""},
		{"g1", "INBOX", "GitHub", "noreply@github.example", "[plant-tools] Pull request #212: tune scan rate", "Merged into main by marcus.", "<p>Merged into <code>main</code> by marcus.</p>", 22 * h, false, false, "t5", nil, ""},
		{"g1", "INBOX", "Rosa Delgado", "rosa@supplier.example", "Bearing order 8812 shipped", "Tracking number is below. Expected Thursday.", "<p>Tracking number is below. Expected Thursday.</p>", 30 * h, false, false, "t6", nil, ""},
		{"g1", "INBOX", "Tom Okafor", "tom@northwind.example", "Forklift 3 inspection overdue", "Reminder: the quarterly inspection lapsed on the 14th.", "<p>Reminder: the quarterly inspection lapsed on the 14th.</p>", 50 * h, false, false, "t7", nil, ""},
		{"g1", "INBOX", "Newsletter", "hello@plantweekly.example", "Plant Weekly: predictive maintenance without the hype", "This week: three sensors worth buying, one worth skipping.", "<p>This week: three sensors worth buying, one worth skipping.</p>", 70 * h, false, false, "t8", nil, ""},
		{"g1", "SENT", "Alex Rivera", "alex@northwind.example", "PO 5521 for Millwork", "Dana, PO attached. Please confirm receipt.", "<p>Dana, PO attached. Please confirm receipt.</p>", 100 * h, false, false, "t9", []string{"PO-5521.pdf"}, "dana@millwork.example"},
		{"m1", "INBOX", "Sam Patel", "sam@harborlogistics.example", "Dock schedule for next week", "Attaching the updated dock schedule. Door 6 is out for repairs.", "<p>Attaching the updated dock schedule. Door 6 is out for repairs.</p>", 55 * time.Minute, true, false, "u1", []string{"dock-schedule.xlsx"}, ""},
		{"m1", "INBOX", "Lena Fischer", "lena@harborlogistics.example", "Customs hold on container 7731", "The broker needs the commercial invoice by noon tomorrow.", "<p>The broker needs the <b>commercial invoice</b> by noon tomorrow.</p>", 4 * h, true, true, "u2", nil, ""},
		{"m1", "INBOX", "Payroll", "payroll@harborlogistics.example", "Pay statement available", "Your pay statement for the period ending Nov 15 is available.", "<p>Your pay statement for the period ending Nov 15 is available.</p>", 26 * h, false, false, "u3", nil, ""},
		{"i1", "INBOX", "Mom", "mom@riverafamily.example", "Thanksgiving plans", "Dinner at 4. Bring the pie. Your aunt is flying in Wednesday.", "<p>Dinner at 4. Bring the pie. Your aunt is flying in Wednesday.</p>", 90 * time.Minute, true, true, "v1", nil, ""},
		{"i1", "INBOX", "Library", "noreply@library.example", "Hold ready for pickup", "Your hold on \"The Soul of a New Machine\" is ready.", "<p>Your hold on <i>The Soul of a New Machine</i> is ready.</p>", 8 * h, false, false, "v2", nil, ""},
	}
	for i, x := range list {
		if x.acct == "m1" && accounts < 2 || x.acct == "i1" && accounts < 3 {
			continue
		}
		to := []mailtypes.Address{{Name: "Alex Rivera", Email: acctEmail(accts, x.acct)}}
		if x.to != "" {
			to = []mailtypes.Address{{Email: x.to}}
		}
		msg := mailtypes.Message{AccountID: x.acct, RemoteID: fmt.Sprint("r", i), ThreadID: x.acct + "/" + x.thread, Subject: x.subj,
			From: mailtypes.Address{Name: x.from, Email: x.fromEm}, To: to, Date: now.Add(-x.ago), Snippet: x.body, BodyText: x.body, BodyHTML: x.html,
			MessageID: fmt.Sprintf("<m%d@demo>", i), Read: !x.unread, Starred: x.star, Folders: []string{x.folder}}
		for j, a := range x.attach {
			msg.Attach = append(msg.Attach, mailtypes.Attachment{ID: fmt.Sprint(j), Filename: a, ContentType: "application/pdf", Size: int64(48000 + 91000*(j+1))})
		}
		if err := st.UpsertMessages([]mailtypes.Message{msg}); err != nil {
			return err
		}
	}
	return st.PutContacts("g1", []mailtypes.Contact{{Name: "Dana Whitfield", Email: "dana@millwork.example"}, {Name: "Priya Nair", Email: "priya@northwind.example"}, {Name: "Marcus Chen", Email: "marcus@northwind.example"}})
}

func acctEmail(as []mailtypes.Account, id string) string {
	for _, a := range as {
		if a.ID == id {
			return a.Email
		}
	}
	return ""
}
