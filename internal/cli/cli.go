// Package cli is the command line for the mail client: the same things the window does, from a terminal or a script.
// It talks to the running app through its MCP server, or, when the app is not running, starts the core itself for the
// length of the command.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/patrickryan01/halyard/internal/app"
	"github.com/patrickryan01/halyard/internal/brand"
	"github.com/patrickryan01/halyard/internal/mcpserver"
)

// Commands lists every command with its one line help, in the order help shows them. The completeness checks read it.
var Commands = []Command{
	{"accounts", "", "list the accounts"},
	{"folders", "[account]", "list folders and labels with unread counts"},
	{"list", "[folder] [-n 25] [--unread]", "list conversations (folder: inbox, sent, drafts, archive, trash, spam, starred, snoozed)"},
	{"search", "<query> [-n 25]", "search with operators: from: to: subject: filename: label: is:unread has:attachment before: after: larger: and more"},
	{"read", "<thread id>", "read a conversation"},
	{"send", "--to a@b [--cc] [--bcc] --subject s (--body text | --body-file f) [--attach f] [--account id] [--from addr] [--at time] [--receipt] [--sign|--encrypt --protect pgp|smime]", "send a message, or schedule it with --at"},
	{"reply", "<message id> --body text [--all]", "reply to a message"},
	{"forward", "<message id> --to a@b [--body text]", "forward a message"},
	{"act", "<archive|trash|untrash|spam|read|unread|star|unstar|delete> <thread id>...", "act on conversations"},
	{"move", "<account id> <folder id> <thread id>...", "move conversations to a folder"},
	{"snooze", "<until RFC3339|off> <thread id>...", "hide conversations until a time"},
	{"outbox", "[cancel <id>|retry <id>]", "show what is waiting to send, or cancel one"},
	{"drafts", "", "list drafts"},
	{"folder", "<create|rename|delete|markread|empty> ...", "manage folders: create <account> <name>, rename <account> <id> <name>, delete <account> <id>, markread <account> <id>, empty <account> <trash|spam>"},
	{"rules", "[list|add|delete|run] ...", "filter rules: add --name n --if field:op:value --then move:folderId|read|star|archive|trash|spam|forward:addr"},
	{"templates", "[list|add|delete] ...", "canned replies: add --name n --body text [--subject s]"},
	{"searches", "[list|add|delete] ...", "saved searches: add --name n --query q"},
	{"identities", "[list|refresh|add|delete] ...", "addresses to send from: add --account id --email a@b [--name n] [--signature s] [--default]"},
	{"vacation", "<get|on|off> --account id [--subject s] [--body text] [--from t] [--to t]", "the automatic reply on the server"},
	{"invite", "<show|accept|decline|tentative|calendar> <message id>", "read and answer a calendar invitation; calendar saves it"},
	{"unsubscribe", "<message id>", "leave a mailing list"},
	{"receipt", "<message id>", "answer a read receipt request"},
	{"import", "<mbox file [--folder name]|thunderbird dir>", "import mail from an mbox file or a Thunderbird profile"},
	{"export", "<eml message-id file|mbox file thread-id...>", "save mail as .eml or mbox"},
	{"source", "<message id>", "show a message exactly as sent"},
	{"keys", "[list|make|import|remove|export] ...", "OpenPGP keys and S/MIME certificates: make --kind pgp|smime --email a@b [--name n]"},
	{"verify", "<message id>", "check a signature and decrypt a protected message"},
	{"calendar", "<set|list|events|add|delete> ...", "calendar server: set --account id --url u --user n --password p; events --account id [--days 30]; add --account id --title t --start t --end t"},
	{"contacts", "[query]", "search contacts"},
	{"groups", "[list|save|delete] ...", "contact groups: save --name n --members a@b,c@d [--account id]; send to a group by using its name as the recipient"},
	{"sieve", "<list|get|put|check|activate|delete> --account id ...", "server side filter scripts: put --name n --file script.sieve [--activate]"},
	{"update", "", "check for a newer release (Windows); other builds are updated by the package manager"},
	{"sync", "[account id]", "check for new mail now"},
	{"settings", "[key value]...", "show or change settings"},
	{"mcp", "", "show how to connect an MCP client"},
	{"version", "", "print the version"},
}

type Command struct{ Name, Usage, Help string }

// IsCommand says whether the first argument names a command, so the desktop program can pass control here.
func IsCommand(arg string) bool {
	if arg == "help" || arg == "--help" || arg == "-h" {
		return true
	}
	for _, c := range Commands {
		if c.Name == arg {
			return true
		}
	}
	return false
}

// Session is a connection to the core.
type Session struct {
	cs    *mcp.ClientSession
	close func()
}

type bearer struct{ tok string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.tok)
	return http.DefaultTransport.RoundTrip(r)
}

