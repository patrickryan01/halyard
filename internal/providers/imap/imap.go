// Package imap is the self-hosted plugin: IMAP with IDLE for receiving and
// SMTP for sending. It works with Dovecot, Stalwart, Cyrus, Fastmail, iCloud
// and anything else that speaks the standards.
package imap

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"

	"github.com/patrickryan01/halyard/internal/mime"
	"github.com/patrickryan01/halyard/internal/mailtypes"
	"github.com/patrickryan01/halyard/internal/plugin"
)

func init() { plugin.Register(mailtypes.KindIMAP, New) }

// Settings keys on the account.
const (
	SHost       = "imap_host"
	SPort       = "imap_port"
	SSecurity   = "imap_security" // ssl, starttls, none
	SUser       = "username"
	SSMTPHost   = "smtp_host"
	SSMTPPort   = "smtp_port"
	SSMTPSec    = "smtp_security"
	SSkipVerify = "insecure_skip_verify"
	SSaveSent   = "save_sent" // "false" when the SMTP server files sent mail itself
	SSyncLimit  = "sync_limit"
	STrusted    = "trusted_ca" // PEM path
)

type Provider struct {
	acct mailtypes.Account
	sec  plugin.Secrets

	mu   sync.Mutex // one command connection at a time
	conn *imapclient.Client
}

func New(a mailtypes.Account, s plugin.Secrets) (plugin.Provider, error) {
	if a.Settings[SHost] == "" {
		return nil, errors.New("imap: server host missing")
	}
	return &Provider{acct: a, sec: s}, nil
}

func (p *Provider) Kind() string { return mailtypes.KindIMAP }

func (p *Provider) set(k, def string) string {
	if v := p.acct.Settings[k]; v != "" {
		return v
	}
	return def
}

func (p *Provider) tlsConf(host string) *tls.Config {
	return &tls.Config{ServerName: host, InsecureSkipVerify: p.acct.Settings[SSkipVerify] == "true", MinVersion: tls.VersionTLS12}
}

func (p *Provider) user() string { return p.set(SUser, p.acct.Email) }

func (p *Provider) password() (string, error) {
	b, err := p.sec.Get()
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (p *Provider) dial(h *imapclient.UnilateralDataHandler) (*imapclient.Client, error) {
	host := p.acct.Settings[SHost]
	sec := p.set(SSecurity, "ssl")
	port := p.set(SPort, map[string]string{"ssl": "993"}[sec])
	if port == "" {
		port = "143"
	}
	addr := net.JoinHostPort(host, port)
	opt := &imapclient.Options{TLSConfig: p.tlsConf(host), UnilateralDataHandler: h, Dialer: &net.Dialer{Timeout: 20 * time.Second}}
	var c *imapclient.Client
	var err error
	switch sec {
	case "ssl":
		c, err = imapclient.DialTLS(addr, opt)
	case "starttls":
		c, err = imapclient.DialStartTLS(addr, opt)
	default:
		c, err = imapclient.DialInsecure(addr, opt)
	}
	if err != nil {
		return nil, fmt.Errorf("imap connect %s: %w", addr, err)
	}
	pw, err := p.password()
	if err != nil {
		c.Close()
		return nil, err
	}
	if p.acct.Settings["auth"] == "xoauth2" {
		// the secret is an OAuth access token (Gmail and Outlook accept it in place of a password)
		if err := c.Authenticate(xoauth2{user: p.user(), token: pw}); err != nil {
			c.Close()
			return nil, fmt.Errorf("imap login: %w", err)
		}
		return c, nil
	}
	if err := c.Login(p.user(), pw).Wait(); err != nil {
		c.Close()
		return nil, fmt.Errorf("imap login: %w", friendlyLogin(err, p.acct.Settings[SHost]))
	}
	return c, nil
}

// withConn runs f on the shared command connection, redialing once if it died.
func (p *Provider) withConn(f func(c *imapclient.Client) error) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for try := 0; try < 2; try++ {
		if p.conn == nil {
			c, err := p.dial(nil)
			if err != nil {
				return err
			}
			p.conn = c
		}
		err := f(p.conn)
		if err == nil {
			return nil
		}
		var ne net.Error
		if errors.Is(err, io.EOF) || errors.As(err, &ne) || strings.Contains(err.Error(), "closed") || strings.Contains(err.Error(), "broken pipe") {
			p.conn.Close()
			p.conn = nil
			continue
		}
		return err
	}
	return errors.New("imap: connection kept failing")
}

