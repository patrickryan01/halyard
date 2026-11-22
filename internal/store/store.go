// Package store is the local cache: accounts, folders, messages, the offline
// action queue, the outbox, snoozes and a full-text index, all in one SQLite
// file. Everything the UI shows is read from here, so the client works offline
// and the engine only ever writes what providers report.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/patrickryan01/halyard/internal/mailtypes"
	"github.com/patrickryan01/halyard/internal/mime"
)

type Store struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS accounts(
  id TEXT PRIMARY KEY, kind TEXT NOT NULL, email TEXT NOT NULL, name TEXT NOT NULL DEFAULT '',
  settings TEXT NOT NULL DEFAULT '{}', signature TEXT NOT NULL DEFAULT '', color TEXT NOT NULL DEFAULT '',
  sync_state TEXT NOT NULL DEFAULT '', last_sync INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS folders(
  id TEXT PRIMARY KEY, account_id TEXT NOT NULL, remote_id TEXT NOT NULL, name TEXT NOT NULL,
  role TEXT NOT NULL, is_label INTEGER NOT NULL DEFAULT 0);
CREATE INDEX IF NOT EXISTS folders_acct ON folders(account_id);
CREATE TABLE IF NOT EXISTS messages(
  id TEXT PRIMARY KEY, account_id TEXT NOT NULL, remote_id TEXT NOT NULL, thread_id TEXT NOT NULL,
  subject TEXT, from_name TEXT, from_email TEXT, to_json TEXT, cc_json TEXT, bcc_json TEXT, reply_to_json TEXT,
  date INTEGER, snippet TEXT, body_text TEXT, body_html TEXT, message_id TEXT, in_reply_to TEXT, refs_json TEXT,
  is_read INTEGER, is_starred INTEGER, is_draft INTEGER, attach_json TEXT, has_attach INTEGER DEFAULT 0,
  snoozed INTEGER NOT NULL DEFAULT 0);
CREATE INDEX IF NOT EXISTS msg_thread ON messages(thread_id);
CREATE INDEX IF NOT EXISTS msg_date ON messages(date);
CREATE INDEX IF NOT EXISTS msg_mid ON messages(account_id, message_id);
CREATE INDEX IF NOT EXISTS msg_acct ON messages(account_id, remote_id);
CREATE TABLE IF NOT EXISTS msg_folders(msg_id TEXT NOT NULL, account_id TEXT NOT NULL, folder_remote TEXT NOT NULL,
  PRIMARY KEY(msg_id, folder_remote));
CREATE INDEX IF NOT EXISTS mf_folder ON msg_folders(account_id, folder_remote);
CREATE VIRTUAL TABLE IF NOT EXISTS search USING fts5(subject, sender, recipients, body, tokenize='unicode61 remove_diacritics 2');
CREATE TABLE IF NOT EXISTS ops(id INTEGER PRIMARY KEY AUTOINCREMENT, account_id TEXT NOT NULL, action TEXT NOT NULL, created INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS outbox(id TEXT PRIMARY KEY, account_id TEXT NOT NULL, payload BLOB NOT NULL, send_at INTEGER NOT NULL,
  state TEXT NOT NULL, error TEXT NOT NULL DEFAULT '', subject TEXT NOT NULL DEFAULT '', attempts INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS contacts(account_id TEXT NOT NULL, email TEXT NOT NULL, name TEXT NOT NULL DEFAULT '',
  uses INTEGER NOT NULL DEFAULT 0, PRIMARY KEY(account_id, email));
CREATE TABLE IF NOT EXISTS drafts(id TEXT PRIMARY KEY, account_id TEXT NOT NULL, data TEXT NOT NULL, subject TEXT NOT NULL DEFAULT '', updated INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS local_contacts(id TEXT PRIMARY KEY, account_id TEXT NOT NULL, name TEXT NOT NULL DEFAULT '', email TEXT NOT NULL DEFAULT '', phone TEXT NOT NULL DEFAULT '', org TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS rules(id TEXT PRIMARY KEY, account_id TEXT NOT NULL DEFAULT '', pos INTEGER NOT NULL, data TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS templates(id TEXT PRIMARY KEY, name TEXT NOT NULL, subject TEXT NOT NULL DEFAULT '', body TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS identities(id TEXT PRIMARY KEY, account_id TEXT NOT NULL, name TEXT NOT NULL DEFAULT '', email TEXT NOT NULL, reply_to TEXT NOT NULL DEFAULT '',
  signature TEXT NOT NULL DEFAULT '', is_default INTEGER NOT NULL DEFAULT 0, alias INTEGER NOT NULL DEFAULT 0, source TEXT NOT NULL DEFAULT 'user');
CREATE TABLE IF NOT EXISTS saved_searches(id TEXT PRIMARY KEY, name TEXT NOT NULL, query TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS raw_messages(account_id TEXT NOT NULL, remote_id TEXT NOT NULL, data BLOB NOT NULL, PRIMARY KEY(account_id, remote_id));
CREATE TABLE IF NOT EXISTS settings(k TEXT PRIMARY KEY, v TEXT NOT NULL);
`

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(8000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("store schema: %w", err)
	}
	if err := migrate(db); err != nil {
		return nil, fmt.Errorf("store migrate: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// migrate adds the columns a database made by an earlier version lacks. A column is added only when it is missing, so
// it is safe on every start.
func migrate(db *sql.DB) error {
	have := map[string]bool{}
	rows, err := db.Query(`PRAGMA table_info(messages)`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		have[name] = true
	}
	rows.Close()
	if _, err := db.Exec(`ALTER TABLE outbox ADD COLUMN attempts INTEGER NOT NULL DEFAULT 0`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return err
	}
	for _, c := range []struct{ name, ddl string }{
		{"size", "ALTER TABLE messages ADD COLUMN size INTEGER NOT NULL DEFAULT 0"},
		{"extra", "ALTER TABLE messages ADD COLUMN extra TEXT NOT NULL DEFAULT ''"},
	} {
		if !have[c.name] {
			if _, err := db.Exec(c.ddl); err != nil {
				return err
			}
		}
	}
	return nil
}

func js(v any) string { b, _ := json.Marshal(v); return string(b) }

// ---- accounts ----

func (s *Store) PutAccount(a mailtypes.Account) error {
	_, err := s.db.Exec(`INSERT INTO accounts(id,kind,email,name,settings,signature,color) VALUES(?,?,?,?,?,?,?)
	 ON CONFLICT(id) DO UPDATE SET kind=excluded.kind,email=excluded.email,name=excluded.name,settings=excluded.settings,
	 signature=excluded.signature,color=excluded.color`,
		a.ID, a.Kind, a.Email, a.Name, js(a.Settings), a.Signature, a.Color)
	return err
}

func (s *Store) Accounts() ([]mailtypes.Account, error) {
	rows, err := s.db.Query(`SELECT id,kind,email,name,settings,signature,color,sync_state,last_sync,last_error FROM accounts ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []mailtypes.Account
	for rows.Next() {
		var a mailtypes.Account
		var set string
		var ls int64
		if err := rows.Scan(&a.ID, &a.Kind, &a.Email, &a.Name, &set, &a.Signature, &a.Color, &a.SyncState, &ls, &a.LastError); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(set), &a.Settings)
		if ls > 0 {
			a.LastSync = time.Unix(ls, 0)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) Account(id string) (mailtypes.Account, error) {
	all, err := s.Accounts()
	if err != nil {
		return mailtypes.Account{}, err
	}
	for _, a := range all {
		if a.ID == id {
			return a, nil
		}
	}
	return mailtypes.Account{}, sql.ErrNoRows
}

func (s *Store) SetSyncState(id, state, lastErr string) error {
	_, err := s.db.Exec(`UPDATE accounts SET sync_state=?, last_sync=?, last_error=? WHERE id=?`, state, time.Now().Unix(), lastErr, id)
	return err
}

func (s *Store) SetAccountError(id, lastErr string) error {
	_, err := s.db.Exec(`UPDATE accounts SET last_error=? WHERE id=?`, lastErr, id)
	return err
}

func (s *Store) DeleteAccount(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM search WHERE rowid IN (SELECT rowid FROM messages WHERE account_id=?)`,
		`DELETE FROM msg_folders WHERE account_id=?`, `DELETE FROM messages WHERE account_id=?`,
		`DELETE FROM folders WHERE account_id=?`, `DELETE FROM ops WHERE account_id=?`,
		`DELETE FROM outbox WHERE account_id=?`, `DELETE FROM contacts WHERE account_id=?`,
		`DELETE FROM accounts WHERE id=?`,
	} {
		if _, err := tx.Exec(q, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---- folders ----

func (s *Store) ReplaceFolders(accountID string, fs []mailtypes.Folder) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM folders WHERE account_id=?`, accountID); err != nil {
		return err
	}
	for _, f := range fs {
		if _, err := tx.Exec(`INSERT INTO folders(id,account_id,remote_id,name,role,is_label) VALUES(?,?,?,?,?,?)`,
			accountID+"/"+f.RemoteID, accountID, f.RemoteID, f.Name, f.Role, b2i(f.IsLabel)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Folders(accountID string) ([]mailtypes.Folder, error) {
	q := `SELECT f.id,f.account_id,f.remote_id,f.name,f.role,f.is_label,
	  (SELECT COUNT(*) FROM msg_folders mf JOIN messages m ON m.id=mf.msg_id WHERE mf.account_id=f.account_id AND mf.folder_remote=f.remote_id AND m.is_read=0),
	  (SELECT COUNT(*) FROM msg_folders mf WHERE mf.account_id=f.account_id AND mf.folder_remote=f.remote_id)
	  FROM folders f`
	var args []any
	if accountID != "" {
		q += ` WHERE f.account_id=?`
		args = append(args, accountID)
	}
	q += ` ORDER BY f.account_id, CASE f.role WHEN 'inbox' THEN 0 WHEN 'starred' THEN 1 WHEN 'drafts' THEN 2 WHEN 'sent' THEN 3 WHEN 'archive' THEN 4 WHEN 'spam' THEN 5 WHEN 'trash' THEN 6 WHEN 'all' THEN 7 ELSE 8 END, f.name COLLATE NOCASE`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []mailtypes.Folder
	for rows.Next() {
		var f mailtypes.Folder
		var lbl int
		if err := rows.Scan(&f.ID, &f.AccountID, &f.RemoteID, &f.Name, &f.Role, &lbl, &f.Unread, &f.Total); err != nil {
			return nil, err
		}
		f.IsLabel = lbl == 1
		out = append(out, f)
	}
	return out, rows.Err()
}

// FolderByRole returns the remote ID of the account's folder with that role.
func (s *Store) FolderByRole(accountID, role string) (string, bool) {
	var id string
	err := s.db.QueryRow(`SELECT remote_id FROM folders WHERE account_id=? AND role=? LIMIT 1`, accountID, role).Scan(&id)
	return id, err == nil
}

// ---- messages ----

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func names(as []mailtypes.Address) string {
	var p []string
	for _, a := range as {
		p = append(p, a.Name+" "+a.Email)
	}
	return strings.Join(p, " ")
}

// UpsertMessages writes messages and keeps the folder join and the text index
// in step. Snooze state is local, so an update from the provider keeps it.
func (s *Store) UpsertMessages(ms []mailtypes.Message) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, m := range ms {
		if m.ID == "" {
			m.ID = m.AccountID + "/" + m.RemoteID
		}
		var oldRow int64
		_ = tx.QueryRow(`SELECT rowid FROM messages WHERE id=?`, m.ID).Scan(&oldRow)
		snoozed := int64(0)
		if oldRow != 0 {
			_ = tx.QueryRow(`SELECT snoozed FROM messages WHERE id=?`, m.ID).Scan(&snoozed)
			if _, err := tx.Exec(`DELETE FROM search WHERE rowid=?`, oldRow); err != nil {
				return err
			}
			if _, err := tx.Exec(`DELETE FROM msg_folders WHERE msg_id=?`, m.ID); err != nil {
				return err
			}
		}
		if m.Snoozed != nil && !m.Snoozed.IsZero() {
			snoozed = m.Snoozed.Unix()
		}
		if m.ThreadID == "" {
			m.ThreadID, err = resolveThread(tx, m)
			if err != nil {
				return err
			}
		}
		res, err := tx.Exec(`INSERT OR REPLACE INTO messages(id,account_id,remote_id,thread_id,subject,from_name,from_email,to_json,cc_json,bcc_json,reply_to_json,
		  date,snippet,body_text,body_html,message_id,in_reply_to,refs_json,is_read,is_starred,is_draft,attach_json,has_attach,snoozed,size,extra)
		  VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			m.ID, m.AccountID, m.RemoteID, m.ThreadID, m.Subject, m.From.Name, m.From.Email, js(m.To), js(m.Cc), js(m.Bcc), js(m.ReplyTo),
			m.Date.Unix(), m.Snippet, m.BodyText, m.BodyHTML, m.MessageID, m.InReplyTo, js(m.References),
			b2i(m.Read), b2i(m.Starred), b2i(m.Draft), js(m.Attach), b2i(hasRealAttach(m.Attach)), snoozed, messageSize(m), packExtra(m))
		if err != nil {
			return err
		}
		rid, _ := res.LastInsertId()
		body := m.BodyText
		if body == "" {
			body = StripHTML(m.BodyHTML)
		}
		if _, err := tx.Exec(`INSERT INTO search(rowid,subject,sender,recipients,body) VALUES(?,?,?,?,?)`,
			rid, m.Subject, m.From.Name+" "+m.From.Email, names(m.To)+" "+names(m.Cc), body); err != nil {
			return err
		}
		for _, f := range m.Folders {
			if _, err := tx.Exec(`INSERT OR IGNORE INTO msg_folders(msg_id,account_id,folder_remote) VALUES(?,?,?)`, m.ID, m.AccountID, f); err != nil {
				return err
			}
		}
		for _, a := range append(append([]mailtypes.Address{m.From}, m.To...), m.Cc...) {
			if a.Email == "" {
				continue
			}
			if _, err := tx.Exec(`INSERT INTO contacts(account_id,email,name,uses) VALUES(?,?,?,1)
			  ON CONFLICT(account_id,email) DO UPDATE SET uses=uses+1, name=CASE WHEN excluded.name<>'' THEN excluded.name ELSE name END`,
				m.AccountID, strings.ToLower(a.Email), a.Name); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func hasRealAttach(as []mailtypes.Attachment) bool {
	for _, a := range as {
		if !a.Inline {
			return true
		}
	}
	return false
}

func (s *Store) DeleteMessages(accountID string, remoteIDs []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, r := range remoteIDs {
		id := accountID + "/" + r
		var row int64
		if err := tx.QueryRow(`SELECT rowid FROM messages WHERE id=?`, id).Scan(&row); err == nil {
			_, _ = tx.Exec(`DELETE FROM search WHERE rowid=?`, row)
		}
		_, _ = tx.Exec(`DELETE FROM msg_folders WHERE msg_id=?`, id)
		_, _ = tx.Exec(`DELETE FROM messages WHERE id=?`, id)
	}
	return tx.Commit()
}

// ClearMessages drops every message of an account before a full resync.
func (s *Store) ClearMessages(accountID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM search WHERE rowid IN (SELECT rowid FROM messages WHERE account_id=?)`,
		`DELETE FROM msg_folders WHERE account_id=?`, `DELETE FROM messages WHERE account_id=?`,
	} {
		if _, err := tx.Exec(q, accountID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const msgCols = `id,account_id,remote_id,thread_id,subject,from_name,from_email,to_json,cc_json,bcc_json,reply_to_json,date,snippet,body_text,body_html,
 message_id,in_reply_to,refs_json,is_read,is_starred,is_draft,attach_json,snoozed,size,extra`

func scanMsg(sc interface{ Scan(...any) error }) (mailtypes.Message, error) {
	var m mailtypes.Message
	var to, cc, bcc, rt, refs, att string
	var date, snz, size int64
	var rd, st, dr int
	var extra string
	if err := sc.Scan(&m.ID, &m.AccountID, &m.RemoteID, &m.ThreadID, &m.Subject, &m.From.Name, &m.From.Email, &to, &cc, &bcc, &rt, &date,
		&m.Snippet, &m.BodyText, &m.BodyHTML, &m.MessageID, &m.InReplyTo, &refs, &rd, &st, &dr, &att, &snz, &size, &extra); err != nil {
		return m, err
	}
	m.Size = size
	unpackExtra(extra, &m)
	_ = json.Unmarshal([]byte(to), &m.To)
	_ = json.Unmarshal([]byte(cc), &m.Cc)
	_ = json.Unmarshal([]byte(bcc), &m.Bcc)
	_ = json.Unmarshal([]byte(rt), &m.ReplyTo)
	_ = json.Unmarshal([]byte(refs), &m.References)
	_ = json.Unmarshal([]byte(att), &m.Attach)
	for _, p := range []*[]mailtypes.Address{&m.To, &m.Cc, &m.Bcc, &m.ReplyTo} {
		if *p == nil {
			*p = []mailtypes.Address{}
		}
	}
	if m.References == nil {
		m.References = []string{}
	}
	if m.Attach == nil {
		m.Attach = []mailtypes.Attachment{}
	}
	if m.Folders == nil {
		m.Folders = []string{}
	}
	m.Date = time.Unix(date, 0)
	m.Read, m.Starred, m.Draft = rd == 1, st == 1, dr == 1
	if snz > 0 {
		t0 := time.Unix(snz, 0)
		m.Snoozed = &t0
	}
	return m, nil
}

// Thread returns every message of a thread, oldest first.
func (s *Store) Thread(threadID string) ([]mailtypes.Message, error) {
	rows, err := s.db.Query(`SELECT `+msgCols+` FROM messages WHERE thread_id=? ORDER BY date, rowid`, threadID)
	if err != nil {
		return nil, err
	}
	var all []mailtypes.Message
	for rows.Next() {
		m, err := scanMsg(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		all = append(all, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// the connection pool is one wide, so folders are read after the cursor is closed
	fr, err := s.db.Query(`SELECT mf.msg_id, mf.folder_remote FROM msg_folders mf JOIN messages m ON m.id=mf.msg_id WHERE m.thread_id=?`, threadID)
	if err != nil {
		return nil, err
	}
	byMsg := map[string][]string{}
	for fr.Next() {
		var id, f string
		_ = fr.Scan(&id, &f)
		byMsg[id] = append(byMsg[id], f)
	}
	fr.Close()
	var out []mailtypes.Message
	for _, m := range all {
		m.Folders = byMsg[m.ID]
		if m.Folders == nil {
			m.Folders = []string{}
		}
		// the same message filed in two IMAP folders is one message in a thread
		if m.MessageID != "" {
			dup := -1
			for i := range out {
				if out[i].MessageID == m.MessageID {
					dup = i
					break
				}
			}
			if dup >= 0 {
				out[dup].Folders = append(out[dup].Folders, m.Folders...)
				continue
			}
		}
		out = append(out, m)
	}
	return out, nil
}

func (s *Store) Message(id string) (mailtypes.Message, error) {
	return scanMsg(s.db.QueryRow(`SELECT `+msgCols+` FROM messages WHERE id=?`, id))
}

// ListQuery selects threads. Folder is "role:inbox" for the unified view of
// one role across accounts, "<accountID>/<remoteFolder>" for one folder, or
// "snoozed" for what is asleep. Text is a search; empty means the folder.
type ListQuery struct {
	AccountID string
	Folder    string
	Text      string
	Unread    bool
	HasAttach bool
	Starred   bool
	// Flat lists every message on its own line (conversation view off); the line still opens the whole conversation.
	Flat   bool
	Limit  int
	Offset int
}

func (s *Store) ListThreads(q ListQuery) ([]mailtypes.Thread, error) {
	if q.Limit <= 0 || q.Limit > 500 {
		q.Limit = 100
	}
	now := time.Now().Unix()
	var where []string
	var args []any
	if q.AccountID != "" {
		where = append(where, `m.account_id=?`)
		args = append(args, q.AccountID)
	}
	switch {
	case q.Folder == "snoozed":
		where = append(where, `m.snoozed>?`)
		args = append(args, now)
	case strings.HasPrefix(q.Folder, "role:"):
		role := strings.TrimPrefix(q.Folder, "role:")
		where = append(where, `EXISTS(SELECT 1 FROM msg_folders mf JOIN folders f ON f.account_id=mf.account_id AND f.remote_id=mf.folder_remote
		  WHERE mf.msg_id=m.id AND f.role=?)`)
		args = append(args, role)
		if role != mailtypes.RoleTrash && role != mailtypes.RoleSpam && role != mailtypes.RoleDrafts {
			where = append(where, `(m.snoozed=0 OR m.snoozed<=?)`)
			args = append(args, now)
		}
	case q.Folder == "starred":
		where = append(where, `m.is_starred=1`)
	case q.Folder != "":
		i := strings.Index(q.Folder, "/")
		if i > 0 {
			acct, rem := q.Folder[:i], q.Folder[i+1:]
			where = append(where, `m.account_id=? AND EXISTS(SELECT 1 FROM msg_folders mf WHERE mf.msg_id=m.id AND mf.folder_remote=?)`)
			args = append(args, acct, rem)
		}
	}
	if strings.TrimSpace(q.Text) != "" {
		sw, sa, ok := searchSQL(q.Text)
		if !ok {
			return nil, nil
		}
		where = append(where, sw...)
		args = append(args, sa...)
		// a search spans everything except trash and spam, unless it names a folder itself
		if !strings.Contains(strings.ToLower(q.Text), "in:") && !strings.Contains(strings.ToLower(q.Text), "label:") && !strings.Contains(strings.ToLower(q.Text), "folder:") {
			where = append(where, `NOT EXISTS(SELECT 1 FROM msg_folders mf JOIN folders f ON f.account_id=mf.account_id AND f.remote_id=mf.folder_remote WHERE mf.msg_id=m.id AND f.role IN('trash','spam'))`)
		}
	}
	if q.Unread {
		where = append(where, `m.is_read=0`)
	}
	if q.HasAttach {
		where = append(where, `m.has_attach=1`)
	}
	if q.Starred {
		where = append(where, `m.is_starred=1`)
	}
	w := ""
	if len(where) > 0 {
		w = "WHERE " + strings.Join(where, " AND ")
	}
	groupBy := `m.thread_id`
	if q.Flat {
		groupBy = `m.id`
	}
	query := `SELECT m.thread_id, m.account_id, MAX(m.date) d, m.subject, m.snippet,
	   (SELECT COUNT(DISTINCT CASE WHEN x.message_id='' THEN x.id ELSE x.message_id END) FROM messages x WHERE x.thread_id=m.thread_id),
	   (SELECT COUNT(*) FROM messages x WHERE x.thread_id=m.thread_id AND x.is_read=0),
	   MAX(m.is_starred), MAX(m.has_attach), MAX(m.snoozed),
	   (SELECT group_concat(DISTINCT CASE WHEN x.from_name<>'' THEN x.from_name ELSE x.from_email END) FROM messages x WHERE x.thread_id=m.thread_id)
	 FROM messages m ` + w + ` GROUP BY ` + groupBy + ` ORDER BY d DESC LIMIT ? OFFSET ?`
	args = append(args, q.Limit, q.Offset)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []mailtypes.Thread
	for rows.Next() {
		var t mailtypes.Thread
		var d, snz int64
		var st, at int
		var parts sql.NullString
		if err := rows.Scan(&t.ID, &t.AccountID, &d, &t.Subject, &t.Snippet, &t.Count, &t.Unread, &st, &at, &snz, &parts); err != nil {
			return nil, err
		}
		t.Date = time.Unix(d, 0)
		t.Starred, t.HasAttach = st == 1, at == 1
		if snz > now {
			t0 := time.Unix(snz, 0)
			t.Snoozed = &t0
		}
		if parts.Valid {
			t.Participants = strings.Split(parts.String, ",")
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ---- search text ----

// FTSQuery turns what a person typed into a safe FTS5 query: every word a
// prefix match, joined by AND. Gmail style operators from:, to:, subject: map
// to FTS columns; has:attachment is handled by the caller.
func FTSQuery(text string) string {
	var parts []string
	for _, w := range strings.Fields(text) {
		col := ""
		for _, p := range []struct{ pre, col string }{{"from:", "sender"}, {"to:", "recipients"}, {"subject:", "subject"}} {
			if strings.HasPrefix(strings.ToLower(w), p.pre) {
				col, w = p.col, w[len(p.pre):]
			}
		}
		w = strings.Map(func(r rune) rune {
			if r == '"' || r == '*' || r == '(' || r == ')' || r == ':' || r == '^' {
				return -1
			}
			return r
		}, w)
		if w == "" {
			continue
		}
		term := `"` + strings.ReplaceAll(w, `"`, "") + `"*`
		if col != "" {
			term = col + ":" + term
		}
		parts = append(parts, term)
	}
	return strings.Join(parts, " AND ")
}

// StripHTML reduces HTML to text for the index and for snippets.
func StripHTML(h string) string {
	var b strings.Builder
	in := false
	for _, r := range h {
		switch {
		case r == '<':
			in = true
			b.WriteRune(' ')
		case r == '>':
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// ---- local state: snooze, flags written ahead of the server ----

func (s *Store) Snooze(threadID string, until time.Time) error {
	_, err := s.db.Exec(`UPDATE messages SET snoozed=? WHERE thread_id=?`, until.Unix(), threadID)
	return err
}

func (s *Store) Unsnooze(threadID string) error { return s.Snooze(threadID, time.Time{}) }

// WakeSnoozed clears snoozes that are due and returns the thread IDs.
func (s *Store) WakeSnoozed(now time.Time) ([]string, error) {
	rows, err := s.db.Query(`SELECT DISTINCT thread_id FROM messages WHERE snoozed>0 AND snoozed<=?`, now.Unix())
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	if len(ids) > 0 {
		if _, err := s.db.Exec(`UPDATE messages SET snoozed=0 WHERE snoozed>0 AND snoozed<=?`, now.Unix()); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

// ApplyLocal mirrors an action into the cache so the UI reacts instantly and
// the change survives offline. The queued action then reaches the server.
func (s *Store) ApplyLocal(a mailtypes.Action, accountID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	role := func(r string) string {
		var id string
		_ = tx.QueryRow(`SELECT remote_id FROM folders WHERE account_id=? AND role=?`, accountID, r).Scan(&id)
		return id
	}
	for _, rid := range a.IDs {
		id := accountID + "/" + rid
		switch a.Kind {
		case mailtypes.ActRead:
			_, err = tx.Exec(`UPDATE messages SET is_read=1 WHERE id=?`, id)
		case mailtypes.ActUnread:
			_, err = tx.Exec(`UPDATE messages SET is_read=0 WHERE id=?`, id)
		case mailtypes.ActStar:
			_, err = tx.Exec(`UPDATE messages SET is_starred=1 WHERE id=?`, id)
		case mailtypes.ActUnstar:
			_, err = tx.Exec(`UPDATE messages SET is_starred=0 WHERE id=?`, id)
		case mailtypes.ActArchive:
			if in := role(mailtypes.RoleInbox); in != "" {
				_, err = tx.Exec(`DELETE FROM msg_folders WHERE msg_id=? AND folder_remote=?`, id, in)
			}
			if err == nil {
				if ar := role(mailtypes.RoleArchive); ar != "" {
					_, err = tx.Exec(`INSERT OR IGNORE INTO msg_folders(msg_id,account_id,folder_remote) VALUES(?,?,?)`, id, accountID, ar)
				}
			}
		case mailtypes.ActTrash, mailtypes.ActSpam:
			target := mailtypes.RoleTrash
			if a.Kind == mailtypes.ActSpam {
				target = mailtypes.RoleSpam
			}
			if t := role(target); t != "" {
				// a message in trash is in nothing else
				if _, err = tx.Exec(`DELETE FROM msg_folders WHERE msg_id=?`, id); err == nil {
					_, err = tx.Exec(`INSERT OR IGNORE INTO msg_folders(msg_id,account_id,folder_remote) VALUES(?,?,?)`, id, accountID, t)
				}
			}
		case mailtypes.ActUntrash:
			if in := role(mailtypes.RoleInbox); in != "" {
				if _, err = tx.Exec(`DELETE FROM msg_folders WHERE msg_id=?`, id); err == nil {
					_, err = tx.Exec(`INSERT OR IGNORE INTO msg_folders(msg_id,account_id,folder_remote) VALUES(?,?,?)`, id, accountID, in)
				}
			}
		case mailtypes.ActMove:
			if _, err = tx.Exec(`DELETE FROM msg_folders WHERE msg_id=?`, id); err == nil {
				_, err = tx.Exec(`INSERT OR IGNORE INTO msg_folders(msg_id,account_id,folder_remote) VALUES(?,?,?)`, id, accountID, a.FolderID)
			}
		case mailtypes.ActLabel:
			_, err = tx.Exec(`INSERT OR IGNORE INTO msg_folders(msg_id,account_id,folder_remote) VALUES(?,?,?)`, id, accountID, a.FolderID)
		case mailtypes.ActUnlabel:
			_, err = tx.Exec(`DELETE FROM msg_folders WHERE msg_id=? AND folder_remote=?`, id, a.FolderID)
		case mailtypes.ActDelete:
			var row int64
			if e := tx.QueryRow(`SELECT rowid FROM messages WHERE id=?`, id).Scan(&row); e == nil {
				_, _ = tx.Exec(`DELETE FROM search WHERE rowid=?`, row)
			}
			_, _ = tx.Exec(`DELETE FROM msg_folders WHERE msg_id=?`, id)
			_, err = tx.Exec(`DELETE FROM messages WHERE id=?`, id)
		}
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---- offline action queue ----

type Op struct {
	ID        int64
	AccountID string
	Action    mailtypes.Action
}

func (s *Store) QueueOp(accountID string, a mailtypes.Action) error {
	_, err := s.db.Exec(`INSERT INTO ops(account_id,action,created) VALUES(?,?,?)`, accountID, js(a), time.Now().Unix())
	return err
}

func (s *Store) PendingOps(accountID string) ([]Op, error) {
	rows, err := s.db.Query(`SELECT id,account_id,action FROM ops WHERE account_id=? ORDER BY id`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Op
	for rows.Next() {
		var o Op
		var a string
		if err := rows.Scan(&o.ID, &o.AccountID, &a); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(a), &o.Action)
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *Store) DoneOp(id int64) error {
	_, err := s.db.Exec(`DELETE FROM ops WHERE id=?`, id)
	return err
}

// ---- outbox (undo send) ----

type OutboxItem struct {
	ID        string
	AccountID string
	Payload   []byte
	SendAt    time.Time
	State     string
	Error     string
	Subject   string
}

func (s *Store) QueueOutbox(it OutboxItem) error {
	_, err := s.db.Exec(`INSERT INTO outbox(id,account_id,payload,send_at,state,subject) VALUES(?,?,?,?,?,?)`,
		it.ID, it.AccountID, it.Payload, it.SendAt.UnixMilli(), "queued", it.Subject)
	return err
}

// CancelOutbox removes a queued message if it has not gone out yet.
func (s *Store) CancelOutbox(id string) (bool, error) {
	r, err := s.db.Exec(`DELETE FROM outbox WHERE id=? AND state='queued'`, id)
	if err != nil {
		return false, err
	}
	n, _ := r.RowsAffected()
	return n > 0, nil
}

func (s *Store) DueOutbox(now time.Time) ([]OutboxItem, error) {
	rows, err := s.db.Query(`SELECT id,account_id,payload,send_at,state,error,subject FROM outbox WHERE state='queued' AND send_at<=? ORDER BY send_at`, now.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OutboxItem
	for rows.Next() {
		var it OutboxItem
		var at int64
		if err := rows.Scan(&it.ID, &it.AccountID, &it.Payload, &at, &it.State, &it.Error, &it.Subject); err != nil {
			return nil, err
		}
		it.SendAt = time.UnixMilli(at)
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *Store) Outbox() ([]OutboxItem, error) {
	rows, err := s.db.Query(`SELECT id,account_id,x'',send_at,state,error,subject FROM outbox ORDER BY send_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OutboxItem
	for rows.Next() {
		var it OutboxItem
		var at int64
		if err := rows.Scan(&it.ID, &it.AccountID, &it.Payload, &at, &it.State, &it.Error, &it.Subject); err != nil {
			return nil, err
		}
		it.SendAt = time.UnixMilli(at)
		out = append(out, it)
	}
	return out, rows.Err()
}

// DeferOutbox puts a message that failed for a temporary reason back in the queue to be tried again at a later time.
// It returns false when the message has been tried too often, and is then left to the person.
func (s *Store) DeferOutbox(id string, sendErr error, delays []time.Duration) (bool, error) {
	var tries int
	if err := s.db.QueryRow(`SELECT attempts FROM outbox WHERE id=?`, id).Scan(&tries); err != nil {
		return false, err
	}
	if tries >= len(delays) {
		return false, nil
	}
	_, err := s.db.Exec(`UPDATE outbox SET state='queued', error=?, attempts=attempts+1, send_at=? WHERE id=?`, "will try again: "+sendErr.Error(), time.Now().Add(delays[tries]).UnixMilli(), id)
	return err == nil, err
}

func (s *Store) FinishOutbox(id string, sendErr error) error {
	if sendErr == nil {
		_, err := s.db.Exec(`DELETE FROM outbox WHERE id=?`, id)
		return err
	}
	_, err := s.db.Exec(`UPDATE outbox SET state='failed', error=? WHERE id=?`, sendErr.Error(), id)
	return err
}

// RetryOutbox puts a failed message back in the queue.
func (s *Store) RetryOutbox(id string) error {
	_, err := s.db.Exec(`UPDATE outbox SET state='queued', error='', send_at=? WHERE id=? AND state='failed'`, time.Now().UnixMilli(), id)
	return err
}

// ---- contacts ----

func (s *Store) PutContacts(accountID string, cs []mailtypes.Contact) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, c := range cs {
		if c.Email == "" {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO contacts(account_id,email,name,uses) VALUES(?,?,?,0)
		  ON CONFLICT(account_id,email) DO UPDATE SET name=CASE WHEN excluded.name<>'' THEN excluded.name ELSE name END`,
			accountID, strings.ToLower(c.Email), c.Name); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) SuggestContacts(q string, limit int) ([]mailtypes.Contact, error) {
	if limit <= 0 {
		limit = 8
	}
	like := "%" + strings.ToLower(q) + "%"
	rows, err := s.db.Query(`SELECT email, MAX(name), SUM(uses) u FROM contacts WHERE lower(email) LIKE ? OR lower(name) LIKE ?
	  GROUP BY email ORDER BY u DESC, email LIMIT ?`, like, like, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []mailtypes.Contact
	for rows.Next() {
		var c mailtypes.Contact
		var u int
		if err := rows.Scan(&c.Email, &c.Name, &u); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---- settings ----

func (s *Store) Setting(k, def string) string {
	var v string
	if err := s.db.QueryRow(`SELECT v FROM settings WHERE k=?`, k).Scan(&v); err != nil {
		return def
	}
	return v
}

func (s *Store) SetSetting(k, v string) error {
	_, err := s.db.Exec(`INSERT INTO settings(k,v) VALUES(?,?) ON CONFLICT(k) DO UPDATE SET v=excluded.v`, k, v)
	return err
}

func (s *Store) SetSignature(accountID, sig string) error {
	_, err := s.db.Exec(`UPDATE accounts SET signature=? WHERE id=?`, sig, accountID)
	return err
}

// resolveThread threads providers that do not do it themselves (IMAP) by
// Message-ID, In-Reply-To and References, the way mail clients have for
// decades: a reply joins the thread of anything it names, and a message that
// others already name takes their thread, merging threads it bridges.
func resolveThread(tx *sql.Tx, m mailtypes.Message) (string, error) {
	seen := map[string]bool{}
	var ids []string
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			ids = append(ids, s)
		}
	}
	add(m.InReplyTo)
	for _, r := range m.References {
		add(r)
	}
	threads := map[string]bool{}
	for _, id := range ids {
		rows, err := tx.Query(`SELECT DISTINCT thread_id FROM messages WHERE account_id=? AND message_id=?`, m.AccountID, id)
		if err != nil {
			return "", err
		}
		for rows.Next() {
			var t string
			_ = rows.Scan(&t)
			threads[t] = true
		}
		rows.Close()
	}
	if m.MessageID != "" {
		rows, err := tx.Query(`SELECT DISTINCT thread_id FROM messages WHERE account_id=? AND (in_reply_to=? OR refs_json LIKE ?)`,
			m.AccountID, m.MessageID, "%"+strings.ReplaceAll(m.MessageID, "%", "")+"%")
		if err != nil {
			return "", err
		}
		for rows.Next() {
			var t string
			_ = rows.Scan(&t)
			threads[t] = true
		}
		rows.Close()
	}
	if len(threads) == 0 {
		key := m.MessageID
		if key == "" {
			key = m.RemoteID
		}
		return m.AccountID + "/t:" + key, nil
	}
	var keep string
	for t := range threads {
		if keep == "" || t < keep {
			keep = t
		}
	}
	for t := range threads {
		if t != keep {
			if _, err := tx.Exec(`UPDATE messages SET thread_id=? WHERE thread_id=?`, keep, t); err != nil {
				return "", err
			}
		}
	}
	return keep, nil
}

// ApplyFlags updates read and starred state, and optionally folders, from a sync pass.
func (s *Store) ApplyFlags(accountID string, fs []mailtypes.FlagUpdate) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, f := range fs {
		id := accountID + "/" + f.RemoteID
		if _, err := tx.Exec(`UPDATE messages SET is_read=?, is_starred=? WHERE id=?`, b2i(f.Read), b2i(f.Starred), id); err != nil {
			return err
		}
		if f.Folders != nil {
			if _, err := tx.Exec(`DELETE FROM msg_folders WHERE msg_id=?`, id); err != nil {
				return err
			}
			for _, fo := range f.Folders {
				if _, err := tx.Exec(`INSERT OR IGNORE INTO msg_folders(msg_id,account_id,folder_remote) VALUES(?,?,?)`, id, accountID, fo); err != nil {
					return err
				}
			}
		}
	}
	return tx.Commit()
}

// ClaimOutbox marks a queued message as sending. It returns false when Undo
// got there first, so exactly one of send and undo wins.
func (s *Store) ClaimOutbox(id string) (bool, error) {
	r, err := s.db.Exec(`UPDATE outbox SET state='sending' WHERE id=? AND state='queued'`, id)
	if err != nil {
		return false, err
	}
	n, _ := r.RowsAffected()
	return n > 0, nil
}

// RecoverOutbox turns messages that were mid send when the app stopped into
// failed ones the person can retry, rather than guessing whether they went.
func (s *Store) RecoverOutbox() error {
	_, err := s.db.Exec(`UPDATE outbox SET state='failed', error='the app closed while this was sending; check Sent before retrying' WHERE state='sending'`)
	return err
}

// ---- drafts ----

type Draft struct {
	ID        string          `json:"id"`
	AccountID string          `json:"accountId"`
	Data      json.RawMessage `json:"data"`
	Subject   string          `json:"subject"`
	Updated   time.Time       `json:"updated"`
}

func (s *Store) PutDraft(d Draft) error {
	_, err := s.db.Exec(`INSERT INTO drafts(id,account_id,data,subject,updated) VALUES(?,?,?,?,?)
	  ON CONFLICT(id) DO UPDATE SET account_id=excluded.account_id,data=excluded.data,subject=excluded.subject,updated=excluded.updated`,
		d.ID, d.AccountID, string(d.Data), d.Subject, time.Now().UnixMilli())
	return err
}

func (s *Store) Drafts() ([]Draft, error) {
	rows, err := s.db.Query(`SELECT id,account_id,data,subject,updated FROM drafts ORDER BY updated DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Draft{}
	for rows.Next() {
		var d Draft
		var data string
		var u int64
		if err := rows.Scan(&d.ID, &d.AccountID, &data, &d.Subject, &u); err != nil {
			return nil, err
		}
		d.Data = json.RawMessage(data)
		d.Updated = time.UnixMilli(u)
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) Draft(id string) (Draft, error) {
	var d Draft
	var data string
	var u int64
	err := s.db.QueryRow(`SELECT id,account_id,data,subject,updated FROM drafts WHERE id=?`, id).Scan(&d.ID, &d.AccountID, &data, &d.Subject, &u)
	d.Data = json.RawMessage(data)
	d.Updated = time.UnixMilli(u)
	return d, err
}

func (s *Store) DeleteDraft(id string) error {
	_, err := s.db.Exec(`DELETE FROM drafts WHERE id=?`, id)
	return err
}

// ---- local address book: contacts for accounts whose server has none ----

func (s *Store) PutLocalContact(accountID string, c mailtypes.Contact) (mailtypes.Contact, error) {
	if c.ID == "" {
		c.ID = "local:" + fmt.Sprint(time.Now().UnixNano())
	}
	_, err := s.db.Exec(`INSERT INTO local_contacts(id,account_id,name,email,phone,org) VALUES(?,?,?,?,?,?)
	  ON CONFLICT(id) DO UPDATE SET name=excluded.name,email=excluded.email,phone=excluded.phone,org=excluded.org`,
		c.ID, accountID, c.Name, c.Email, c.Phone, c.Org)
	return c, err
}

func (s *Store) DeleteLocalContact(id string) error {
	_, err := s.db.Exec(`DELETE FROM local_contacts WHERE id=?`, id)
	return err
}

func (s *Store) LocalContacts(q string) ([]mailtypes.Contact, error) {
	like := "%" + strings.ToLower(q) + "%"
	rows, err := s.db.Query(`SELECT id,name,email,phone,org FROM local_contacts WHERE lower(name) LIKE ? OR lower(email) LIKE ? ORDER BY name`, like, like)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []mailtypes.Contact{}
	for rows.Next() {
		var c mailtypes.Contact
		if err := rows.Scan(&c.ID, &c.Name, &c.Email, &c.Phone, &c.Org); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// KnownMessages reports which of the remote IDs the cache already holds, so a sync can tell mail that has just arrived from mail it is only refreshing.
func (s *Store) KnownMessages(accountID string, remoteIDs []string) map[string]bool {
	known := map[string]bool{}
	for _, r := range remoteIDs {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE id=?`, accountID+"/"+r).Scan(&n); err == nil && n > 0 {
			known[r] = true
		}
	}
	return known
}

// messageSize is the size a message is searched by (larger: and smaller:): the text, the HTML and what is attached.
func messageSize(m mailtypes.Message) int64 {
	if m.Size > 0 {
		return m.Size
	}
	n := int64(len(m.BodyText) + len(m.BodyHTML) + len(m.Subject))
	for _, a := range m.Attach {
		n += a.Size
	}
	return n
}

type extra struct {
	ICS       string `json:"i,omitempty"`
	Unsub     string `json:"u,omitempty"`
	UnsubPost bool   `json:"p,omitempty"`
	Receipt   string `json:"r,omitempty"`
}

func packExtra(m mailtypes.Message) string {
	ics := m.ICS
	if ics == "" {
		for _, a := range m.Attach {
			if mime.IsCalendar(a) {
				ics = "1" // there is an invitation to fetch when the message is opened
				break
			}
		}
	}
	if ics == "" && m.ListUnsubscribe == "" && m.ReceiptTo == "" {
		return ""
	}
	return js(extra{ics, m.ListUnsubscribe, m.ListUnsubscribePost, m.ReceiptTo})
}

func unpackExtra(s string, m *mailtypes.Message) {
	if s == "" {
		return
	}
	var e extra
	if json.Unmarshal([]byte(s), &e) == nil {
		m.ICS, m.ListUnsubscribe, m.ListUnsubscribePost, m.ReceiptTo = e.ICS, e.Unsub, e.UnsubPost, e.Receipt
		if m.ICS == "1" {
			m.ICS = ""
			m.HasInvite = true
		} else if m.ICS != "" {
			m.HasInvite = true
		}
	}
}