// Connect finds the running app through mcp.json, or starts a core of its own.
func Connect(ctx context.Context, dir string) (*Session, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: brand.Slug + "-cli", Version: brand.Version}, nil)
	if b, err := os.ReadFile(filepath.Join(dir, "mcp.json")); err == nil && os.Getenv("HALYARD_CLI_STANDALONE") == "" {
		var info mcpserver.Info
		if json.Unmarshal(b, &info) == nil && info.URL != "" {
			cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			cs, err := client.Connect(cctx, &mcp.StreamableClientTransport{Endpoint: info.URL, HTTPClient: &http.Client{Transport: bearer{info.Token}}}, nil)
			cancel()
			if err == nil {
				return &Session{cs: cs, close: func() { cs.Close() }}, nil
			}
		}
	}
	os.Setenv("HALYARD_NO_MCP_HTTP", "1")
	a, err := app.Start(dir, "")
	if err != nil {
		return nil, err
	}
	ct, st := mcp.NewInMemoryTransports()
	ss, err := a.MCPServer.Connect(ctx, st, nil)
	if err != nil {
		a.Close()
		return nil, err
	}
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		ss.Close()
		a.Close()
		return nil, err
	}
	return &Session{cs: cs, close: func() { cs.Close(); ss.Close(); a.Close() }}, nil
}

func (s *Session) Close() { s.close() }

// Call runs a tool and returns its JSON object.
func (s *Session) Call(ctx context.Context, name string, args map[string]any) (map[string]any, error) {
	res, err := s.cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return nil, err
	}
	if res.IsError {
		msg := "failed"
		for _, c := range res.Content {
			if t, ok := c.(*mcp.TextContent); ok {
				msg = t.Text
			}
		}
		return nil, errors.New(msg)
	}
	var out map[string]any
	if res.StructuredContent != nil {
		b, _ := json.Marshal(res.StructuredContent)
		_ = json.Unmarshal(b, &out)
	}
	if out == nil {
		for _, c := range res.Content {
			if t, ok := c.(*mcp.TextContent); ok {
				_ = json.Unmarshal([]byte(t.Text), &out)
			}
		}
	}
	return out, nil
}

// flags splits arguments into positional ones and --name value / --flag options. A name may repeat.
type flags struct {
	pos  []string
	opt  map[string][]string
	bool map[string]bool
}

var boolFlags = map[string]bool{"unread": true, "all": true, "receipt": true, "sign": true, "encrypt": true, "default": true, "activate": true, "json": true, "now": true}

func parse(args []string) flags {
	f := flags{opt: map[string][]string{}, bool: map[string]bool{}}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "--") || (a == "-n") {
			k := strings.TrimLeft(a, "-")
			if k == "n" {
				k = "limit"
			}
			if boolFlags[k] || i+1 >= len(args) {
				f.bool[k] = true
				continue
			}
			f.opt[k] = append(f.opt[k], args[i+1])
			i++
			continue
		}
		f.pos = append(f.pos, a)
	}
	return f
}

func (f flags) str(k string) string {
	if v := f.opt[k]; len(v) > 0 {
		return v[len(v)-1]
	}
	return ""
}

func (f flags) num(k string, def int) int {
	var n int
	if _, err := fmt.Sscan(f.str(k), &n); err != nil || n <= 0 {
		return def
	}
	return n
}

// Main runs one command and returns the exit code.
func Main(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintf(stdout, "%s: mail from the command line\n\nUsage: %s <command> [options]   (add --json for raw output)\n\n", brand.Name, brand.Slug)
		for _, c := range Commands {
			fmt.Fprintf(stdout, "  %-12s %s\n", c.Name, c.Help)
			if c.Usage != "" {
				fmt.Fprintf(stdout, "  %-12s   %s %s\n", "", c.Name, c.Usage)
			}
		}
		return 0
	}
	if args[0] == "version" {
		fmt.Fprintf(stdout, "%s %s\n", brand.Name, brand.Version)
		return 0
	}
	dir, err := app.DataDir()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	s, err := Connect(ctx, dir)
	if err != nil {
		fmt.Fprintln(stderr, "could not open the mail core:", err)
		return 1
	}
	defer s.Close()
	f := parse(args[1:])
	r := &runner{s: s, ctx: ctx, f: f, out: stdout, json: f.bool["json"]}
	if err := r.run(args[0]); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

type runner struct {
	s    *Session
	ctx  context.Context
	f    flags
	out  io.Writer
	json bool
}

func (r *runner) call(name string, args map[string]any) (map[string]any, error) {
	return r.s.Call(r.ctx, name, args)
}

func (r *runner) emit(v any, human func()) {
	if r.json {
		b, _ := json.MarshalIndent(v, "", "  ")
		fmt.Fprintln(r.out, string(b))
		return
	}
	human()
}

func (r *runner) need(n int, usage string) error {
	if len(r.f.pos) < n {
		return errors.New("usage: " + usage)
	}
	return nil
}

func list(v any) []map[string]any {
	var out []map[string]any
	if l, ok := v.([]any); ok {
		for _, x := range l {
			if m, ok := x.(map[string]any); ok {
				out = append(out, m)
			}
		}
	}
	return out
}

func s(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		if x == float64(int64(x)) {
			return fmt.Sprint(int64(x))
		}
		return fmt.Sprint(x)
	default:
		b, _ := json.Marshal(x)
		return string(b)
	}
}