func (p *Provider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn != nil {
		_ = p.conn.Logout().Wait()
		p.conn.Close()
		p.conn = nil
	}
	return nil
}

func (p *Provider) Profile(ctx context.Context) (plugin.Profile, error) {
	err := p.withConn(func(c *imapclient.Client) error { return nil })
	return plugin.Profile{Email: p.acct.Email, Name: p.acct.Name}, err
}

// ---- remote IDs: mailbox | uidvalidity | uid ----

func rid(mbox string, v uint32, uid imap.UID) string {
	return mbox + "|" + strconv.FormatUint(uint64(v), 10) + "|" + strconv.FormatUint(uint64(uid), 10)
}

func parseRID(s string) (mbox string, uid imap.UID, err error) {
	i := strings.LastIndex(s, "|")
	if i < 0 {
		return "", 0, fmt.Errorf("bad imap id %q", s)
	}
	j := strings.LastIndex(s[:i], "|")
	if j < 0 {
		return "", 0, fmt.Errorf("bad imap id %q", s)
	}
	u, err := strconv.ParseUint(s[i+1:], 10, 32)
	return s[:j], imap.UID(u), err
}

// ---- folders ----

func roleOf(l *imap.ListData) string {
	for _, a := range l.Attrs {
		switch a {
		case imap.MailboxAttrSent:
			return mailtypes.RoleSent
		case imap.MailboxAttrDrafts:
			return mailtypes.RoleDrafts
		case imap.MailboxAttrTrash:
			return mailtypes.RoleTrash
		case imap.MailboxAttrJunk:
			return mailtypes.RoleSpam
		case imap.MailboxAttrArchive:
			return mailtypes.RoleArchive
		case imap.MailboxAttrAll:
			return mailtypes.RoleAll
		}
	}
	if strings.EqualFold(l.Mailbox, "INBOX") {
		return mailtypes.RoleInbox
	}
	n := strings.ToLower(l.Mailbox)
	if i := strings.LastIndexAny(n, "/."); i >= 0 {
		n = n[i+1:]
	}
	switch n {
	case "sent", "sent items", "sent mail", "sent messages":
		return mailtypes.RoleSent
	case "drafts", "draft":
		return mailtypes.RoleDrafts
	case "trash", "deleted items", "deleted messages", "bin":
		return mailtypes.RoleTrash
	case "junk", "spam", "junk e-mail", "junk email":
		return mailtypes.RoleSpam
	case "archive", "archives":
		return mailtypes.RoleArchive
	}
	return mailtypes.RoleCustom
}

func (p *Provider) listFolders(c *imapclient.Client) ([]*imap.ListData, []mailtypes.Folder, error) {
	opt := &imap.ListOptions{}
	if c.Caps().Has(imap.CapSpecialUse) {
		opt.ReturnSpecialUse = true
	}
	ls, err := c.List("", "*", opt).Collect()
	if err != nil {
		return nil, nil, err
	}
	var keep []*imap.ListData
	var fs []mailtypes.Folder
	for _, l := range ls {
		skip := false
		for _, a := range l.Attrs {
			if a == imap.MailboxAttrNoSelect || a == imap.MailboxAttrNonExistent {
				skip = true
			}
		}
		if skip {
			continue
		}
		keep = append(keep, l)
		fs = append(fs, mailtypes.Folder{RemoteID: l.Mailbox, Name: l.Mailbox, Role: roleOf(l)})
	}
	return keep, fs, nil
}

