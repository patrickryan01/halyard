package secure

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

func pgpConfig() *packet.Config {
	return &packet.Config{DefaultHash: 0, Time: time.Now}
}

func fp(e *openpgp.Entity) string { return fmt.Sprintf("%X", e.PrimaryKey.Fingerprint) }

func entityEmail(e *openpgp.Entity) (name, email string) {
	for _, id := range e.Identities {
		return id.UserId.Name, id.UserId.Email
	}
	return "", ""
}

func armorPub(e *openpgp.Entity) (string, error) {
	var b bytes.Buffer
	w, err := armor.Encode(&b, openpgp.PublicKeyType, nil)
	if err != nil {
		return "", err
	}
	if err := e.Serialize(w); err != nil {
		return "", err
	}
	w.Close()
	return b.String(), nil
}

func armorPriv(e *openpgp.Entity) (string, error) {
	var b bytes.Buffer
	w, err := armor.Encode(&b, openpgp.PrivateKeyType, nil)
	if err != nil {
		return "", err
	}
	if err := e.SerializePrivate(w, nil); err != nil {
		return "", err
	}
	w.Close()
	return b.String(), nil
}

func toKey(e *openpgp.Entity, withPrivate bool) (PGPKey, error) {
	pub, err := armorPub(e)
	if err != nil {
		return PGPKey{}, err
	}
	n, em := entityEmail(e)
	k := PGPKey{ID: fp(e), Fingerprint: fp(e), Name: n, Email: em, Public: pub}
	if withPrivate && e.PrivateKey != nil {
		if k.Private, err = armorPriv(e); err != nil {
			return PGPKey{}, err
		}
	}
	return k, nil
}

// GeneratePGP makes a new key pair (Curve25519) for a name and address.
func GeneratePGP(name, email string) (PGPKey, error) {
	if email == "" {
		return PGPKey{}, errors.New("an email address is needed")
	}
	cfg := &packet.Config{Algorithm: packet.PubKeyAlgoEdDSA, Time: time.Now}
	e, err := openpgp.NewEntity(name, "", email, cfg)
	if err != nil {
		return PGPKey{}, err
	}
	return toKey(e, true)
}

// ImportPGP reads armored or binary key material: public keys, or secret keys (decrypted with pass when protected).
func ImportPGP(data []byte, pass string) ([]PGPKey, error) {
	var es openpgp.EntityList
	var err error
	if bytes.Contains(data, []byte("-----BEGIN PGP")) {
		es, err = openpgp.ReadArmoredKeyRing(bytes.NewReader(data))
	} else {
		es, err = openpgp.ReadKeyRing(bytes.NewReader(data))
	}
	if err != nil {
		return nil, fmt.Errorf("that is not a PGP key: %w", err)
	}
	var out []PGPKey
	for _, e := range es {
		priv := e.PrivateKey != nil
		if priv && e.PrivateKey.Encrypted {
			if pass == "" {
				return nil, errors.New("this key has a passphrase: enter it to import the key")
			}
			if err := e.PrivateKey.Decrypt([]byte(pass)); err != nil {
				return nil, errors.New("that passphrase does not open the key")
			}
			for _, sk := range e.Subkeys {
				if sk.PrivateKey != nil && sk.PrivateKey.Encrypted {
					if err := sk.PrivateKey.Decrypt([]byte(pass)); err != nil {
						return nil, errors.New("that passphrase does not open the key")
					}
				}
			}
		}
		k, err := toKey(e, priv)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	if len(out) == 0 {
		return nil, errors.New("no key found in that file")
	}
	return out, nil
}

func entity(armored string) (*openpgp.Entity, error) {
	es, err := openpgp.ReadArmoredKeyRing(strings.NewReader(armored))
	if err != nil || len(es) == 0 {
		return nil, errors.New("the stored key cannot be read")
	}
	return es[0], nil
}

func entities(keys ...PGPKey) (openpgp.EntityList, error) {
	var l openpgp.EntityList
	for _, k := range keys {
		src := k.Public
		if k.Private != "" {
			src = k.Private
		}
		e, err := entity(src)
		if err != nil {
			return nil, err
		}
		l = append(l, e)
	}
	return l, nil
}

// pgpSignDetached returns the armored detached signature of data.
func pgpSignDetached(data []byte, k PGPKey) (string, error) {
	e, err := entity(k.Private)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err := openpgp.ArmoredDetachSign(&b, e, bytes.NewReader(data), pgpConfig()); err != nil {
		return "", err
	}
	return b.String(), nil
}

// pgpVerifyDetached checks a signature against the keys known; it returns who signed it.
func pgpVerifyDetached(data []byte, sig string, keys []PGPKey) (PGPKey, error) {
	kr, err := entities(keys...)
	if err != nil {
		return PGPKey{}, err
	}
	signer, err := openpgp.CheckArmoredDetachedSignature(kr, bytes.NewReader(data), strings.NewReader(sig), pgpConfig())
	if err != nil {
		return PGPKey{}, err
	}
	for _, k := range keys {
		if strings.EqualFold(k.Fingerprint, fp(signer)) {
			return k, nil
		}
	}
	n, em := entityEmail(signer)
	return PGPKey{ID: fp(signer), Name: n, Email: em}, nil
}

// pgpEncrypt encrypts data to the recipients, signing it when signer has a private key.
func pgpEncrypt(data []byte, to []PGPKey, signer *PGPKey) (string, error) {
	rcpt, err := entities(to...)
	if err != nil {
		return "", err
	}
	var se *openpgp.Entity
	if signer != nil {
		if se, err = entity(signer.Private); err != nil {
			return "", err
		}
	}
	var b bytes.Buffer
	aw, err := armor.Encode(&b, "PGP MESSAGE", nil)
	if err != nil {
		return "", err
	}
	w, err := openpgp.Encrypt(aw, rcpt, se, &openpgp.FileHints{IsBinary: false}, pgpConfig())
	if err != nil {
		return "", err
	}
	if _, err := w.Write(data); err != nil {
		return "", err
	}
	w.Close()
	aw.Close()
	return b.String(), nil
}

// pgpDecrypt decrypts a message with the person's keys. signed reports whether the data carried a signature, and ok
// whether it checked out against the keys known.
func pgpDecrypt(armored string, keys []PGPKey) (plain []byte, signed, ok bool, signer PGPKey, err error) {
	var priv []PGPKey
	for _, k := range keys {
		if k.HasPrivate() {
			priv = append(priv, k)
		}
	}
	if len(priv) == 0 {
		return nil, false, false, PGPKey{}, errors.New("you have no private key to open this with")
	}
	all, err := entities(keys...) // signers' public keys too
	if err != nil {
		return nil, false, false, PGPKey{}, err
	}
	blk, err := armor.Decode(strings.NewReader(armored))
	if err != nil {
		return nil, false, false, PGPKey{}, err
	}
	md, err := openpgp.ReadMessage(blk.Body, all, nil, pgpConfig())
	if err != nil {
		return nil, false, false, PGPKey{}, errors.New("this message was not encrypted to any key you have")
	}
	plain, err = io.ReadAll(md.UnverifiedBody)
	if err != nil {
		return nil, false, false, PGPKey{}, err
	}
	if md.IsSigned {
		signed = true
		if md.SignatureError == nil && md.SignedBy != nil {
			ok = true
			e := md.SignedBy.Entity
			for _, k := range keys {
				if e != nil && strings.EqualFold(k.Fingerprint, fp(e)) {
					signer = k
				}
			}
		} else if md.SignedBy == nil {
			err = nil // signed by a key we do not hold: reported as unverified
		}
	}
	return plain, signed, ok, signer, nil
}