func (r *runner) run(cmd string) error {
	f := r.f
	switch cmd {
	case "accounts":
		o, err := r.call("list_accounts", nil)
		if err != nil {
			return err
		}
		r.emit(o, func() {
			for _, a := range list(o["accounts"]) {
				fmt.Fprintf(r.out, "%s  %-10s %s  %s\n", s(a["id"]), s(a["kind"]), s(a["email"]), s(a["lastError"]))
			}
		})
	case "folders":
		o, err := r.call("list_folders", map[string]any{"accountId": strings.Join(f.pos, "")})
		if err != nil {
			return err
		}
		r.emit(o, func() {
			for _, x := range list(o["folders"]) {
				fmt.Fprintf(r.out, "%-8s %-24s %-8s unread %-4s account %s  id %s\n", s(x["role"]), s(x["name"]), "", s(x["unread"]), s(x["accountId"]), s(x["remoteId"]))
			}
		})
	case "list", "search":
		args := map[string]any{"limit": f.num("limit", 25)}
		if cmd == "search" {
			if err := r.need(1, "search <query>"); err != nil {
				return err
			}
			args["query"] = strings.Join(f.pos, " ")
		} else if len(f.pos) > 0 {
			fo := f.pos[0]
			if fo == "starred" || fo == "snoozed" || strings.Contains(fo, "/") {
				args["folder"] = fo
			} else {
				args["folder"] = "role:" + fo
			}
		}
		if f.bool["unread"] {
			args["unreadOnly"] = true
		}
		if a := f.str("account"); a != "" {
			args["accountId"] = a
		}
		tool := "list_threads"
		if cmd == "search" {
			tool = "search_threads"
		}
		o, err := r.call(tool, args)
		if err != nil {
			return err
		}
		r.emit(o, func() {
			for _, t := range list(o["threads"]) {
				mark := " "
				if n, _ := t["unread"].(float64); n > 0 {
					mark = "*"
				}
				fmt.Fprintf(r.out, "%s %-34s  %-28.28s  %.60s  (%s)\n", mark, s(t["id"]), strings.Trim(s(t["participants"]), `[]"`), s(t["subject"]), s(t["date"]))
			}
		})
	case "read":
		if err := r.need(1, "read <thread id>"); err != nil {
			return err
		}
		o, err := r.call("get_thread", map[string]any{"threadId": f.pos[0]})
		if err != nil {
			return err
		}
		r.emit(o, func() {
			for _, m := range list(o["messages"]) {
				from, _ := m["from"].(map[string]any)
				fmt.Fprintf(r.out, "From: %s <%s>\nDate: %s\nSubject: %s\nMessage: %s\n", s(from["name"]), s(from["email"]), s(m["date"]), s(m["subject"]), s(m["id"]))
				for _, a := range list(m["attachments"]) {
					fmt.Fprintf(r.out, "Attachment: %s (%s bytes) id %s\n", s(a["filename"]), s(a["size"]), s(a["id"]))
				}
				fmt.Fprintf(r.out, "\n%s\n\n----\n", s(m["bodyText"]))
			}
		})
	case "send":
		return r.send()
	case "reply", "forward":
		if err := r.need(1, cmd+" <message id> ..."); err != nil {
			return err
		}
		args := map[string]any{"messageId": f.pos[0], "bodyText": r.body(), "immediate": true}
		if f.bool["all"] {
			args["replyAll"] = true
		}
		tool := "reply_message"
		if cmd == "forward" {
			tool = "forward_message"
			args["to"] = f.opt["to"]
		}
		o, err := r.call(tool, args)
		if err != nil {
			return err
		}
		return r.waitSent(o)
	case "act":
		if err := r.need(2, "act <action> <thread id>..."); err != nil {
			return err
		}
		o, err := r.call("thread_action", map[string]any{"kind": f.pos[0], "threadIds": f.pos[1:]})
		if err != nil {
			return err
		}
		r.emit(o, func() { fmt.Fprintf(r.out, "%s: %s conversation(s)\n", f.pos[0], s(o["threads"])) })
	case "move":
		if err := r.need(3, "move <account id> <folder id> <thread id>..."); err != nil {
			return err
		}
		_, err := r.call("move_threads", map[string]any{"accountId": f.pos[0], "folderId": f.pos[1], "threadIds": f.pos[2:]})
		return err
	case "snooze":
		if err := r.need(2, "snooze <until|off> <thread id>..."); err != nil {
			return err
		}
		args := map[string]any{"threadIds": f.pos[1:]}
		if f.pos[0] != "off" {
			args["until"] = f.pos[0]
		}
		_, err := r.call("snooze_threads", args)
		return err
	case "outbox":
		if len(f.pos) >= 2 && (f.pos[0] == "cancel" || f.pos[0] == "retry") {
			tool, arg := "cancel_send", "outboxId"
			if f.pos[0] == "retry" {
				tool = "retry_send"
			}
			o, err := r.call(tool, map[string]any{arg: f.pos[1]})
			if err != nil {
				return err
			}
			r.emit(o, func() { fmt.Fprintln(r.out, s(o)) })
			return nil
		}
		o, err := r.call("list_outbox", nil)
		if err != nil {
			return err
		}
		r.emit(o, func() {
			for _, x := range list(o["outbox"]) {
				fmt.Fprintf(r.out, "%s  %-8s %s %s\n", s(x["id"]), s(x["state"]), s(x["subject"]), s(x["error"]))
			}
		})
	case "drafts":
		o, err := r.call("list_drafts", nil)
		if err != nil {
			return err
		}
		r.emit(o, func() {
			for _, d := range list(o["drafts"]) {
				fmt.Fprintf(r.out, "%s  %s\n", s(d["id"]), s(d["subject"]))
			}
		})
	case "folder":
		return r.folder()
	case "rules":
		return r.rules()
	case "templates":
		return r.simpleList("templates", "list_templates", "save_template", "delete_template", "templates", "name", "name", "body", "subject")
	case "searches":
		return r.simpleList("searches", "list_saved_searches", "save_search", "delete_saved_search", "searches", "name", "name", "query")
	case "identities":
		return r.identities()
	case "vacation":
		return r.vacation()
	case "invite":
		return r.invite()
	case "unsubscribe", "receipt":
		if err := r.need(1, cmd+" <message id>"); err != nil {
			return err
		}
		tool := map[string]string{"unsubscribe": "unsubscribe", "receipt": "send_read_receipt"}[cmd]
		o, err := r.call(tool, map[string]any{"messageId": f.pos[0]})
		if err != nil {
			return err
		}
		r.emit(o, func() {
			if u := s(o["openUrl"]); u != "" {
				fmt.Fprintln(r.out, "this sender wants you to confirm on a web page:", u)
			} else {
				fmt.Fprintln(r.out, "done")
			}
		})
	case "import":
		return r.importMail()
	case "export":
		return r.export()
	case "source":
		if err := r.need(1, "source <message id>"); err != nil {
			return err
		}
		o, err := r.call("get_message_source", map[string]any{"messageId": f.pos[0]})
		if err != nil {
			return err
		}
		fmt.Fprint(r.out, s(o["source"]))
	case "keys":
		return r.keys()
	case "verify":
		if err := r.need(1, "verify <message id>"); err != nil {
			return err
		}
		o, err := r.call("open_secure_message", map[string]any{"messageId": f.pos[0]})
		if err != nil {
			return err
		}
		r.emit(o, func() {
			fmt.Fprintf(r.out, "protocol %s  encrypted %s  decrypted %s  signed %s  valid %s  trusted %s\n", s(o["protocol"]), s(o["encrypted"]), s(o["decrypted"]), s(o["signed"]), s(o["valid"]), s(o["trusted"]))
			if sg, ok := o["signer"].(map[string]any); ok && s(sg["email"]) != "" {
				fmt.Fprintf(r.out, "signer %s <%s>\n", s(sg["name"]), s(sg["email"]))
			}
			if p := s(o["problem"]); p != "" {
				fmt.Fprintln(r.out, "note:", p)
			}
			if t := s(o["text"]); t != "" {
				fmt.Fprintf(r.out, "\n%s\n", t)
			}
		})
	case "calendar":
		return r.calendar()
	case "contacts":
		o, err := r.call("search_contacts", map[string]any{"query": strings.Join(f.pos, " ")})
		if err != nil {
			return err
		}
		r.emit(o, func() {
			for _, c := range list(o["contacts"]) {
				fmt.Fprintf(r.out, "%-28s %s %s\n", s(c["name"]), s(c["email"]), s(c["phone"]))
			}
		})
	case "sieve":
		return r.sieve()
	case "update":
		o, err := r.call("check_update", nil)
		if err != nil {
			return err
		}
		r.emit(o, func() {
			switch {
			case o["managed"] == true:
				fmt.Fprintln(r.out, s(o["statement"]))
			case o["newer"] == true:
				fmt.Fprintf(r.out, "version %s is out (you have %s): %s\nsha256 %s\n", s(o["latest"]), s(o["current"]), s(o["url"]), s(o["sha256"]))
			default:
				fmt.Fprintf(r.out, "you have the newest version (%s)\n", s(o["current"]))
			}
		})
	case "groups":
		sub := "list"
		if len(f.pos) > 0 {
			sub = f.pos[0]
		}
		switch sub {
		case "list":
			o, err := r.call("list_contact_groups", nil)
			if err != nil {
				return err
			}
			r.emit(o, func() {
				for _, g := range list(o["groups"]) {
					fmt.Fprintf(r.out, "%s  %-20s %s\n", s(g["id"]), s(g["name"]), s(g["members"]))
				}
			})
		case "save":
			o, err := r.call("save_contact_group", map[string]any{"accountId": f.str("account"), "id": f.str("id"), "name": f.str("name"), "members": strings.Split(f.str("members"), ",")})
			if err != nil {
				return err
			}
			r.emit(o, func() { fmt.Fprintln(r.out, "saved", s(o["id"])) })
		case "delete":
			if len(f.pos) < 2 {
				return errors.New("usage: groups delete <id>")
			}
			_, err := r.call("delete_contact_group", map[string]any{"accountId": f.str("account"), "id": f.pos[1]})
			return err
		default:
			return errors.New("groups: list, save or delete")
		}
	case "sync":
		args := map[string]any{}
		if len(f.pos) > 0 {
			args["accountId"] = f.pos[0]
		}
		_, err := r.call("sync_now", args)
		return err
	case "settings":
		if len(f.pos) >= 2 {
			upd := map[string]any{}
			for i := 0; i+1 < len(f.pos); i += 2 {
				upd[f.pos[i]] = f.pos[i+1]
			}
			_, err := r.call("update_settings", upd)
			return err
		}
		o, err := r.call("get_settings", nil)
		if err != nil {
			return err
		}
		r.emit(o, func() {
			keys := make([]string, 0, len(o))
			for k := range o {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintf(r.out, "%s = %s\n", k, s(o[k]))
			}
		})
	case "mcp":
		dir, _ := app.DataDir()
		if raw, err := os.ReadFile(filepath.Join(dir, "mcp.json")); err == nil {
			fmt.Fprintf(r.out, "The MCP server runs while the app is open. Connect a client to:\n%s\n", raw)
		} else {
			fmt.Fprintf(r.out, "Open the app once and it starts the MCP server; connection details are then written to %s\nFor a client that launches the program itself, run: %s-mcp (stdio)\n", filepath.Join(dir, "mcp.json"), brand.Slug)
		}
	default:
		return fmt.Errorf("unknown command %q: run %s help", cmd, brand.Slug)
	}
	return nil
}

