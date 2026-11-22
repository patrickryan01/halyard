package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/patrickryan01/halyard/internal/mailtypes"
)

// ---- rules ----

func (s *Store) Rules() ([]mailtypes.Rule, error) {
	rows, err := s.db.Query(`SELECT id, account_id, data FROM rules ORDER BY pos, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []mailtypes.Rule{}
	for rows.Next() {
		var r mailtypes.Rule
		var acct, data string
		if err := rows.Scan(&r.ID, &acct, &data); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(data), &r); err != nil {
			continue
		}
		r.AccountID = acct
		out = append(out, r)
	}
	return out, rows.Err()
}

// PutRule adds a rule at the end, or replaces the one with the same ID.
func (s *Store) PutRule(r mailtypes.Rule) (mailtypes.Rule, error) {
	if r.Match != "any" {
		r.Match = "all"
	}
	if r.Conditions == nil {
		r.Conditions = []mailtypes.RuleCondition{}
	}
	if r.Actions == nil {
		r.Actions = []mailtypes.RuleAction{}
	}
	if r.ID == "" {
		r.ID = newRandID()
		var pos int
		_ = s.db.QueryRow(`SELECT COALESCE(MAX(pos),0)+1 FROM rules`).Scan(&pos)
		_, err := s.db.Exec(`INSERT INTO rules(id,account_id,pos,data) VALUES(?,?,?,?)`, r.ID, r.AccountID, pos, js(r))
		return r, err
	}
	res, err := s.db.Exec(`UPDATE rules SET account_id=?, data=? WHERE id=?`, r.AccountID, js(r), r.ID)
	if err != nil {
		return r, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return r, errors.New("no such rule")
	}
	return r, nil
}

func (s *Store) DeleteRule(id string) error {
	_, err := s.db.Exec(`DELETE FROM rules WHERE id=?`, id)
	return err
}

// MoveRule puts the rule at position index (0 is first); rules run in that order.
func (s *Store) MoveRule(id string, index int) error {
	rs, err := s.Rules()
	if err != nil {
		return err
	}
	var ids []string
	for _, r := range rs {
		if r.ID != id {
			ids = append(ids, r.ID)
		}
	}
	if index < 0 {
		index = 0
	}
	if index > len(ids) {
		index = len(ids)
	}
	ids = append(ids[:index], append([]string{id}, ids[index:]...)...)
	for i, x := range ids {
		if _, err := s.db.Exec(`UPDATE rules SET pos=? WHERE id=?`, i+1, x); err != nil {
			return err
		}
	}
	return nil
}

// ---- templates ----

func (s *Store) Templates() ([]mailtypes.Template, error) {
	rows, err := s.db.Query(`SELECT id,name,subject,body FROM templates ORDER BY LOWER(name)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []mailtypes.Template{}
	for rows.Next() {
		var t mailtypes.Template
		if err := rows.Scan(&t.ID, &t.Name, &t.Subject, &t.Body); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) PutTemplate(t mailtypes.Template) (mailtypes.Template, error) {
	if t.Name == "" {
		return t, errors.New("a template needs a name")
	}
	if t.ID == "" {
		t.ID = newRandID()
	}
	_, err := s.db.Exec(`INSERT INTO templates(id,name,subject,body) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name, subject=excluded.subject, body=excluded.body`, t.ID, t.Name, t.Subject, t.Body)
	return t, err
}

func (s *Store) DeleteTemplate(id string) error {
	_, err := s.db.Exec(`DELETE FROM templates WHERE id=?`, id)
	return err
}

// ---- saved searches ----

func (s *Store) SavedSearches() ([]mailtypes.SavedSearch, error) {
	rows, err := s.db.Query(`SELECT id,name,query FROM saved_searches ORDER BY LOWER(name)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []mailtypes.SavedSearch{}
	for rows.Next() {
		var t mailtypes.SavedSearch
		if err := rows.Scan(&t.ID, &t.Name, &t.Query); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) PutSavedSearch(t mailtypes.SavedSearch) (mailtypes.SavedSearch, error) {
	if t.Name == "" || t.Query == "" {
		return t, errors.New("a saved search needs a name and a search")
	}
	if t.ID == "" {
		t.ID = newRandID()
	}
	_, err := s.db.Exec(`INSERT INTO saved_searches(id,name,query) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name, query=excluded.query`, t.ID, t.Name, t.Query)
	return t, err
}

func (s *Store) DeleteSavedSearch(id string) error {
	_, err := s.db.Exec(`DELETE FROM saved_searches WHERE id=?`, id)
	return err
}

// ---- identities ----

func (s *Store) Identities(accountID string) ([]mailtypes.Identity, error) {
	q := `SELECT id,account_id,name,email,reply_to,signature,is_default,alias,source FROM identities`
	var args []any
	if accountID != "" {
		q += ` WHERE account_id=?`
		args = append(args, accountID)
	}
	rows, err := s.db.Query(q+` ORDER BY account_id, is_default DESC, alias, email`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []mailtypes.Identity{}
	for rows.Next() {
		var i mailtypes.Identity
		var d, a int
		if err := rows.Scan(&i.ID, &i.AccountID, &i.Name, &i.Email, &i.ReplyTo, &i.Signature, &d, &a, &i.Source); err != nil {
			return nil, err
		}
		i.Default, i.Alias = d == 1, a == 1
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *Store) PutIdentity(i mailtypes.Identity) (mailtypes.Identity, error) {
	if i.Email == "" {
		return i, errors.New("an identity needs an email address")
	}
	if i.Source == "" {
		i.Source = "user"
	}
	if i.ID == "" {
		i.ID = newRandID()
	}
	_, err := s.db.Exec(`INSERT INTO identities(id,account_id,name,email,reply_to,signature,is_default,alias,source) VALUES(?,?,?,?,?,?,?,?,?)
	  ON CONFLICT(id) DO UPDATE SET name=excluded.name, email=excluded.email, reply_to=excluded.reply_to, signature=excluded.signature, is_default=excluded.is_default, alias=excluded.alias`,
		i.ID, i.AccountID, i.Name, i.Email, i.ReplyTo, i.Signature, b2i(i.Default), b2i(i.Alias), i.Source)
	if err == nil && i.Default {
		_, err = s.db.Exec(`UPDATE identities SET is_default=0 WHERE account_id=? AND id<>?`, i.AccountID, i.ID)
	}
	return i, err
}

func (s *Store) DeleteIdentity(id string) error {
	_, err := s.db.Exec(`DELETE FROM identities WHERE id=?`, id)
	return err
}

// ReplaceProviderIdentities swaps in what the server reports. A signature or name changed here is kept for an address
// that was already known.
func (s *Store) ReplaceProviderIdentities(accountID string, ids []mailtypes.Identity) error {
	have, err := s.Identities(accountID)
	if err != nil {
		return err
	}
	byEmail := map[string]mailtypes.Identity{}
	for _, h := range have {
		byEmail[h.Email] = h
	}
	seen := map[string]bool{}
	for _, n := range ids {
		n.AccountID, n.Source = accountID, "provider"
		if old, ok := byEmail[n.Email]; ok {
			n.ID = old.ID
			if old.Signature != "" {
				n.Signature = old.Signature
			}
			if old.Source == "user" {
				n.Source = "user"
			}
		} else {
			n.ID = ""
		}
		seen[n.Email] = true
		if _, err := s.PutIdentity(n); err != nil {
			return err
		}
	}
	for _, h := range have {
		if h.Source == "provider" && !seen[h.Email] {
			if err := s.DeleteIdentity(h.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// ---- helpers used by the engine ----

// MessagesInFolder lists the cached messages of one folder (remote ID) with their IDs, newest first, for bulk actions.
func (s *Store) MessagesInFolder(accountID, folderRemote string, onlyUnread bool) ([]mailtypes.Message, error) {
	q := `SELECT ` + msgCols + ` FROM messages WHERE id IN (SELECT msg_id FROM msg_folders WHERE account_id=? AND folder_remote=?)`
	if onlyUnread {
		q += ` AND is_read=0`
	}
	rows, err := s.db.Query(q+` ORDER BY date DESC`, accountID, folderRemote)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []mailtypes.Message
	for rows.Next() {
		m, err := scanMsg(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func newRandID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// PutRaw keeps the original text of an imported message, so its attachments can be read later.
func (s *Store) PutRaw(accountID, remoteID string, raw []byte) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO raw_messages(account_id, remote_id, data) VALUES(?,?,?)`, accountID, remoteID, raw)
	return err
}

func (s *Store) Raw(accountID, remoteID string) ([]byte, error) {
	var b []byte
	err := s.db.QueryRow(`SELECT data FROM raw_messages WHERE account_id=? AND remote_id=?`, accountID, remoteID).Scan(&b)
	return b, err
}
