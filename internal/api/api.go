// Package api is the JSON interface between the web UI and the Go core. The
// native window serves it in process; the headless daemon serves it on
// loopback behind a token. The UI never talks to a mail server itself.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/patrickryan01/halyard/internal/discover"
	"github.com/patrickryan01/halyard/internal/engine"
	mimeb "github.com/patrickryan01/halyard/internal/mime"
	"github.com/patrickryan01/halyard/internal/mailtypes"
	"github.com/patrickryan01/halyard/internal/oauth"
	"github.com/patrickryan01/halyard/internal/platform"
	"github.com/patrickryan01/halyard/internal/plugin"
	"github.com/patrickryan01/halyard/internal/providers/google"
	"github.com/patrickryan01/halyard/internal/providers/imap"
	"github.com/patrickryan01/halyard/internal/providers/microsoft"
	"github.com/patrickryan01/halyard/internal/store"
)

type Server struct {
	Eng      *engine.Engine
	Token    string // empty when served in process to the native window
	Version  string
	UI       http.Handler
	Discover *discover.Discoverer

	mu      sync.Mutex
	pending map[string]*pendingAuth
	events  []engine.Event
	evSeq   int
	evCond  *sync.Cond
}

type pendingAuth struct {
	p    *oauth.Pending
	cfg  oauth.Config
	kind string
	done chan authDone
}

type authDone struct {
	acct mailtypes.Account
	err  error
}

func New(e *engine.Engine, version string, ui http.Handler) *Server {
	s := &Server{Eng: e, Version: version, UI: ui, Discover: discover.New(), pending: map[string]*pendingAuth{}}
	s.evCond = sync.NewCond(&s.mu)
	ch, _ := e.Subscribe()
	go func() {
		for ev := range ch {
			s.mu.Lock()
			s.evSeq++
			s.events = append(s.events, ev)
			if len(s.events) > 200 {
				s.events = s.events[len(s.events)-200:]
			}
			s.mu.Unlock()
			s.evCond.Broadcast()
		}
	}()
	return s
}

// Events returns a feed for the native shell, which raises desktop
// notifications from it.
func (s *Server) OnEvent(f func(engine.Event)) {
	ch, _ := s.Eng.Subscribe()
	go func() {
		for ev := range ch {
			f(ev)
		}
	}()
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	h := func(path string, f func(http.ResponseWriter, *http.Request)) { mux.HandleFunc(path, s.guard(f)) }
	h("GET /api/state", s.state)
	h("POST /api/discover", s.discoverAcct)
	h("POST /api/accounts/password", s.addPassword)
	h("POST /api/accounts/oauth/start", s.oauthStart)
	h("GET /api/accounts/oauth/wait", s.oauthWait)
	h("POST /api/accounts/oauth/cancel", s.oauthCancel)
	h("POST /api/accounts/update", s.updateAccount)
	h("POST /api/accounts/remove", s.removeAccount)
	h("GET /api/threads", s.threads)
	h("GET /api/thread", s.thread)
	h("POST /api/act", s.act)
	h("POST /api/snooze", s.snooze)
	h("POST /api/send", s.send)
	h("POST /api/undo", s.undo)
	h("POST /api/outbox/retry", s.retry)
	h("GET /api/outbox", s.outbox)
	h("GET /api/attachment", s.attachment)
	h("GET /api/contacts", s.contacts)
	h("POST /api/settings", s.settings)
	h("GET /api/events", s.eventsPoll)
	h("POST /api/sync", func(w http.ResponseWriter, r *http.Request) {
		s.Eng.SyncNow("")
		writeJSON(w, map[string]bool{"ok": true})
	})
	h("POST /api/open", s.open)
	if s.UI != nil {
		mux.Handle("/", s.UI)
	}
	return mux
}

