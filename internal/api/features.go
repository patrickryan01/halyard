package api

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/patrickryan01/halyard/internal/engine"
	"github.com/patrickryan01/halyard/internal/mailtypes"
)

// call wraps a handler that takes a JSON body of type T and returns a value to send back.
func call[T any](s *Server, f func(ctx context.Context, in T) (any, error)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var in T
		if r.Method != http.MethodGet && r.ContentLength != 0 {
			if err := decodeBody(r, &in); err != nil {
				fail(w, 400, err)
				return
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		out, err := f(ctx, in)
		if err != nil {
			fail(w, 400, err)
			return
		}
		if out == nil {
			out = map[string]bool{"ok": true}
		}
		writeJSON(w, out)
	}
}

// view wraps a GET handler that reads query parameters.
func view(s *Server, f func(ctx context.Context, q func(string) string) (any, error)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		out, err := f(ctx, r.URL.Query().Get)
		if err != nil {
			fail(w, 400, err)
			return
		}
		writeJSON(w, out)
	}
}

func file(w http.ResponseWriter, name, ct string, b []byte) {
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(b)
}

type idIn struct {
	ID string `json:"id"`
}

type acctIn struct {
	AccountID string `json:"accountId"`
}

func (s *Server) features(h func(string, func(http.ResponseWriter, *http.Request))) {
	e := s.Eng
	// rules
	h("GET /api/rules", view(s, func(ctx context.Context, q func(string) string) (any, error) { return e.St.Rules() }))
	h("POST /api/rules", call(s, func(ctx context.Context, in mailtypes.Rule) (any, error) { return e.SaveRule(in) }))
	h("POST /api/rules/delete", call(s, func(ctx context.Context, in idIn) (any, error) { return nil, e.St.DeleteRule(in.ID) }))
	h("POST /api/rules/move", call(s, func(ctx context.Context, in struct {
		ID    string `json:"id"`
		Index int    `json:"index"`
	}) (any, error) {
		return nil, e.St.MoveRule(in.ID, in.Index)
	}))
	h("POST /api/rules/run", call(s, func(ctx context.Context, in acctIn) (any, error) {
		n, err := e.RunRulesNow(in.AccountID)
		return map[string]int{"matched": n}, err
	}))
	// templates and saved searches
	h("GET /api/templates", view(s, func(ctx context.Context, q func(string) string) (any, error) { return e.St.Templates() }))
	h("POST /api/templates", call(s, func(ctx context.Context, in mailtypes.Template) (any, error) { return e.St.PutTemplate(in) }))
	h("POST /api/templates/delete", call(s, func(ctx context.Context, in idIn) (any, error) { return nil, e.St.DeleteTemplate(in.ID) }))
	h("GET /api/searches", view(s, func(ctx context.Context, q func(string) string) (any, error) { return e.St.SavedSearches() }))
	h("POST /api/searches", call(s, func(ctx context.Context, in mailtypes.SavedSearch) (any, error) { return e.St.PutSavedSearch(in) }))
	h("POST /api/searches/delete", call(s, func(ctx context.Context, in idIn) (any, error) { return nil, e.St.DeleteSavedSearch(in.ID) }))
	// identities
	h("GET /api/identities", view(s, func(ctx context.Context, q func(string) string) (any, error) { return e.St.Identities(q("account")) }))
	h("POST /api/identities", call(s, func(ctx context.Context, in mailtypes.Identity) (any, error) { return e.St.PutIdentity(in) }))
	h("POST /api/identities/delete", call(s, func(ctx context.Context, in idIn) (any, error) { return nil, e.St.DeleteIdentity(in.ID) }))
	h("POST /api/identities/refresh", call(s, func(ctx context.Context, in acctIn) (any, error) {
		if err := e.RefreshIdentities(ctx, in.AccountID); err != nil {
			return nil, err
		}
		return e.St.Identities(in.AccountID)
	}))
	// vacation
	h("GET /api/vacation", view(s, func(ctx context.Context, q func(string) string) (any, error) { return e.GetVacation(ctx, q("account")) }))
	h("POST /api/vacation", call(s, func(ctx context.Context, in struct {
		AccountID string `json:"accountId"`
		mailtypes.Vacation
	}) (any, error) {
		return nil, e.SetVacation(ctx, in.AccountID, in.Vacation)
	}))
	// invitations
	h("GET /api/invite", view(s, func(ctx context.Context, q func(string) string) (any, error) { return e.Invite(ctx, q("id")) }))
	h("POST /api/invite/respond", call(s, func(ctx context.Context, in struct{ ID, Status string }) (any, error) {
		id, err := e.RespondInvite(ctx, in.ID, in.Status)
		return map[string]string{"id": id}, err
	}))
	h("POST /api/invite/add", call(s, func(ctx context.Context, in struct{ ID, AccountID string }) (any, error) {
		return e.AddInviteToCalendar(ctx, in.ID, in.AccountID)
	}))
	// unsubscribe and read receipts
	h("POST /api/unsubscribe", call(s, func(ctx context.Context, in idIn) (any, error) { return e.Unsubscribe(ctx, in.ID) }))
	h("POST /api/receipt", call(s, func(ctx context.Context, in idIn) (any, error) {
		id, err := e.SendReceipt(ctx, in.ID)
		return map[string]string{"id": id}, err
	}))
	// folders
	h("POST /api/folders/rename", call(s, func(ctx context.Context, in struct{ AccountID, RemoteID, Name string }) (any, error) {
		return nil, e.RenameFolder(ctx, in.AccountID, in.RemoteID, in.Name)
	}))
	h("POST /api/folders/delete", call(s, func(ctx context.Context, in struct{ AccountID, RemoteID string }) (any, error) {
		return nil, e.DeleteFolder(ctx, in.AccountID, in.RemoteID)
	}))
	h("POST /api/folders/markread", call(s, func(ctx context.Context, in struct{ AccountID, RemoteID string }) (any, error) {
		n, err := e.MarkAllRead(in.AccountID, in.RemoteID)
		return map[string]int{"count": n}, err
	}))
	h("POST /api/folders/empty", call(s, func(ctx context.Context, in struct{ AccountID, Role string }) (any, error) {
		n, err := e.EmptyFolder(in.AccountID, in.Role)
		return map[string]int{"count": n}, err
	}))
	// export, source, import
	h("GET /api/export/eml", func(w http.ResponseWriter, r *http.Request) {
		b, name, err := e.ExportEML(r.Context(), r.URL.Query().Get("id"))
		if err != nil {
			fail(w, 400, err)
			return
		}
		file(w, name, "message/rfc822", b)
	})
	h("GET /api/export/mbox", func(w http.ResponseWriter, r *http.Request) {
		ids := strings.Split(r.URL.Query().Get("threads"), ",")
		b, err := e.ExportMbox(r.Context(), ids)
		if err != nil {
			fail(w, 400, err)
			return
		}
		file(w, "mail.mbox", "application/mbox", b)
	})
	h("GET /api/source", func(w http.ResponseWriter, r *http.Request) {
		raw, _, err := e.RawMessage(r.Context(), r.URL.Query().Get("id"))
		if err != nil {
			fail(w, 400, err)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(raw)
	})
	h("POST /api/import/mbox", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(64 << 20); err != nil {
			fail(w, 400, err)
			return
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			fail(w, 400, fmt.Errorf("choose an mbox file"))
			return
		}
		defer f.Close()
		res, err := e.ImportMbox(r.Context(), r.FormValue("folder"), f)
		if err != nil {
			fail(w, 400, err)
			return
		}
		writeJSON(w, res)
	})
	h("POST /api/import/thunderbird", call(s, func(ctx context.Context, in struct{ Path string }) (any, error) {
		return e.ImportThunderbird(ctx, in.Path)
	}))
	// keys, signing and encryption
	h("GET /api/keys", view(s, func(ctx context.Context, q func(string) string) (any, error) { return e.Keyring() }))
	h("POST /api/keys/make", call(s, func(ctx context.Context, in struct{ Kind, Name, Email string }) (any, error) {
		return nil, e.MakeKey(in.Kind, in.Name, in.Email)
	}))
	h("POST /api/keys/import", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(16 << 20); err != nil {
			fail(w, 400, err)
			return
		}
		var data []byte
		if f, _, err := r.FormFile("file"); err == nil {
			data, _ = io.ReadAll(io.LimitReader(f, 4<<20))
			f.Close()
		} else {
			data = []byte(r.FormValue("text"))
		}
		n, err := e.ImportKey(r.FormValue("kind"), data, r.FormValue("password"))
		if err != nil {
			fail(w, 400, err)
			return
		}
		writeJSON(w, map[string]int{"imported": n})
	})
	h("POST /api/keys/remove", call(s, func(ctx context.Context, in idIn) (any, error) { return nil, e.RemoveKey(in.ID) }))
	h("GET /api/keys/export", func(w http.ResponseWriter, r *http.Request) {
		txt, name, err := e.ExportKey(r.URL.Query().Get("id"))
		if err != nil {
			fail(w, 400, err)
			return
		}
		file(w, name, "text/plain", []byte(txt))
	})
	h("GET /api/security", view(s, func(ctx context.Context, q func(string) string) (any, error) { return e.Security(ctx, q("id")) }))
	h("GET /api/security/attachment", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		b, ct, err := e.SecurityAttachment(r.Context(), q.Get("id"), q.Get("att"))
		if err != nil {
			fail(w, 400, err)
			return
		}
		if ct == "" {
			ct = "application/octet-stream"
		}
		file(w, q.Get("name"), ct, b)
	})
	h("POST /api/security/keep", call(s, func(ctx context.Context, in idIn) (any, error) { return nil, e.KeepSigner(ctx, in.ID) }))
	// calendars and contacts servers
	h("POST /api/dav/set", call(s, func(ctx context.Context, in struct{ AccountID, URL, User, Password string }) (any, error) {
		return nil, e.SetDAV(ctx, in.AccountID, in.URL, in.User, in.Password)
	}))
	h("GET /api/calendars", view(s, func(ctx context.Context, q func(string) string) (any, error) { return e.Calendars(ctx, q("account")) }))
	h("GET /api/calendar/events", view(s, func(ctx context.Context, q func(string) string) (any, error) {
		from, err := time.Parse(time.RFC3339, q("from"))
		if err != nil {
			from = time.Now().AddDate(0, 0, -7)
		}
		to, err := time.Parse(time.RFC3339, q("to"))
		if err != nil {
			to = from.AddDate(0, 0, 45)
		}
		return e.Events(ctx, q("account"), from, to)
	}))
	h("POST /api/calendar/save", call(s, func(ctx context.Context, in struct {
		AccountID string          `json:"accountId"`
		Calendar  string          `json:"calendar"`
		Event     engine.CalEvent `json:"event"`
	}) (any, error) {
		return e.SaveEvent(ctx, in.AccountID, in.Calendar, in.Event)
	}))
	h("POST /api/calendar/delete", call(s, func(ctx context.Context, in struct{ AccountID, URL, ETag string }) (any, error) {
		return nil, e.DeleteEvent(ctx, in.AccountID, in.URL, in.ETag)
	}))
	h("GET /api/contacts/all", view(s, func(ctx context.Context, q func(string) string) (any, error) {
		out, _ := e.St.SuggestContacts(q("q"), 500)
		loc, _ := e.St.LocalContacts(q("q"))
		out = append(out, loc...)
		if out == nil {
			out = []mailtypes.Contact{}
		}
		return out, nil
	}))
	h("GET /api/addressbook", view(s, func(ctx context.Context, q func(string) string) (any, error) { return e.AddressBook(ctx, q("account")) }))
	h("POST /api/contacts/save", call(s, func(ctx context.Context, in struct {
		AccountID string `json:"accountId"`
		mailtypes.Contact
	}) (any, error) {
		if in.Contact.ID == "" {
			return e.CreateContact(ctx, in.AccountID, in.Contact)
		}
		return e.UpdateContact(ctx, in.AccountID, in.Contact)
	}))
	h("POST /api/contacts/delete", call(s, func(ctx context.Context, in struct{ AccountID, ID string }) (any, error) {
		return nil, e.DeleteContact(ctx, in.AccountID, in.ID)
	}))
	// server side filters (Sieve over ManageSieve)
	h("GET /api/sieve/scripts", view(s, func(ctx context.Context, q func(string) string) (any, error) {
		return e.SieveScripts(ctx, q("account"))
	}))
	h("GET /api/sieve/script", view(s, func(ctx context.Context, q func(string) string) (any, error) {
		t, err := e.SieveScript(ctx, q("account"), q("name"))
		return map[string]string{"name": q("name"), "script": t}, err
	}))
	h("POST /api/sieve/put", call(s, func(ctx context.Context, in struct {
		AccountID, Name, Script string
		Activate                bool
	}) (any, error) {
		return nil, e.SievePut(ctx, in.AccountID, in.Name, in.Script, in.Activate)
	}))
	h("POST /api/sieve/check", call(s, func(ctx context.Context, in struct{ AccountID, Script string }) (any, error) {
		return nil, e.SieveCheck(ctx, in.AccountID, in.Script)
	}))
	h("POST /api/sieve/activate", call(s, func(ctx context.Context, in struct{ AccountID, Name string }) (any, error) {
		return nil, e.SieveActivate(ctx, in.AccountID, in.Name)
	}))
	h("POST /api/sieve/delete", call(s, func(ctx context.Context, in struct{ AccountID, Name string }) (any, error) {
		return nil, e.SieveDelete(ctx, in.AccountID, in.Name)
	}))
	// the one place the window asks what it can do
	h("GET /api/capabilities", view(s, func(ctx context.Context, q func(string) string) (any, error) {
		a, err := e.St.Account(q("account"))
		if err != nil {
			return nil, err
		}
		caps := map[string]bool{"identities": true, "rules": true, "dav": a.Kind != "google" && a.Kind != "microsoft" && a.Kind != "local"}
		switch a.Kind {
		case "google", "microsoft", "jmap":
			caps["vacation"], caps["folders"] = true, true
		case "imap":
			caps["vacation"], caps["sieve"], caps["folders"] = true, true, true
		case "pop3", "local":
		}
		return caps, nil
	}))
}