// ---- sync ----

type mboxState struct {
	V uint32           `json:"v"` // UIDVALIDITY
	N uint32           `json:"n"` // UIDNEXT seen
	F map[string]uint8 `json:"f"` // uid -> flag bits
}

type syncState struct {
	M map[string]*mboxState `json:"m"`
}

const (
	bitSeen = 1 << iota
	bitFlag
	bitDraft
)

func bits(fl []imap.Flag) uint8 {
	var b uint8
	for _, f := range fl {
		switch f {
		case imap.FlagSeen:
			b |= bitSeen
		case imap.FlagFlagged:
			b |= bitFlag
		case imap.FlagDraft:
			b |= bitDraft
		}
	}
	return b
}

func (p *Provider) syncLimit() int {
	if n, err := strconv.Atoi(p.acct.Settings[SSyncLimit]); err == nil && n > 0 {
		return n
	}
	return 5000
}

func (p *Provider) Sync(ctx context.Context, state string) (plugin.SyncResult, error) {
	var res plugin.SyncResult
	st := syncState{M: map[string]*mboxState{}}
	if state != "" {
		_ = json.Unmarshal([]byte(state), &st)
		if st.M == nil {
			st.M = map[string]*mboxState{}
		}
	}
	res.Full = state == ""
	err := p.withConn(func(c *imapclient.Client) error {
		_, folders, err := p.listFolders(c)
		if err != nil {
			return err
		}
		res.Change.Folders = folders
		live := map[string]bool{}
		for _, f := range folders {
			live[f.RemoteID] = true
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := p.syncMailbox(ctx, c, f.RemoteID, st.M, &res.Change); err != nil {
				return fmt.Errorf("mailbox %s: %w", f.RemoteID, err)
			}
		}
		for name := range st.M {
			if !live[name] {
				delete(st.M, name)
			}
		}
		return nil
	})
	if err != nil {
		return res, err
	}
	b, _ := json.Marshal(st)
	res.State = string(b)
	return res, nil
}

func (p *Provider) syncMailbox(ctx context.Context, c *imapclient.Client, name string, all map[string]*mboxState, ch *mailtypes.Change) error {
	sd, err := c.Select(name, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return err
	}
	ms := all[name]
	if ms != nil && ms.V != sd.UIDValidity {
		// the server renumbered the mailbox: everything we know is void
		for u := range ms.F {
			ch.Delete = append(ch.Delete, name+"|"+strconv.FormatUint(uint64(ms.V), 10)+"|"+u)
		}
		ms = nil
	}
	if ms == nil {
		ms = &mboxState{V: sd.UIDValidity, F: map[string]uint8{}}
		all[name] = ms
	}
	if sd.NumMessages == 0 {
		for u := range ms.F {
			ch.Delete = append(ch.Delete, rid(name, ms.V, parseU(u)))
		}
		ms.F, ms.N = map[string]uint8{}, uint32(sd.UIDNext)
		return nil
	}
	// UID and FLAGS of everything: finds expunges and flag changes.
	var all1 imap.UIDSet
	all1.AddRange(1, 0)
	bufs, err := c.Fetch(all1, &imap.FetchOptions{UID: true, Flags: true}).Collect()
	if err != nil {
		return err
	}
	server := map[string]uint8{}
	var uids []imap.UID
	for _, b := range bufs {
		server[strconv.FormatUint(uint64(b.UID), 10)] = bits(b.Flags)
		uids = append(uids, b.UID)
	}
	sort.Slice(uids, func(i, j int) bool { return uids[i] > uids[j] })
	for u, fb := range ms.F {
		sb, ok := server[u]
		switch {
		case !ok:
			ch.Delete = append(ch.Delete, rid(name, ms.V, parseU(u)))
		case sb != fb:
			ch.Flags = append(ch.Flags, mailtypes.FlagUpdate{RemoteID: rid(name, ms.V, parseU(u)), Read: sb&bitSeen != 0, Starred: sb&bitFlag != 0})
		}
	}
	// new messages: the newest syncLimit of those never seen
	var fresh []imap.UID
	for _, u := range uids {
		if _, known := ms.F[strconv.FormatUint(uint64(u), 10)]; !known {
			fresh = append(fresh, u)
		}
	}
	if len(fresh) > p.syncLimit() {
		fresh = fresh[:p.syncLimit()]
	}
	for i := 0; i < len(fresh); i += 40 {
		end := i + 40
		if end > len(fresh) {
			end = len(fresh)
		}
		var set imap.UIDSet
		for _, u := range fresh[i:end] {
			set.AddNum(u)
		}
		items, err := c.Fetch(set, &imap.FetchOptions{UID: true, Flags: true, InternalDate: true,
			BodySection: []*imap.FetchItemBodySection{{Peek: true}}}).Collect()
		if err != nil {
			return err
		}
		for _, it := range items {
			raw := it.FindBodySection(&imap.FetchItemBodySection{Peek: true})
			if raw == nil {
				raw = it.FindBodySection(&imap.FetchItemBodySection{})
			}
			m, err := toMessage(p.acct.ID, name, ms.V, it, raw)
			if err != nil {
				continue
			}
			ch.Upsert = append(ch.Upsert, m)
		}
	}
	// the state records everything the server has, including messages beyond
	// the sync limit, so they are not refetched and then reported as new
	next := map[string]uint8{}
	for u, b := range server {
		next[u] = b
	}
	ms.F = next
	ms.N = uint32(sd.UIDNext)
	return nil
}

