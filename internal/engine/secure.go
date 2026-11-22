package engine

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/patrickryan01/halyard/internal/mime"
	"github.com/patrickryan01/halyard/internal/mailtypes"
	"github.com/patrickryan01/halyard/internal/plugin"
	"github.com/patrickryan01/halyard/internal/secure"
)

const keyringSecret = "keyring"

var keyMu sync.Mutex

func (e *Engine) loadKeyring() (*secure.Keyring, error) {
	b, err := e.Sec.Get(keyringSecret)
	if err != nil {
		return secure.ParseKeyring(nil)
	}
	return secure.ParseKeyring(b)
}

func (e *Engine) saveKeyring(k *secure.Keyring) error { return e.Sec.Put(keyringSecret, k.Marshal()) }

// Keyring returns what is held, without any private key.
func (e *Engine) Keyring() (*secure.Keyring, error) {
	keyMu.Lock()
	defer keyMu.Unlock()
	k, err := e.loadKeyring()
	if err != nil {
		return nil, err
	}
	return k.Public(), nil
}

// MakeKey creates a key pair or certificate for an address. kind is "pgp" or "smime".
func (e *Engine) MakeKey(kind, name, email string) error {
	keyMu.Lock()
	defer keyMu.Unlock()
	k, err := e.loadKeyring()
	if err != nil {
		return err
	}
	switch kind {
	case "pgp":
		p, err := secure.GeneratePGP(name, email)
		if err != nil {
			return err
		}
		k.AddPGP(p)
	case "smime":
		s, err := secure.GenerateSMIME(name, email)
		if err != nil {
			return err
		}
		k.AddSMIME(s)
	default:
		return errors.New("choose OpenPGP or S/MIME")
	}
	return e.saveKeyring(k)
}

// ImportKey adds keys or certificates from a file's contents. pass opens a protected private key or a .p12 file.
func (e *Engine) ImportKey(kind string, data []byte, pass string) (int, error) {
	keyMu.Lock()
	defer keyMu.Unlock()
	k, err := e.loadKeyring()
	if err != nil {
		return 0, err
	}
	n := 0
	switch kind {
	case "pgp":
		ks, err := secure.ImportPGP(data, pass)
		if err != nil {
			return 0, err
		}
		for _, x := range ks {
			k.AddPGP(x)
			n++
		}
	case "smime":
		ks, err := secure.ImportSMIME(data, pass)
		if err != nil {
			return 0, err
		}
		for _, x := range ks {
			k.AddSMIME(x)
			n++
		}
	default:
		return 0, errors.New("choose OpenPGP or S/MIME")
	}
	return n, e.saveKeyring(k)
}

func (e *Engine) RemoveKey(id string) error {
	keyMu.Lock()
	defer keyMu.Unlock()
	k, err := e.loadKeyring()
	if err != nil {
		return err
	}
	if !k.Remove(id) {
		return errors.New("no such key")
	}
	return e.saveKeyring(k)
}

// ExportKey returns the public half of a key as text to share: armored for OpenPGP, PEM for a certificate.
func (e *Engine) ExportKey(id string) (string, string, error) {
	keyMu.Lock()
	defer keyMu.Unlock()
	k, err := e.loadKeyring()
	if err != nil {
		return "", "", err
	}
	for _, p := range k.PGP {
		if p.ID == id {
			return p.Public, "public-key.asc", nil
		}
	}
	for _, s := range k.SMIME {
		if s.ID == id {
			return s.Cert, "certificate.pem", nil
		}
	}
	return "", "", errors.New("no such key")
}