func (r *runner) body() string {
	if p := r.f.str("body-file"); p != "" {
		b, _ := os.ReadFile(p)
		return string(b)
	}
	if b := r.f.str("body"); b != "" {
		return b
	}
	if fi, err := os.Stdin.Stat(); err == nil && fi.Mode()&os.ModeCharDevice == 0 {
		b, _ := io.ReadAll(os.Stdin)
		return string(b)
	}
	return ""
}

func (r *runner) waitSent(o map[string]any) error {
	id := s(o["outboxId"])
	if id == "" || s(o["sendsAt"]) != "" {
		r.emit(o, func() {
			if t := s(o["sendsAt"]); t != "" {
				fmt.Fprintf(r.out, "scheduled for %s (cancel with: outbox cancel %s)\n", t, id)
			}
		})
		return nil
	}
	end := time.Now().Add(90 * time.Second)
	for time.Now().Before(end) {
		ob, err := r.call("list_outbox", nil)
		if err != nil {
			return err
		}
		found := false
		for _, x := range list(ob["outbox"]) {
			if s(x["id"]) == id {
				found = true
				if s(x["state"]) == "failed" {
					return errors.New("could not send: " + s(x["error"]))
				}
			}
		}
		if !found {
			r.emit(o, func() { fmt.Fprintln(r.out, "sent") })
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return errors.New("still sending after 90 seconds: check the outbox")
}

func (r *runner) send() error {
	f := r.f
	if len(f.opt["to"]) == 0 {
		return errors.New("usage: send --to a@b --subject s --body text")
	}
	acct := f.str("account")
	if acct == "" {
		o, err := r.call("list_accounts", nil)
		if err != nil {
			return err
		}
		as := list(o["accounts"])
		if len(as) == 0 {
			return errors.New("no account is set up")
		}
		acct = s(as[0]["id"])
	}
	args := map[string]any{"accountId": acct, "to": f.opt["to"], "subject": f.str("subject"), "bodyText": r.body()}
	if v := f.opt["cc"]; len(v) > 0 {
		args["cc"] = v
	}
	if v := f.opt["bcc"]; len(v) > 0 {
		args["bcc"] = v
	}
	var att []map[string]any
	for _, p := range f.opt["attach"] {
		att = append(att, map[string]any{"filename": filepath.Base(p), "path": p})
	}
	if len(att) > 0 {
		args["attachments"] = att
	}
	if v := f.str("from"); v != "" {
		args["from"] = v
	}
	if v := f.str("at"); v != "" {
		args["sendAt"] = v
	} else {
		args["immediate"] = true
	}
	if f.bool["receipt"] {
		args["receipt"] = true
	}
	if f.bool["sign"] {
		args["sign"] = true
	}
	if f.bool["encrypt"] {
		args["encrypt"] = true
	}
	if v := f.str("protect"); v != "" {
		args["protect"] = v
	}
	o, err := r.call("send_message", args)
	if err != nil {
		return err
	}
	return r.waitSent(o)
}

func (r *runner) folder() error {
	f := r.f
	if err := r.need(2, "folder <create|rename|delete|markread|empty> <account> ..."); err != nil {
		return err
	}
	sub, acct := f.pos[0], f.pos[1]
	rest := f.pos[2:]
	var tool string
	args := map[string]any{"accountId": acct}
	switch sub {
	case "create":
		tool = "create_folder"
		args["name"] = strings.Join(rest, " ")
	case "rename":
		if len(rest) < 2 {
			return errors.New("usage: folder rename <account> <folder id> <new name>")
		}
		tool, args["remoteId"], args["name"] = "rename_folder", rest[0], strings.Join(rest[1:], " ")
	case "delete":
		tool = "delete_folder"
		if len(rest) < 1 {
			return errors.New("usage: folder delete <account> <folder id>")
		}
		args["remoteId"] = rest[0]
	case "markread":
		tool = "mark_folder_read"
		if len(rest) < 1 {
			return errors.New("usage: folder markread <account> <folder id>")
		}
		args["remoteId"] = rest[0]
	case "empty":
		tool = "empty_folder"
		if len(rest) < 1 {
			return errors.New("usage: folder empty <account> <trash|spam>")
		}
		args["role"] = rest[0]
	default:
		return errors.New("folder: create, rename, delete, markread or empty")
	}
	o, err := r.call(tool, args)
	if err != nil {
		return err
	}
	r.emit(o, func() { fmt.Fprintln(r.out, "done") })
	return nil
}

func (r *runner) rules() error {
	f := r.f
	sub := "list"
	if len(f.pos) > 0 {
		sub = f.pos[0]
	}
	switch sub {
	case "list":
		o, err := r.call("list_rules", nil)
		if err != nil {
			return err
		}
		r.emit(o, func() {
			for _, x := range list(o["rules"]) {
				fmt.Fprintf(r.out, "%s  %-20s enabled=%s  if %s  then %s\n", s(x["id"]), s(x["name"]), s(x["enabled"]), s(x["conditions"]), s(x["actions"]))
			}
		})
	case "add":
		var conds, acts []map[string]any
		for _, c := range f.opt["if"] {
			p := strings.SplitN(c, ":", 3)
			if len(p) < 2 {
				return errors.New("--if field:op:value, for example subject:contains:invoice")
			}
			cond := map[string]any{"field": p[0], "op": p[1]}
			if len(p) == 3 {
				cond["value"] = p[2]
			} else {
				cond["op"], cond["value"] = "contains", p[1]
			}
			conds = append(conds, cond)
		}
		for _, a := range f.opt["then"] {
			p := strings.SplitN(a, ":", 2)
			act := map[string]any{"kind": p[0]}
			if len(p) == 2 {
				if p[0] == "forward" {
					act["to"] = p[1]
				} else {
					act["folderId"] = p[1]
				}
			}
			acts = append(acts, act)
		}
		args := map[string]any{"name": f.str("name"), "enabled": true, "match": "all", "conditions": conds, "actions": acts, "accountId": f.str("account")}
		if f.str("match") == "any" {
			args["match"] = "any"
		}
		o, err := r.call("save_rule", args)
		if err != nil {
			return err
		}
		r.emit(o, func() { fmt.Fprintln(r.out, "saved", s(o["id"])) })
	case "delete":
		if len(f.pos) < 2 {
			return errors.New("usage: rules delete <id>")
		}
		_, err := r.call("delete_rule", map[string]any{"id": f.pos[1]})
		return err
	case "run":
		if len(f.pos) < 2 {
			return errors.New("usage: rules run <account id>")
		}
		o, err := r.call("run_rules", map[string]any{"accountId": f.pos[1]})
		if err != nil {
			return err
		}
		r.emit(o, func() { fmt.Fprintln(r.out, s(o["matched"]), "message(s) matched") })
	default:
		return errors.New("rules: list, add, delete or run")
	}
	return nil
}

// simpleList handles templates and saved searches, which are lists of named records.
func (r *runner) simpleList(_ string, listTool, saveTool, delTool, key string, fields ...string) error {
	f := r.f
	sub := "list"
	if len(f.pos) > 0 {
		sub = f.pos[0]
	}
	switch sub {
	case "list":
		o, err := r.call(listTool, nil)
		if err != nil {
			return err
		}
		r.emit(o, func() {
			for _, x := range list(o[key]) {
				fmt.Fprintf(r.out, "%s  %-24s %s\n", s(x["id"]), s(x["name"]), strings.TrimSpace(s(x["query"])+s(x["body"])))
			}
		})
	case "add":
		args := map[string]any{}
		for _, k := range fields {
			if v := f.str(k); v != "" {
				args[k] = v
			}
		}
		o, err := r.call(saveTool, args)
		if err != nil {
			return err
		}
		r.emit(o, func() { fmt.Fprintln(r.out, "saved", s(o["id"])) })
	case "delete":
		if len(f.pos) < 2 {
			return errors.New("usage: delete <id>")
		}
		_, err := r.call(delTool, map[string]any{"id": f.pos[1]})
		return err
	default:
		return errors.New("use list, add or delete")
	}
	return nil
}

func (r *runner) identities() error {
	f := r.f
	sub := "list"
	if len(f.pos) > 0 {
		sub = f.pos[0]
	}
	switch sub {
	case "list", "refresh":
		tool, args := "list_identities", map[string]any{}
		if sub == "refresh" {
			tool = "refresh_identities"
			args["accountId"] = f.str("account")
		}
		o, err := r.call(tool, args)
		if err != nil {
			return err
		}
		r.emit(o, func() {
			for _, x := range list(o["identities"]) {
				fmt.Fprintf(r.out, "%s  %s <%s>  default=%s alias=%s\n", s(x["id"]), s(x["name"]), s(x["email"]), s(x["default"]), s(x["alias"]))
			}
		})
	case "add":
		o, err := r.call("save_identity", map[string]any{"accountId": f.str("account"), "email": f.str("email"), "name": f.str("name"), "signature": f.str("signature"), "replyTo": f.str("reply-to"), "default": f.bool["default"]})
		if err != nil {
			return err
		}
		r.emit(o, func() { fmt.Fprintln(r.out, "saved", s(o["id"])) })
	case "delete":
		if len(f.pos) < 2 {
			return errors.New("usage: identities delete <id>")
		}
		_, err := r.call("delete_identity", map[string]any{"id": f.pos[1]})
		return err
	default:
		return errors.New("identities: list, refresh, add or delete")
	}
	return nil
}

func (r *runner) vacation() error {
	f := r.f
	if len(f.pos) < 1 || f.str("account") == "" {
		return errors.New("usage: vacation <get|on|off> --account id [--subject s --body text --from t --to t]")
	}
	acct := f.str("account")
	switch f.pos[0] {
	case "get":
		o, err := r.call("get_vacation", map[string]any{"accountId": acct})
		if err != nil {
			return err
		}
		r.emit(o, func() {
			fmt.Fprintf(r.out, "enabled=%s subject=%q from=%s to=%s\n%s\n", s(o["enabled"]), s(o["subject"]), s(o["start"]), s(o["end"]), s(o["body"]))
		})
	case "on", "off":
		args := map[string]any{"accountId": acct, "enabled": f.pos[0] == "on", "subject": f.str("subject"), "body": r.body()}
		if v := f.str("from"); v != "" {
			args["start"] = v
		}
		if v := f.str("to"); v != "" {
			args["end"] = v
		}
		_, err := r.call("set_vacation", args)
		return err
	default:
		return errors.New("vacation: get, on or off")
	}
	return nil
}

func (r *runner) invite() error {
	f := r.f
	if len(f.pos) < 2 {
		return errors.New("usage: invite <show|accept|decline|tentative|calendar> <message id>")
	}
	switch f.pos[0] {
	case "show":
		o, err := r.call("get_invite", map[string]any{"messageId": f.pos[1]})
		if err != nil {
			return err
		}
		r.emit(o, func() {
			for _, ev := range list(o["events"]) {
				fmt.Fprintf(r.out, "%s\n  %s to %s\n  %s\n  organizer %s\n", s(ev["summary"]), s(ev["start"]), s(ev["end"]), s(ev["location"]), s(ev["organizer"]))
			}
			if me, ok := o["me"].(map[string]any); ok {
				fmt.Fprintln(r.out, "your answer so far:", s(me["partStat"]))
			}
		})
	case "accept", "decline", "tentative":
		o, err := r.call("respond_invite", map[string]any{"messageId": f.pos[1], "answer": f.pos[0]})
		if err != nil {
			return err
		}
		r.emit(o, func() { fmt.Fprintln(r.out, "answered") })
	case "calendar":
		o, err := r.call("add_invite_to_calendar", map[string]any{"messageId": f.pos[1], "calendarAccountId": f.str("account")})
		if err != nil {
			return err
		}
		r.emit(o, func() { fmt.Fprintln(r.out, "saved to the calendar:", s(o["url"])) })
	default:
		return errors.New("invite: show, accept, decline, tentative or calendar")
	}
	return nil
}

func (r *runner) importMail() error {
	f := r.f
	if len(f.pos) < 2 {
		return errors.New("usage: import <mbox file|thunderbird dir> ...")
	}
	var o map[string]any
	var err error
	switch f.pos[0] {
	case "mbox":
		o, err = r.call("import_mbox", map[string]any{"path": f.pos[1], "folder": f.str("folder")})
	case "thunderbird":
		o, err = r.call("import_thunderbird", map[string]any{"path": f.pos[1]})
	default:
		return errors.New("import: mbox or thunderbird")
	}
	if err != nil {
		return err
	}
	r.emit(o, func() { fmt.Fprintln(r.out, s(o)) })
	return nil
}

func (r *runner) export() error {
	f := r.f
	if len(f.pos) < 3 {
		return errors.New("usage: export eml <message id> <file> | export mbox <file> <thread id>...")
	}
	var o map[string]any
	var err error
	switch f.pos[0] {
	case "eml":
		o, err = r.call("export_message", map[string]any{"messageId": f.pos[1], "path": f.pos[2]})
	case "mbox":
		o, err = r.call("export_threads", map[string]any{"path": f.pos[1], "threadIds": f.pos[2:]})
	default:
		return errors.New("export: eml or mbox")
	}
	if err != nil {
		return err
	}
	r.emit(o, func() { fmt.Fprintf(r.out, "saved %s (%s bytes)\n", s(o["path"]), s(o["size"])) })
	return nil
}

func (r *runner) keys() error {
	f := r.f
	sub := "list"
	if len(f.pos) > 0 {
		sub = f.pos[0]
	}
	switch sub {
	case "list":
		o, err := r.call("list_keys", nil)
		if err != nil {
			return err
		}
		r.emit(o, func() {
			for _, k := range list(o["pgp"]) {
				fmt.Fprintf(r.out, "pgp    %-20.20s %s <%s>\n", s(k["id"]), s(k["name"]), s(k["email"]))
			}
			for _, k := range list(o["smime"]) {
				fmt.Fprintf(r.out, "smime  %-20.20s %s <%s>  expires %s\n", s(k["id"]), s(k["name"]), s(k["email"]), s(k["expires"]))
			}
		})
	case "make":
		_, err := r.call("make_key", map[string]any{"kind": f.str("kind"), "name": f.str("name"), "email": f.str("email")})
		return err
	case "import":
		if len(f.pos) < 2 {
			return errors.New("usage: keys import <file> --kind pgp|smime [--password p]")
		}
		o, err := r.call("import_key", map[string]any{"kind": f.str("kind"), "path": f.pos[1], "password": f.str("password")})
		if err != nil {
			return err
		}
		r.emit(o, func() { fmt.Fprintln(r.out, "imported", s(o["imported"])) })
	case "remove":
		if len(f.pos) < 2 {
			return errors.New("usage: keys remove <id>")
		}
		_, err := r.call("remove_key", map[string]any{"id": f.pos[1]})
		return err
	case "export":
		if len(f.pos) < 2 {
			return errors.New("usage: keys export <id>")
		}
		o, err := r.call("export_key", map[string]any{"id": f.pos[1]})
		if err != nil {
			return err
		}
		fmt.Fprint(r.out, s(o["text"]))
	default:
		return errors.New("keys: list, make, import, remove or export")
	}
	return nil
}

func (r *runner) calendar() error {
	f := r.f
	if len(f.pos) < 1 {
		return errors.New("usage: calendar <set|list|events|add|delete> --account id ...")
	}
	acct := f.str("account")
	switch f.pos[0] {
	case "set":
		_, err := r.call("set_calendar_server", map[string]any{"accountId": acct, "url": f.str("url"), "user": f.str("user"), "password": f.str("password")})
		return err
	case "list":
		o, err := r.call("list_calendars", map[string]any{"accountId": acct})
		if err != nil {
			return err
		}
		r.emit(o, func() {
			for _, c := range list(o["calendars"]) {
				fmt.Fprintf(r.out, "%-24s %s\n", s(c["name"]), s(c["url"]))
			}
		})
	case "events":
		from := time.Now()
		o, err := r.call("list_events", map[string]any{"accountId": acct, "from": from.Format(time.RFC3339), "to": from.AddDate(0, 0, f.num("days", 30)).Format(time.RFC3339)})
		if err != nil {
			return err
		}
		r.emit(o, func() {
			for _, e := range list(o["events"]) {
				fmt.Fprintf(r.out, "%s  %-32s %s  %s\n", s(e["start"]), s(e["summary"]), s(e["location"]), s(e["url"]))
			}
		})
	case "add":
		o, err := r.call("save_event", map[string]any{"accountId": acct, "title": f.str("title"), "start": f.str("start"), "end": f.str("end"), "location": f.str("location"), "description": f.str("description")})
		if err != nil {
			return err
		}
		r.emit(o, func() { fmt.Fprintln(r.out, "saved", s(o["url"])) })
	case "delete":
		_, err := r.call("delete_event", map[string]any{"accountId": acct, "url": f.str("url")})
		return err
	default:
		return errors.New("calendar: set, list, events, add or delete")
	}
	return nil
}

func (r *runner) sieve() error {
	f := r.f
	if len(f.pos) < 1 || f.str("account") == "" {
		return errors.New("usage: sieve <list|get|put|check|activate|delete> --account id ...")
	}
	acct, name := f.str("account"), f.str("name")
	script := ""
	if p := f.str("file"); p != "" {
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		script = string(b)
	}
	var o map[string]any
	var err error
	switch f.pos[0] {
	case "list":
		o, err = r.call("list_sieve_scripts", map[string]any{"accountId": acct})
		if err == nil {
			r.emit(o, func() {
				for _, x := range list(o["scripts"]) {
					fmt.Fprintf(r.out, "%-24s active=%s\n", s(x["name"]), s(x["active"]))
				}
			})
		}
		return err
	case "get":
		o, err = r.call("get_sieve_script", map[string]any{"accountId": acct, "name": name})
		if err == nil {
			fmt.Fprint(r.out, s(o["script"]))
		}
		return err
	case "put":
		_, err = r.call("put_sieve_script", map[string]any{"accountId": acct, "name": name, "script": script, "activate": f.bool["activate"]})
	case "check":
		_, err = r.call("check_sieve_script", map[string]any{"accountId": acct, "script": script})
	case "activate":
		_, err = r.call("activate_sieve_script", map[string]any{"accountId": acct, "name": name})
	case "delete":
		_, err = r.call("delete_sieve_script", map[string]any{"accountId": acct, "name": name})
	default:
		return errors.New("sieve: list, get, put, check, activate or delete")
	}
	if err == nil {
		fmt.Fprintln(r.out, "ok")
	}
	return err
}