func parseU(s string) imap.UID { n, _ := strconv.ParseUint(s, 10, 32); return imap.UID(n) }

func toMessage(acct, mbox string, v uint32, it *imapclient.FetchMessageBuffer, raw []byte) (mailtypes.Message, error) {
	pm, err := mime.Parse(raw)
	if err != nil && len(raw) == 0 {
		return mailtypes.Message{}, err
	}
	m := mailtypes.Message{
		AccountID: acct, RemoteID: rid(mbox, v, it.UID), Subject: pm.Subject, From: pm.From, To: pm.To, Cc: pm.Cc, ReplyTo: pm.ReplyTo,
		Date: pm.Date, BodyText: pm.Text, BodyHTML: pm.HTML, MessageID: pm.MessageID, InReplyTo: pm.InReplyTo, References: pm.References,
		Attach: pm.Attach, Folders: []string{mbox}, Snippet: mime.Snippet(pm.Text, pm.HTML),
		Size: pm.Size, ICS: pm.ICS, ListUnsubscribe: pm.ListUnsubscribe, ListUnsubscribePost: pm.ListUnsubscribePost, ReceiptTo: pm.ReceiptTo,
	}
	if m.Date.IsZero() {
		m.Date = it.InternalDate
	}
	b := bits(it.Flags)
	m.Read, m.Starred, m.Draft = b&bitSeen != 0, b&bitFlag != 0, b&bitDraft != 0
	return m, nil
}

// ---- actions ----

func (p *Provider) folderRole(c *imapclient.Client, role string) (string, error) {
	_, fs, err := p.listFolders(c)
	if err != nil {
		return "", err
	}
	for _, f := range fs {
		if f.Role == role {
			return f.RemoteID, nil
		}
	}
	return "", nil
}