// protect builds the finished, signed or encrypted message for an outgoing one.
func (e *Engine) protect(o mailtypes.Outgoing) ([]byte, error) {
	keyMu.Lock()
	k, err := e.loadKeyring()
	keyMu.Unlock()
	if err != nil {
		return nil, err
	}
	domain := "localhost"
	if i := strings.LastIndex(o.From.Email, "@"); i >= 0 {
		domain = o.From.Email[i+1:]
	}
	plain := o
	plain.Raw = nil
	raw, err := mime.Build(plain, mime.MessageID(domain, "s"))
	if err != nil {
		return nil, err
	}
	var rcpt []string
	for _, a := range append(append(append([]mailtypes.Address{}, o.To...), o.Cc...), o.Bcc...) {
		rcpt = append(rcpt, a.Email)
	}
	proto := o.Protect
	if proto == "" {
		proto = "pgp"
		if _, ok := k.OwnSMIME(o.From.Email); ok {
			if _, pg := k.OwnPGP(o.From.Email); !pg {
				proto = "smime"
			}
		}
	}
	return secure.Protect(raw, secure.Options{Protocol: proto, Sign: o.Sign, Encrypt: o.Encrypt, From: o.From.Email, Recipients: rcpt}, k)
}

// RawMessage returns a message exactly as it was sent, from the provider.
func (e *Engine) RawMessage(ctx context.Context, messageID string) ([]byte, mailtypes.Message, error) {
	m, err := e.St.Message(messageID)
	if err != nil {
		return nil, m, errors.New("no such message")
	}
	p, _, err := e.provider(m.AccountID)
	if err != nil {
		return nil, m, err
	}
	defer p.Close()
	rf, ok := p.(plugin.RawFetcher)
	if !ok {
		return nil, m, errors.New("this kind of account cannot return the original message")
	}
	raw, err := rf.Raw(ctx, m.RemoteID)
	return raw, m, err
}

// SecurityView is a protected message, opened.
type SecurityView struct {
	secure.Result
	// SignerMatchesFrom is false when the signature is good but made by someone other than the sender.
	SignerMatchesFrom bool               `json:"signerMatchesFrom"`
	Text              string             `json:"text,omitempty"`
	HTML              string             `json:"html,omitempty"`
	Attachments       []mailtypes.Attachment `json:"attachments,omitempty"`
}

// Security opens a signed or encrypted message: it checks the signature and decrypts what it can.
func (e *Engine) Security(ctx context.Context, messageID string) (SecurityView, error) {
	raw, m, err := e.RawMessage(ctx, messageID)
	if err != nil {
		return SecurityView{}, err
	}
	keyMu.Lock()
	k, err := e.loadKeyring()
	keyMu.Unlock()
	if err != nil {
		return SecurityView{}, err
	}
	r := secure.Open(raw, k)
	v := SecurityView{Result: r}
	v.SignerMatchesFrom = r.Signer.Email != "" && strings.EqualFold(r.Signer.Email, m.From.Email)
	if len(r.Inner) > 0 {
		if pm, err := mime.Parse(r.Inner); err == nil {
			v.Text, v.HTML, v.Attachments = pm.Text, pm.HTML, pm.Attach
		}
	}
	return v, nil
}

// SecurityAttachment returns an attachment of a decrypted message.
func (e *Engine) SecurityAttachment(ctx context.Context, messageID, attID string) ([]byte, string, error) {
	raw, _, err := e.RawMessage(ctx, messageID)
	if err != nil {
		return nil, "", err
	}
	keyMu.Lock()
	k, err := e.loadKeyring()
	keyMu.Unlock()
	if err != nil {
		return nil, "", err
	}
	r := secure.Open(raw, k)
	pm, err := mime.Parse(r.Inner)
	if err != nil {
		return nil, "", err
	}
	for i, a := range pm.Attach {
		if a.ID == attID {
			return pm.Parts[i], a.ContentType, nil
		}
	}
	return nil, "", errors.New("attachment not found")
}

// KeepSigner stores the key or certificate that signed a message, so later mail from that sender is trusted and can be
// answered encrypted.
func (e *Engine) KeepSigner(ctx context.Context, messageID string) error {
	raw, _, err := e.RawMessage(ctx, messageID)
	if err != nil {
		return err
	}
	keyMu.Lock()
	defer keyMu.Unlock()
	k, err := e.loadKeyring()
	if err != nil {
		return err
	}
	r := secure.Open(raw, k)
	switch {
	case r.SignerKey != nil:
		k.AddSMIME(*r.SignerKey)
	case r.SignerPGP != nil && r.SignerPGP.Public != "":
		k.AddPGP(*r.SignerPGP)
	default:
		return errors.New("this message carries no signer to keep")
	}
	return e.saveKeyring(k)
}
