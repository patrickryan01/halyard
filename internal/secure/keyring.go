// Package secure signs, encrypts, verifies and decrypts mail with OpenPGP (RFC 3156) and S/MIME (RFC 8551), and keeps
// the keys and certificates they need.
package secure

import (
	"encoding/json"
	"errors"
	"strings"
)

// PGPKey is one OpenPGP key pair, or a correspondent's public key when Private is empty.
type PGPKey struct {
	ID          string `json:"id"` // the fingerprint
	Name        string `json:"name"`
	Email       string `json:"email"`
	Public      string `json:"public"`            // armored
	Private     string `json:"private,omitempty"` // armored, never encrypted: the keyring itself is kept in the secret store
	Fingerprint string `json:"fingerprint"`
}

// SMIMEKey is one S/MIME certificate, with its private key when it is the person's own.
type SMIMEKey struct {
	ID    string `json:"id"` // serial number, hex
	Name  string `json:"name"`
	Email string `json:"email"`
	Cert  string `json:"cert"`            // PEM
	Key   string `json:"key,omitempty"`   // PKCS 8 PEM
	Chain string `json:"chain,omitempty"` // PEM, intermediates
	// Expires is the end of validity, RFC 3339.
	Expires string `json:"expires"`
	Self    bool   `json:"selfSigned"`
}

// Keyring is everything this person holds.
type Keyring struct {
	PGP   []PGPKey   `json:"pgp"`
	SMIME []SMIMEKey `json:"smime"`
}

func (k *Keyring) Marshal() []byte { b, _ := json.Marshal(k); return b }

func ParseKeyring(b []byte) (*Keyring, error) {
	k := &Keyring{PGP: []PGPKey{}, SMIME: []SMIMEKey{}}
	if len(b) == 0 {
		return k, nil
	}
	if err := json.Unmarshal(b, k); err != nil {
		return nil, err
	}
	if k.PGP == nil {
		k.PGP = []PGPKey{}
	}
	if k.SMIME == nil {
		k.SMIME = []SMIMEKey{}
	}
	return k, nil
}

// Public is the keyring without any private key, for showing in the window.
func (k *Keyring) Public() *Keyring {
	out := &Keyring{PGP: []PGPKey{}, SMIME: []SMIMEKey{}}
	for _, p := range k.PGP {
		p.Private = ""
		out.PGP = append(out.PGP, p)
	}
	for _, s := range k.SMIME {
		s.Key = ""
		out.SMIME = append(out.SMIME, s)
	}
	return out
}

// HasPrivate says whether the key pair can sign and decrypt.
func (p PGPKey) HasPrivate() bool   { return p.Private != "" }
func (s SMIMEKey) HasPrivate() bool { return s.Key != "" }

func (k *Keyring) AddPGP(p PGPKey) {
	for i, x := range k.PGP {
		if x.ID == p.ID {
			if x.Private != "" && p.Private == "" {
				p.Private = x.Private
			}
			k.PGP[i] = p
			return
		}
	}
	k.PGP = append(k.PGP, p)
}

func (k *Keyring) AddSMIME(s SMIMEKey) {
	for i, x := range k.SMIME {
		if x.ID == s.ID {
			if x.Key != "" && s.Key == "" {
				s.Key = x.Key
			}
			k.SMIME[i] = s
			return
		}
	}
	k.SMIME = append(k.SMIME, s)
}

func (k *Keyring) Remove(id string) bool {
	n := false
	var pg []PGPKey
	for _, p := range k.PGP {
		if p.ID == id {
			n = true
			continue
		}
		pg = append(pg, p)
	}
	var sm []SMIMEKey
	for _, s := range k.SMIME {
		if s.ID == id {
			n = true
			continue
		}
		sm = append(sm, s)
	}
	k.PGP, k.SMIME = pg, sm
	if k.PGP == nil {
		k.PGP = []PGPKey{}
	}
	if k.SMIME == nil {
		k.SMIME = []SMIMEKey{}
	}
	return n
}

// OwnPGP finds the person's own key pair for an address.
func (k *Keyring) OwnPGP(email string) (PGPKey, bool) {
	for _, p := range k.PGP {
		if p.HasPrivate() && strings.EqualFold(p.Email, email) {
			return p, true
		}
	}
	return PGPKey{}, false
}

func (k *Keyring) OwnSMIME(email string) (SMIMEKey, bool) {
	for _, s := range k.SMIME {
		if s.HasPrivate() && strings.EqualFold(s.Email, email) {
			return s, true
		}
	}
	return SMIMEKey{}, false
}

// PGPFor and SMIMEFor find what is needed to write to someone: their public key or certificate.
func (k *Keyring) PGPFor(email string) (PGPKey, bool) {
	for _, p := range k.PGP {
		if strings.EqualFold(p.Email, email) {
			return p, true
		}
	}
	return PGPKey{}, false
}

func (k *Keyring) SMIMEFor(email string) (SMIMEKey, bool) {
	for _, s := range k.SMIME {
		if strings.EqualFold(s.Email, email) {
			return s, true
		}
	}
	return SMIMEKey{}, false
}

var ErrNoKey = errors.New("no key")