func (p *Provider) Apply(ctx context.Context, a mailtypes.Action) error {
	byBox := map[string]*imap.UIDSet{}
	for _, id := range a.IDs {
		mb, uid, err := parseRID(id)
		if err != nil {
			return err
		}
		if byBox[mb] == nil {
			byBox[mb] = &imap.UIDSet{}
		}
		byBox[mb].AddNum(uid)
	}
	return p.withConn(func(c *imapclient.Client) error {
		for mb, set := range byBox {
			if _, err := c.Select(mb, nil).Wait(); err != nil {
				return err
			}
			flags := func(op imap.StoreFlagsOp, f imap.Flag) error {
				return c.Store(*set, &imap.StoreFlags{Op: op, Silent: true, Flags: []imap.Flag{f}}, nil).Close()
			}
			moveRole := func(role string) error {
				dest, err := p.folderRole(c, role)
				if err != nil {
					return err
				}
				if dest == "" {
					dest = map[string]string{mailtypes.RoleArchive: "Archive", mailtypes.RoleTrash: "Trash", mailtypes.RoleSpam: "Junk"}[role]
					if dest == "" {
						return fmt.Errorf("no %s folder", role)
					}
					if err := c.Create(dest, nil).Wait(); err != nil {
						return err
					}
				}
				if dest == mb {
					return nil
				}
				return move(c, *set, dest)
			}
			var err error
			switch a.Kind {
			case mailtypes.ActRead:
				err = flags(imap.StoreFlagsAdd, imap.FlagSeen)
			case mailtypes.ActUnread:
				err = flags(imap.StoreFlagsDel, imap.FlagSeen)
			case mailtypes.ActStar:
				err = flags(imap.StoreFlagsAdd, imap.FlagFlagged)
			case mailtypes.ActUnstar:
				err = flags(imap.StoreFlagsDel, imap.FlagFlagged)
			case mailtypes.ActArchive:
				err = moveRole(mailtypes.RoleArchive)
			case mailtypes.ActTrash:
				err = moveRole(mailtypes.RoleTrash)
			case mailtypes.ActSpam:
				err = moveRole(mailtypes.RoleSpam)
			case mailtypes.ActUntrash:
				err = moveRole(mailtypes.RoleInbox)
			case mailtypes.ActMove:
				if a.FolderID != mb {
					err = move(c, *set, a.FolderID)
				}
			case mailtypes.ActLabel:
				_, err = c.Copy(*set, a.FolderID).Wait()
			case mailtypes.ActUnlabel, mailtypes.ActDelete:
				if err = flags(imap.StoreFlagsAdd, imap.FlagDeleted); err == nil {
					err = c.UIDExpunge(*set).Close()
				}
			default:
				err = fmt.Errorf("imap: unknown action %q", a.Kind)
			}
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func move(c *imapclient.Client, set imap.UIDSet, dest string) error {
	if c.Caps().Has(imap.CapMove) {
		_, err := c.Move(set, dest).Wait()
		return err
	}
	if _, err := c.Copy(set, dest).Wait(); err != nil {
		return err
	}
	if err := c.Store(set, &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagDeleted}}, nil).Close(); err != nil {
		return err
	}
	return c.UIDExpunge(set).Close()
}

func (p *Provider) Attachment(ctx context.Context, remoteID, attID string) ([]byte, string, error) {
	mb, uid, err := parseRID(remoteID)
	if err != nil {
		return nil, "", err
	}
	idx, err := strconv.Atoi(attID)
	if err != nil {
		return nil, "", err
	}
	var raw []byte
	err = p.withConn(func(c *imapclient.Client) error {
		if _, err := c.Select(mb, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
			return err
		}
		bufs, err := c.Fetch(imap.UIDSetNum(uid), &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{{Peek: true}}}).Collect()
		if err != nil {
			return err
		}
		if len(bufs) == 0 {
			return errors.New("message not found")
		}
		raw = bufs[0].FindBodySection(&imap.FetchItemBodySection{Peek: true})
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	pm, err := mime.Parse(raw)
	if err != nil || idx < 0 || idx >= len(pm.Parts) {
		return nil, "", errors.New("attachment not found")
	}
	return pm.Parts[idx], pm.Attach[idx].ContentType, nil
}

// ---- send ----

func (p *Provider) smtpClient() (*smtp.Client, error) {
	host := p.set(SSMTPHost, p.acct.Settings[SHost])
	sec := p.set(SSMTPSec, "starttls")
	port := p.set(SSMTPPort, map[string]string{"ssl": "465", "starttls": "587"}[sec])
	if port == "" {
		port = "25"
	}
	addr := net.JoinHostPort(host, port)
	var c *smtp.Client
	var err error
	switch sec {
	case "ssl":
		c, err = smtp.DialTLS(addr, p.tlsConf(host))
	case "starttls":
		c, err = smtp.DialStartTLS(addr, p.tlsConf(host))
	default:
		c, err = smtp.Dial(addr)
	}
	if err != nil {
		return nil, fmt.Errorf("smtp connect %s: %w", addr, err)
	}
	pw, err := p.password()
	if err != nil {
		c.Close()
		return nil, err
	}
	var a sasl.Client = sasl.NewPlainClient("", p.user(), pw)
	if p.acct.Settings["auth"] == "xoauth2" {
		a = xoauth2{user: p.user(), token: pw}
	}
	if err := c.Auth(a); err != nil {
		c.Close()
		return nil, fmt.Errorf("smtp auth: %w", friendlyLogin(err, host))
	}
	return c, nil
}

// SendSMTP sends through the account's SMTP server and does nothing else. Accounts without an IMAP mailbox use it.
func (p *Provider) SendSMTP(ctx context.Context, o mailtypes.Outgoing) error {
	domain := "localhost"
	if i := strings.LastIndex(o.From.Email, "@"); i >= 0 {
		domain = o.From.Email[i+1:]
	}
	raw, err := mime.Build(o, mime.MessageID(domain, "m"))
	if err != nil {
		return err
	}
	c, err := p.smtpClient()
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.SendMail(o.From.Email, mime.Envelope(o), strings.NewReader(string(raw))); err != nil {
		return fmt.Errorf("smtp send: %w", err)
	}
	return c.Quit()
}

func (p *Provider) Send(ctx context.Context, o mailtypes.Outgoing) error {
	domain := "localhost"
	if i := strings.LastIndex(o.From.Email, "@"); i >= 0 {
		domain = o.From.Email[i+1:]
	}
	raw, err := mime.Build(o, mime.MessageID(domain, "m"))
	if err != nil {
		return err
	}
	c, err := p.smtpClient()
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.SendMail(o.From.Email, mime.Envelope(o), strings.NewReader(string(raw))); err != nil {
		return fmt.Errorf("smtp send: %w", err)
	}
	_ = c.Quit()
	if p.acct.Settings[SSaveSent] == "false" {
		return nil
	}
	return p.withConn(func(ic *imapclient.Client) error {
		sent, err := p.folderRole(ic, mailtypes.RoleSent)
		if err != nil {
			return err
		}
		if sent == "" {
			sent = "Sent"
			if err := ic.Create(sent, nil).Wait(); err != nil {
				return err
			}
		}
		ap := ic.Append(sent, int64(len(raw)), &imap.AppendOptions{Flags: []imap.Flag{imap.FlagSeen}, Time: time.Now()})
		if _, err := ap.Write(raw); err != nil {
			return err
		}
		if err := ap.Close(); err != nil {
			return err
		}
		_, err = ap.Wait()
		return err
	})
}

func (p *Provider) Contacts(ctx context.Context, q string) ([]mailtypes.Contact, error) {
	return nil, nil // plain IMAP has no address book; the cache learns from mail
}

// ---- watch ----

func (p *Provider) Watch(ctx context.Context, notify func()) error {
	for ctx.Err() == nil {
		err := p.watchOnce(ctx, notify)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (p *Provider) watchOnce(ctx context.Context, notify func()) error {
	ping := make(chan struct{}, 1)
	poke := func() {
		select {
		case ping <- struct{}{}:
		default:
		}
	}
	c, err := p.dial(&imapclient.UnilateralDataHandler{
		Mailbox: func(d *imapclient.UnilateralDataMailbox) { poke() },
		Expunge: func(uint32) { poke() },
		Fetch:   func(*imapclient.FetchMessageData) { poke() },
	})
	if err != nil {
		return err
	}
	defer c.Close()
	if _, err := c.Select("INBOX", &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		return err
	}
	if !c.Caps().Has(imap.CapIdle) {
		// no IDLE on this server: a short poll is the only option
		t := time.NewTicker(45 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-t.C:
				notify()
			case <-c.Closed():
				return errors.New("imap watch: connection closed")
			}
		}
	}
	for {
		idle, err := c.Idle()
		if err != nil {
			return err
		}
		restart := time.NewTimer(20 * time.Minute)
		var out error
		stop := false
		select {
		case <-ctx.Done():
			stop = true
		case <-ping:
			notify()
		case <-restart.C:
		case <-c.Closed():
			out = errors.New("imap watch: connection closed")
		}
		restart.Stop()
		_ = idle.Close()
		if err := idle.Wait(); err != nil && out == nil && !stop {
			out = err
		}
		if stop {
			return nil
		}
		if out != nil {
			return out
		}
	}
}

// CreateFolder makes a mailbox. IMAP has no way to give it a role, so role is ignored.
func (p *Provider) CreateFolder(ctx context.Context, name, role string) (string, error) {
	err := p.withConn(func(c *imapclient.Client) error { return c.Create(name, nil).Wait() })
	return name, err
}

// ---- drafts ----

// SaveDraft appends the draft to the Drafts mailbox with the Draft flag. An
// update appends the new version and expunges the old one.
func (p *Provider) SaveDraft(ctx context.Context, o mailtypes.Outgoing, replaceID string) (string, error) {
	domain := "localhost"
	if i := strings.LastIndex(o.From.Email, "@"); i >= 0 {
		domain = o.From.Email[i+1:]
	}
	raw, err := mime.BuildDraft(o, mime.MessageID(domain, "d"))
	if err != nil {
		return "", err
	}
	var id string
	err = p.withConn(func(c *imapclient.Client) error {
		box, err := p.folderRole(c, mailtypes.RoleDrafts)
		if err != nil {
			return err
		}
		if box == "" {
			box = "Drafts"
			if err := c.Create(box, nil).Wait(); err != nil {
				return err
			}
		}
		ap := c.Append(box, int64(len(raw)), &imap.AppendOptions{Flags: []imap.Flag{imap.FlagDraft, imap.FlagSeen}, Time: time.Now()})
		if _, err := ap.Write(raw); err != nil {
			return err
		}
		if err := ap.Close(); err != nil {
			return err
		}
		data, err := ap.Wait()
		if err != nil {
			return err
		}
		id = rid(box, data.UIDValidity, data.UID)
		return nil
	})
	if err != nil {
		return "", err
	}
	if replaceID != "" {
		_ = p.DeleteDraft(ctx, replaceID)
	}
	return id, nil
}

func (p *Provider) DeleteDraft(ctx context.Context, id string) error {
	return p.Apply(ctx, mailtypes.Action{Kind: mailtypes.ActDelete, IDs: []string{id}})
}

func (p *Provider) DraftID(ctx context.Context, messageID string) (string, error) {
	return messageID, nil
}

// xoauth2 is the SASL XOAUTH2 mechanism: the user and an access token.
type xoauth2 struct{ user, token string }

func (x xoauth2) Start() (string, []byte, error) {
	return "XOAUTH2", []byte("user=" + x.user + "\x01auth=Bearer " + x.token + "\x01\x01"), nil
}

// Next answers the error challenge a server sends on a bad token with an empty line, so it can say why.
func (x xoauth2) Next(challenge []byte) ([]byte, error) { return []byte{}, nil }

// friendlyLogin turns the servers' terse refusals into what a person can act on.
func friendlyLogin(err error, host string) error {
	m := strings.ToLower(err.Error())
	switch {
	case strings.Contains(m, "application-specific password") || strings.Contains(m, "app password") || strings.Contains(m, "apppasswords") || (strings.Contains(host, "gmail") && strings.Contains(m, "invalid credentials")):
		return fmt.Errorf("%v (this server needs an app password, not your normal password; create one in your account's security settings)", err)
	}
	return err
}