// guard checks the token (daemon mode) and that the request really is for
// loopback, which blocks DNS rebinding from a web page.
func (s *Server) guard(f func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Token != "" {
			host, _, _ := net.SplitHostPort(r.Host)
			if host != "127.0.0.1" && host != "localhost" {
				http.Error(w, "bad host", http.StatusForbidden)
				return
			}
			t := r.Header.Get("X-Token")
			if t == "" {
				t = r.URL.Query().Get("token")
			}
			if subtle.ConstantTimeCompare([]byte(t), []byte(s.Token)) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		f(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func decodeBody(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(v)
}

// ---- state ----

func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	accts, _ := s.Eng.St.Accounts()
	folders, _ := s.Eng.St.Folders("")
	st := s.Eng.St
	writeJSON(w, map[string]any{
		"version":  s.Version,
		"accounts": accts,
		"folders":  folders,
		"kinds":    plugin.Kinds(),
		"outbox":   outboxList(st),
		"settings": map[string]string{
			"theme":         st.Setting("theme", "auto"),
			"density":       st.Setting("density", "comfortable"),
			"loadImages":    st.Setting("loadImages", "false"),
			"notifications": st.Setting("notifications", "true"),
			"undoSeconds":   st.Setting("undoSeconds", "10"),
		},
		"oauth": map[string]bool{
			"google":    google.OAuthConfig(nil).ClientID != "",
			"microsoft": microsoft.OAuthConfig(nil).ClientID != "",
		},
	})
}

func outboxList(st *store.Store) []map[string]any {
	items, _ := st.Outbox()
	out := []map[string]any{}
	for _, it := range items {
		out = append(out, map[string]any{"id": it.ID, "accountId": it.AccountID, "subject": it.Subject, "state": it.State, "error": it.Error, "sendAt": it.SendAt.UnixMilli()})
	}
	return out
}

func (s *Server) outbox(w http.ResponseWriter, r *http.Request) { writeJSON(w, outboxList(s.Eng.St)) }

// ---- accounts ----

func (s *Server) discoverAcct(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email string }
	if err := decodeBody(r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	res, err := s.Discover.Find(ctx, in.Email)
	if err != nil {
		fail(w, 400, err)
		return
	}
	writeJSON(w, res)
}

type passwordIn struct {
	Kind         string `json:"kind"` // imap or jmap
	Email        string `json:"email"`
	Name         string `json:"name"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	IMAPHost     string `json:"imapHost"`
	IMAPPort     int    `json:"imapPort"`
	IMAPSecurity string `json:"imapSecurity"`
	SMTPHost     string `json:"smtpHost"`
	SMTPPort     int    `json:"smtpPort"`
	SMTPSecurity string `json:"smtpSecurity"`
	JMAPURL      string `json:"jmapUrl"`
	SkipVerify   bool   `json:"skipVerify"`
}

func (s *Server) addPassword(w http.ResponseWriter, r *http.Request) {
	var in passwordIn
	if err := decodeBody(r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	if in.Email == "" || in.Password == "" {
		fail(w, 400, fmt.Errorf("email and password are required"))
		return
	}
	set := map[string]string{}
	if in.Username != "" {
		set["username"] = in.Username
	}
	if in.SkipVerify {
		set["insecure_skip_verify"] = "true"
	}
	a := mailtypes.Account{Kind: in.Kind, Email: in.Email, Name: in.Name, Settings: set}
	switch in.Kind {
	case mailtypes.KindJMAP:
		if in.JMAPURL == "" {
			fail(w, 400, fmt.Errorf("server address is required"))
			return
		}
		set["jmap_url"] = in.JMAPURL
	default:
		a.Kind = mailtypes.KindIMAP
		if in.IMAPHost == "" {
			fail(w, 400, fmt.Errorf("server address is required"))
			return
		}
		set[imap.SHost], set[imap.SSecurity] = in.IMAPHost, orDef(in.IMAPSecurity, "ssl")
		if in.IMAPPort > 0 {
			set[imap.SPort] = strconv.Itoa(in.IMAPPort)
		}
		set[imap.SSMTPHost] = orDef(in.SMTPHost, in.IMAPHost)
		set[imap.SSMTPSec] = orDef(in.SMTPSecurity, "starttls")
		if in.SMTPPort > 0 {
			set[imap.SSMTPPort] = strconv.Itoa(in.SMTPPort)
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	acct, err := s.Eng.AddAccount(ctx, a, []byte(in.Password))
	if err != nil {
		fail(w, 400, err)
		return
	}
	writeJSON(w, acct)
}

func orDef(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

func (s *Server) oauthStart(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Kind         string `json:"kind"`
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
		Tenant       string `json:"tenant"`
	}
	if err := decodeBody(r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	set := map[string]string{}
	if in.ClientID != "" {
		set["client_id"], set["client_secret"] = in.ClientID, in.ClientSecret
	}
	if in.Tenant != "" {
		set["tenant"] = in.Tenant
	}
	var cfg oauth.Config
	switch in.Kind {
	case mailtypes.KindGoogle:
		cfg = google.OAuthConfig(set)
	case mailtypes.KindMicrosoft:
		cfg = microsoft.OAuthConfig(set)
	default:
		fail(w, 400, fmt.Errorf("unknown provider"))
		return
	}
	if cfg.ClientID == "" {
		fail(w, 400, fmt.Errorf("this build has no %s sign-in registered; enter a client ID under Advanced", in.Kind))
		return
	}
	p, err := oauth.Start(cfg)
	if err != nil {
		fail(w, 500, err)
		return
	}
	id := fmt.Sprint(time.Now().UnixNano())
	pa := &pendingAuth{p: p, cfg: cfg, kind: in.Kind, done: make(chan authDone, 1)}
	s.mu.Lock()
	s.pending[id] = pa
	s.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		tok, err := p.Wait(ctx, cfg)
		if err != nil {
			pa.done <- authDone{err: err}
			return
		}
		b, _ := json.Marshal(tok)
		a := mailtypes.Account{Kind: in.Kind, Settings: set}
		acct, err := s.Eng.AddAccount(ctx, a, b)
		pa.done <- authDone{acct: acct, err: err}
	}()
	_ = platform.OpenURL(p.URL)
	writeJSON(w, map[string]string{"id": id, "url": p.URL})
}

func (s *Server) oauthWait(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	s.mu.Lock()
	pa := s.pending[id]
	s.mu.Unlock()
	if pa == nil {
		fail(w, 404, fmt.Errorf("no sign-in in progress"))
		return
	}
	select {
	case d := <-pa.done:
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
		if d.err != nil {
			fail(w, 400, d.err)
			return
		}
		writeJSON(w, d.acct)
	case <-time.After(25 * time.Second):
		writeJSON(w, map[string]bool{"pending": true})
	case <-r.Context().Done():
	}
}

func (s *Server) oauthCancel(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	s.mu.Lock()
	pa := s.pending[id]
	delete(s.pending, id)
	s.mu.Unlock()
	if pa != nil {
		pa.p.Cancel()
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) updateAccount(w http.ResponseWriter, r *http.Request) {
	var in struct{ ID, Name, Signature, Color string }
	if err := decodeBody(r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	a, err := s.Eng.St.Account(in.ID)
	if err != nil {
		fail(w, 404, err)
		return
	}
	a.Name, a.Signature = in.Name, in.Signature
	if in.Color != "" {
		a.Color = in.Color
	}
	if err := s.Eng.St.PutAccount(a); err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, a)
}

func (s *Server) removeAccount(w http.ResponseWriter, r *http.Request) {
	var in struct{ ID string }
	if err := decodeBody(r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	if err := s.Eng.RemoveAccount(in.ID); err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// ---- mail ----

func (s *Server) threads(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	off, _ := strconv.Atoi(q.Get("offset"))
	lim, _ := strconv.Atoi(q.Get("limit"))
	text := q.Get("q")
	lq := store.ListQuery{AccountID: q.Get("account"), Folder: q.Get("folder"), Text: text, Unread: q.Get("unread") == "1", Offset: off, Limit: lim}
	// operators the FTS table cannot answer
	lq.Text, lq.Unread, lq.HasAttach, lq.Starred = stripOps(lq.Text, lq.Unread)
	th, err := s.Eng.St.ListThreads(lq)
	if err != nil {
		fail(w, 500, err)
		return
	}
	if th == nil {
		th = []mailtypes.Thread{}
	}
	writeJSON(w, th)
}

func stripOps(text string, unread bool) (string, bool, bool, bool) {
	var keep []string
	var att, star bool
	for _, w := range strings.Fields(text) {
		switch strings.ToLower(w) {
		case "is:unread":
			unread = true
		case "has:attachment":
			att = true
		case "is:starred":
			star = true
		default:
			keep = append(keep, w)
		}
	}
	return strings.Join(keep, " "), unread, att, star
}

func (s *Server) thread(w http.ResponseWriter, r *http.Request) {
	ms, err := s.Eng.St.Thread(r.URL.Query().Get("id"))
	if err != nil {
		fail(w, 500, err)
		return
	}
	if ms == nil {
		ms = []mailtypes.Message{}
	}
	writeJSON(w, ms)
}

type actIn struct {
	Kind      string   `json:"kind"`
	ThreadIDs []string `json:"threadIds"`
	FolderID  string   `json:"folderId"`
	AccountID string   `json:"accountId"`
}

// act applies an action to whole conversations: every message in each thread.
func (s *Server) act(w http.ResponseWriter, r *http.Request) {
	var in actIn
	if err := decodeBody(r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	byAcct := map[string][]string{}
	for _, tid := range in.ThreadIDs {
		ms, err := s.Eng.St.Thread(tid)
		if err != nil {
			fail(w, 500, err)
			return
		}
		for _, m := range ms {
			// do not re-apply what is already so
			switch in.Kind {
			case mailtypes.ActRead:
				if m.Read {
					continue
				}
			case mailtypes.ActUnread:
				if !m.Read {
					continue
				}
			}
			byAcct[m.AccountID] = append(byAcct[m.AccountID], m.RemoteID)
		}
	}
	for acct, ids := range byAcct {
		if in.FolderID != "" && in.AccountID != "" && in.AccountID != acct {
			continue
		}
		if err := s.Eng.Act(acct, mailtypes.Action{Kind: in.Kind, IDs: ids, FolderID: in.FolderID}); err != nil {
			fail(w, 500, err)
			return
		}
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) snooze(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ThreadIDs []string `json:"threadIds"`
		Until     int64    `json:"until"` // unix seconds, 0 clears
	}
	if err := decodeBody(r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	for _, t := range in.ThreadIDs {
		var err error
		if in.Until == 0 {
			err = s.Eng.St.Unsnooze(t)
		} else {
			err = s.Eng.St.Snooze(t, time.Unix(in.Until, 0))
		}
		if err != nil {
			fail(w, 500, err)
			return
		}
	}
	writeJSON(w, map[string]bool{"ok": true})
}

type sendIn struct {
	AccountID  string `json:"accountId"`
	To         string `json:"to"`
	Cc         string `json:"cc"`
	Bcc        string `json:"bcc"`
	Subject    string `json:"subject"`
	BodyText   string `json:"bodyText"`
	BodyHTML   string `json:"bodyHtml"`
	InReplyTo  string `json:"inReplyTo"`
	References string `json:"references"`
	ThreadHint string `json:"threadHint"`
}

// send takes a multipart form: a "json" field and any number of "file" parts.
func (s *Server) send(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		fail(w, 400, err)
		return
	}
	var in sendIn
	if err := json.Unmarshal([]byte(r.FormValue("json")), &in); err != nil {
		fail(w, 400, err)
		return
	}
	o := mailtypes.Outgoing{AccountID: in.AccountID, To: mimeb.ParseList(in.To), Cc: mimeb.ParseList(in.Cc), Bcc: mimeb.ParseList(in.Bcc), Subject: in.Subject,
		BodyText: in.BodyText, BodyHTML: in.BodyHTML, InReplyTo: in.InReplyTo, References: strings.Fields(in.References), ThreadHint: in.ThreadHint}
	if r.MultipartForm != nil {
		for _, fh := range r.MultipartForm.File["file"] {
			f, err := fh.Open()
			if err != nil {
				fail(w, 400, err)
				return
			}
			b, err := io.ReadAll(f)
			f.Close()
			if err != nil {
				fail(w, 400, err)
				return
			}
			o.Attachments = append(o.Attachments, mailtypes.OutAttachment{Filename: fh.Filename, ContentType: fh.Header.Get("Content-Type"), Data: b})
		}
	}
	if n, err := strconv.Atoi(s.Eng.St.Setting("undoSeconds", "10")); err == nil && n >= 0 && n <= 30 {
		s.Eng.UndoDelay = time.Duration(n) * time.Second
	}
	id, err := s.Eng.Send(o)
	if err != nil {
		fail(w, 400, err)
		return
	}
	writeJSON(w, map[string]any{"id": id, "undoMs": s.Eng.UndoDelay.Milliseconds()})
}

func (s *Server) undo(w http.ResponseWriter, r *http.Request) {
	var in struct{ ID string }
	if err := decodeBody(r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	ok, err := s.Eng.Undo(in.ID)
	if err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, map[string]bool{"undone": ok})
}

func (s *Server) retry(w http.ResponseWriter, r *http.Request) {
	var in struct{ ID string }
	if err := decodeBody(r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	if err := s.Eng.RetryOutbox(in.ID); err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) attachment(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	b, ct, err := s.Eng.Attachment(r.Context(), q.Get("account"), q.Get("msg"), q.Get("att"))
	if err != nil {
		fail(w, 502, err)
		return
	}
	name := q.Get("name")
	if q.Get("ct") != "" {
		ct = q.Get("ct")
	}
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	disp := "attachment"
	if q.Get("inline") == "1" && (strings.HasPrefix(ct, "image/") || ct == "application/pdf") {
		disp = "inline"
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disp, map[string]string{"filename": name}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(b)
}

func (s *Server) contacts(w http.ResponseWriter, r *http.Request) {
	cs := s.Eng.Contacts(r.Context(), r.URL.Query().Get("q"))
	if cs == nil {
		cs = []mailtypes.Contact{}
	}
	writeJSON(w, cs)
}

func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	var in map[string]string
	if err := decodeBody(r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	allowed := map[string]bool{"theme": true, "density": true, "loadImages": true, "notifications": true, "undoSeconds": true}
	for k, v := range in {
		if !allowed[k] {
			continue
		}
		if err := s.Eng.St.SetSetting(k, v); err != nil {
			fail(w, 500, err)
			return
		}
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// eventsPoll long polls: it returns events after ?since, or an empty list
// after 25 seconds. Long polling works through any transport, including the
// native window's in-process one.
func (s *Server) eventsPoll(w http.ResponseWriter, r *http.Request) {
	since, _ := strconv.Atoi(r.URL.Query().Get("since"))
	deadline := time.Now().Add(25 * time.Second)
	s.mu.Lock()
	defer s.mu.Unlock()
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-r.Context().Done():
			s.evCond.Broadcast()
		case <-stop:
		case <-time.After(time.Until(deadline)):
			s.evCond.Broadcast()
		}
	}()
	for s.evSeq <= since && time.Now().Before(deadline) && r.Context().Err() == nil {
		s.evCond.Wait()
	}
	out := []engine.Event{}
	if s.evSeq > since {
		n := s.evSeq - since
		if n > len(s.events) {
			n = len(s.events)
		}
		out = append(out, s.events[len(s.events)-n:]...)
	}
	writeJSON(w, map[string]any{"seq": s.evSeq, "events": out})
}

// open hands a link to the system browser: the message viewer cannot navigate
// away from the app, so link clicks come here. Only http, https and mailto.
func (s *Server) open(w http.ResponseWriter, r *http.Request) {
	var in struct{ URL string }
	if err := decodeBody(r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	u, err := url.Parse(in.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "mailto") {
		fail(w, 400, fmt.Errorf("only web and mail links can be opened"))
		return
	}
	if err := platform.OpenURL(in.URL); err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
