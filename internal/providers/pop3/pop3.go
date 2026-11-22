// Package pop3 is the POP3 plugin, for the mailboxes that only offer POP3. POP3 has one folder and no flags, so the
// inbox is downloaded into the local cache (by UIDL, once each) and everything else (read state, stars, archive, trash,
// labels as folders, the Sent folder) lives on this computer. Sending is SMTP.
package pop3

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/patrickryan01/halyard/internal/mailtypes"
	"github.com/patrickryan01/halyard/internal/mime"
	"github.com/patrickryan01/halyard/internal/plugin"
	"github.com/patrickryan01/halyard/internal/providers/imap"
)

func init() { plugin.Register(mailtypes.KindPOP3, New) }

const (
	SHost     = "pop3_host"
	SPort     = "pop3_port"
	SSecurity = "pop3_security" // ssl, starttls, none
	SLeave    = "leave_on_server"
)

type Provider struct {
	acct mailtypes.Account
	sec  plugin.Secrets
	smtp *imap.Provider
}

func New(a mailtypes.Account, s plugin.Secrets) (plugin.Provider, error) {
	if a.Settings[SHost] == "" {
		return nil, errors.New("pop3: server host missing")
	}
	// SMTP is the IMAP plugin's sender; give it settings it accepts
	sa := a
	sa.Settings = map[string]string{}
	for k, v := range a.Settings {
		sa.Settings[k] = v
	}
	sa.Settings[imap.SHost] = firstNonEmpty(a.Settings[imap.SSMTPHost], a.Settings[SHost])
	sm, err := imap.New(sa, s)
	if err != nil {
		return nil, err
	}
	return &Provider{acct: a, sec: s, smtp: sm.(*imap.Provider)}, nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (p *Provider) Kind() string { return mailtypes.KindPOP3 }
func (p *Provider) Close() error { return p.smtp.Close() }

func (p *Provider) user() string {
	if u := p.acct.Settings["username"]; u != "" {
		return u
	}
	return p.acct.Email
}

// conn is one POP3 session.
type conn struct {
	c  net.Conn
	tp *textproto.Conn
}

func (p *Provider) dial(ctx context.Context) (*conn, error) {
	host := p.acct.Settings[SHost]
	sec := firstNonEmpty(p.acct.Settings[SSecurity], "ssl")
	port := p.acct.Settings[SPort]
	if port == "" {
		port = map[string]string{"ssl": "995"}[sec]
		if port == "" {
			port = "110"
		}
	}
	addr := net.JoinHostPort(host, port)
	d := &net.Dialer{Timeout: 20 * time.Second}
	tc := &tls.Config{ServerName: host, InsecureSkipVerify: p.acct.Settings["insecure_skip_verify"] == "true", MinVersion: tls.VersionTLS12}
	var raw net.Conn
	var err error
	if sec == "ssl" {
		raw, err = tls.DialWithDialer(d, "tcp", addr, tc)
	} else {
		raw, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return nil, fmt.Errorf("pop3 connect %s: %w", addr, err)
	}
	c := &conn{c: raw, tp: textproto.NewConn(raw)}
	if _, err := c.line(); err != nil {
		raw.Close()
		return nil, err
	}
	if sec == "starttls" {
		if err := c.cmdOK("STLS"); err != nil {
			raw.Close()
			return nil, err
		}
		t := tls.Client(raw, tc)
		if err := t.Handshake(); err != nil {
			raw.Close()
			return nil, err
		}
		c = &conn{c: t, tp: textproto.NewConn(t)}
	}
	pw, err := p.sec.Get()
	if err != nil {
		c.close()
		return nil, err
	}
	if err := c.cmdOK("USER " + p.user()); err != nil {
		c.close()
		return nil, fmt.Errorf("pop3 login: %w", err)
	}
	if err := c.cmdOK("PASS " + string(pw)); err != nil {
		c.close()
		return nil, fmt.Errorf("pop3 login: %w", err)
	}
	return c, nil
}

func (c *conn) close() { _ = c.cmdOK("QUIT"); c.c.Close() }

func (c *conn) line() (string, error) {
	c.c.SetDeadline(time.Now().Add(60 * time.Second))
	l, err := c.tp.ReadLine()
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(l, "-ERR") {
		return "", errors.New(strings.TrimSpace(strings.TrimPrefix(l, "-ERR")))
	}
	return strings.TrimSpace(strings.TrimPrefix(l, "+OK")), nil
}

func (c *conn) cmdOK(cmd string) error {
	if err := c.tp.PrintfLine("%s", cmd); err != nil {
		return err
	}
	_, err := c.line()
	return err
}

// multi sends a command whose answer is a dot terminated block.
func (c *conn) multi(cmd string) ([]string, error) {
	if err := c.tp.PrintfLine("%s", cmd); err != nil {
		return nil, err
	}
	if _, err := c.line(); err != nil {
		return nil, err
	}
	c.c.SetDeadline(time.Now().Add(5 * time.Minute))
	var out []string
	for {
		l, err := c.tp.ReadLine()
		if err != nil {
			return nil, err
		}
		if l == "." {
			return out, nil
		}
		if strings.HasPrefix(l, "..") {
			l = l[1:]
		}
		out = append(out, l)
	}
}

func (c *conn) retr(n int) ([]byte, error) {
	ls, err := c.multi("RETR " + strconv.Itoa(n))
	if err != nil {
		return nil, err
	}
	return []byte(strings.Join(ls, "\r\n") + "\r\n"), nil
}

func (p *Provider) Profile(ctx context.Context) (plugin.Profile, error) {
	c, err := p.dial(ctx)
	if err != nil {
		return plugin.Profile{}, err
	}
	c.close()
	return plugin.Profile{Email: p.acct.Email, Name: p.acct.Name}, nil
}

type syncState struct {
	Seen map[string]bool `json:"seen"`
}

var folders = []mailtypes.Folder{
	{RemoteID: "INBOX", Name: "Inbox", Role: mailtypes.RoleInbox}, {RemoteID: "SENT", Name: "Sent", Role: mailtypes.RoleSent},
	{RemoteID: "ARCHIVE", Name: "Archive", Role: mailtypes.RoleArchive}, {RemoteID: "TRASH", Name: "Trash", Role: mailtypes.RoleTrash},
}

func (p *Provider) Sync(ctx context.Context, state string) (plugin.SyncResult, error) {
	res := plugin.SyncResult{}
	st := syncState{Seen: map[string]bool{}}
	if state != "" {
		_ = json.Unmarshal([]byte(state), &st)
		if st.Seen == nil {
			st.Seen = map[string]bool{}
		}
	}
	res.Change.Folders = folders
	c, err := p.dial(ctx)
	if err != nil {
		return res, err
	}
	defer c.close()
	ul, err := c.multi("UIDL")
	if err != nil {
		return res, fmt.Errorf("pop3 uidl: %w (this server does not list message ids)", err)
	}
	type item struct {
		n   int
		uid string
	}
	var fresh []item
	for _, l := range ul {
		f := strings.Fields(l)
		if len(f) != 2 {
			continue
		}
		n, _ := strconv.Atoi(f[0])
		if !st.Seen[f[1]] {
			fresh = append(fresh, item{n, f[1]})
		}
	}
	limit := 500
	if v, err := strconv.Atoi(p.acct.Settings["sync_limit"]); err == nil && v > 0 {
		limit = v
	}
	if len(fresh) > limit { // the newest ones; the rest are marked seen so they are not fetched later
		for _, it := range fresh[:len(fresh)-limit] {
			st.Seen[it.uid] = true
		}
		fresh = fresh[len(fresh)-limit:]
	}
	leave := p.acct.Settings[SLeave] != "false"
	for _, it := range fresh {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		raw, err := c.retr(it.n)
		if err != nil {
			return res, fmt.Errorf("pop3 retr %d: %w", it.n, err)
		}
		pm, _ := mime.Parse(raw)
		m := mailtypes.Message{AccountID: p.acct.ID, RemoteID: it.uid, Subject: pm.Subject, From: pm.From, To: pm.To, Cc: pm.Cc, ReplyTo: pm.ReplyTo, Date: pm.Date,
			BodyText: pm.Text, BodyHTML: pm.HTML, MessageID: pm.MessageID, InReplyTo: pm.InReplyTo, References: pm.References, Attach: pm.Attach,
			Folders: []string{"INBOX"}, Snippet: mime.Snippet(pm.Text, pm.HTML),
			Size: pm.Size, ICS: pm.ICS, ListUnsubscribe: pm.ListUnsubscribe, ListUnsubscribePost: pm.ListUnsubscribePost, ReceiptTo: pm.ReceiptTo}
		if m.Date.IsZero() {
			m.Date = time.Now()
		}
		res.Change.Upsert = append(res.Change.Upsert, m)
		st.Seen[it.uid] = true
		if !leave {
			_ = c.cmdOK("DELE " + strconv.Itoa(it.n))
		}
	}
	b, _ := json.Marshal(st)
	res.State = string(b)
	return res, nil
}

// Apply does nothing on the server: a POP3 mailbox keeps no flags or folders, so those live in the cache.
func (p *Provider) Apply(ctx context.Context, a mailtypes.Action) error { return nil }

func (p *Provider) Send(ctx context.Context, o mailtypes.Outgoing) error {
	return p.smtp.SendSMTP(ctx, o)
}

// Attachment re-downloads the message and reads the part from it.
func (p *Provider) Attachment(ctx context.Context, msgID, attID string) ([]byte, string, error) {
	c, err := p.dial(ctx)
	if err != nil {
		return nil, "", err
	}
	defer c.close()
	ul, err := c.multi("UIDL")
	if err != nil {
		return nil, "", err
	}
	for _, l := range ul {
		f := strings.Fields(l)
		if len(f) == 2 && f[1] == msgID {
			n, _ := strconv.Atoi(f[0])
			raw, err := c.retr(n)
			if err != nil {
				return nil, "", err
			}
			pm, _ := mime.Parse(raw)
			idx, _ := strconv.Atoi(attID)
			if idx < 0 || idx >= len(pm.Parts) {
				return nil, "", errors.New("attachment not found")
			}
			return pm.Parts[idx], pm.Attach[idx].ContentType, nil
		}
	}
	return nil, "", errors.New("the message is no longer on the server")
}

func (p *Provider) Contacts(ctx context.Context, q string) ([]mailtypes.Contact, error) {
	return nil, nil
}

// Watch polls: POP3 has no way for a server to say something arrived.
func (p *Provider) Watch(ctx context.Context, notify func()) error {
	t := time.NewTicker(2 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			notify()
		}
	}
}
