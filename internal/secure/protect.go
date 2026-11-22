package secure

import (
	"errors"
	"fmt"
	"strings"
)

// Options say how to protect one outgoing message.
type Options struct {
	Protocol string // "pgp" or "smime"
	Sign     bool
	Encrypt  bool
	From     string
	// Recipients are every address that will receive the message (To, Cc and Bcc), so each can read it.
	Recipients []string
}

// Protect takes a finished message (headers and MIME body as the composer built them) and returns the signed and/or
// encrypted version. It fails, naming the people, when a recipient has no key.
func Protect(raw []byte, o Options, kr *Keyring) ([]byte, error) {
	if !o.Sign && !o.Encrypt {
		return raw, nil
	}
	e := splitEntity(raw)
	inner := e.content()
	switch o.Protocol {
	case "pgp":
		return protectPGP(e, inner, o, kr)
	case "smime":
		return protectSMIME(e, inner, o, kr)
	}
	return nil, errors.New("choose OpenPGP or S/MIME")
}

func protectPGP(e entityParts, inner []byte, o Options, kr *Keyring) ([]byte, error) {
	var signer *PGPKey
	if o.Sign {
		k, ok := kr.OwnPGP(o.From)
		if !ok {
			return nil, fmt.Errorf("you have no OpenPGP key for %s: make or import one under Security", o.From)
		}
		signer = &k
	}
	if !o.Encrypt {
		sig, err := pgpSignDetached(inner, *signer)
		if err != nil {
			return nil, err
		}
		b := boundary()
		var body strings.Builder
		body.WriteString("--" + b + "\r\n")
		body.Write(inner)
		body.WriteString("\r\n--" + b + "\r\nContent-Type: application/pgp-signature; name=\"signature.asc\"\r\nContent-Description: OpenPGP digital signature\r\n\r\n")
		body.WriteString(strings.ReplaceAll(sig, "\n", "\r\n"))
		body.WriteString("\r\n--" + b + "--\r\n")
		return assemble(e.outer(), []string{`Content-Type: multipart/signed; protocol="application/pgp-signature"; micalg=pgp-sha256; boundary="` + b + `"`}, []byte(body.String())), nil
	}
	var to []PGPKey
	var missing []string
	seen := map[string]bool{}
	for _, r := range append([]string{o.From}, o.Recipients...) { // the sender can read their own copy
		if seen[strings.ToLower(r)] {
			continue
		}
		seen[strings.ToLower(r)] = true
		k, ok := kr.PGPFor(r)
		if !ok {
			missing = append(missing, r)
			continue
		}
		to = append(to, k)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("no OpenPGP key for %s: import their public key under Security", strings.Join(missing, ", "))
	}
	enc, err := pgpEncrypt(inner, to, signer)
	if err != nil {
		return nil, err
	}
	b := boundary()
	var body strings.Builder
	body.WriteString("--" + b + "\r\nContent-Type: application/pgp-encrypted\r\nContent-Description: PGP/MIME version identification\r\n\r\nVersion: 1\r\n\r\n")
	body.WriteString("--" + b + "\r\nContent-Type: application/octet-stream; name=\"encrypted.asc\"\r\nContent-Description: OpenPGP encrypted message\r\nContent-Disposition: inline; filename=\"encrypted.asc\"\r\n\r\n")
	body.WriteString(strings.ReplaceAll(enc, "\n", "\r\n"))
	body.WriteString("\r\n--" + b + "--\r\n")
	return assemble(e.outer(), []string{`Content-Type: multipart/encrypted; protocol="application/pgp-encrypted"; boundary="` + b + `"`}, []byte(body.String())), nil
}

func protectSMIME(e entityParts, inner []byte, o Options, kr *Keyring) ([]byte, error) {
	entity := inner
	var ch []string
	var body []byte
	if o.Sign {
		k, ok := kr.OwnSMIME(o.From)
		if !ok {
			return nil, fmt.Errorf("you have no S/MIME certificate for %s: make or import one under Security", o.From)
		}
		sig, err := smimeSignDetached(inner, k)
		if err != nil {
			return nil, err
		}
		b := boundary()
		var sb strings.Builder
		sb.WriteString("--" + b + "\r\n")
		sb.Write(inner)
		sb.WriteString("\r\n--" + b + "\r\nContent-Type: application/pkcs7-signature; name=\"smime.p7s\"\r\nContent-Transfer-Encoding: base64\r\nContent-Disposition: attachment; filename=\"smime.p7s\"\r\nContent-Description: S/MIME cryptographic signature\r\n\r\n")
		sb.WriteString(b64(sig))
		sb.WriteString("--" + b + "--\r\n")
		ch = []string{`Content-Type: multipart/signed; protocol="application/pkcs7-signature"; micalg=sha-256; boundary="` + b + `"`}
		body = []byte(sb.String())
		entity = append([]byte(ch[0]+"\r\n\r\n"), body...)
	}
	if o.Encrypt {
		var to []SMIMEKey
		var missing []string
		seen := map[string]bool{}
		for _, r := range append([]string{o.From}, o.Recipients...) {
			if seen[strings.ToLower(r)] {
				continue
			}
			seen[strings.ToLower(r)] = true
			k, ok := kr.SMIMEFor(r)
			if !ok {
				missing = append(missing, r)
				continue
			}
			to = append(to, k)
		}
		if len(missing) > 0 {
			return nil, fmt.Errorf("no S/MIME certificate for %s: import their certificate under Security (it comes with any message they sign)", strings.Join(missing, ", "))
		}
		der, err := smimeEncrypt(entity, to)
		if err != nil {
			return nil, err
		}
		ch = []string{`Content-Type: application/pkcs7-mime; smime-type=enveloped-data; name="smime.p7m"`, "Content-Transfer-Encoding: base64", `Content-Disposition: attachment; filename="smime.p7m"`}
		body = []byte(b64(der))
	}
	return assemble(e.outer(), ch, body), nil
}
