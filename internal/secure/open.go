package secure

import (
	"bytes"
	"errors"
	"strings"

	"github.com/smallstep/pkcs7"
)

// Signer says who signed.
type Signer struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	ID    string `json:"id"`
}

// Result is what is known about a message's protection.
type Result struct {
	Protocol  string `json:"protocol,omitempty"` // pgp or smime; empty for a plain message
	Encrypted bool   `json:"encrypted"`
	Decrypted bool   `json:"decrypted"`
	Signed    bool   `json:"signed"`
	Valid     bool   `json:"valid"`   // the signature checks out
	Trusted   bool   `json:"trusted"` // and the signer is known or vouched for
	Signer    Signer `json:"signer"`
	Problem   string `json:"problem,omitempty"`
	// Inner is the readable message (Content headers and body) when there was something to open.
	Inner []byte `json:"-"`
	// Signature and Certificate material the person may want to keep.
	SignerKey *SMIMEKey `json:"-"`
	SignerPGP *PGPKey   `json:"-"`
}

// Inspect reports what protection a message claims without opening it.
func Inspect(raw []byte) Result {
	e := splitEntity(raw)
	ct, params := mediaType(e.get("Content-Type"))
	var r Result
	switch {
	case ct == "multipart/signed" && strings.Contains(params["protocol"], "pgp"):
		r.Protocol, r.Signed = "pgp", true
	case ct == "multipart/signed":
		r.Protocol, r.Signed = "smime", true
	case ct == "multipart/encrypted":
		r.Protocol, r.Encrypted = "pgp", true
	case ct == "application/pkcs7-mime" || ct == "application/x-pkcs7-mime":
		r.Protocol = "smime"
		if strings.Contains(strings.ToLower(params["smime-type"]), "signed") {
			r.Signed = true
		} else {
			r.Encrypted = true
		}
	}
	return r
}

// Open decrypts and verifies what it can. It never returns an error for a bad signature: the answer is in the Result.
func Open(raw []byte, kr *Keyring) Result {
	e := splitEntity(raw)
	ct, params := mediaType(e.get("Content-Type"))
	r := Inspect(raw)
	switch {
	case ct == "multipart/signed":
		parts, err := splitMultipart(e.body, params["boundary"])
		if err != nil || len(parts) < 2 {
			r.Problem = "the signed message is not built correctly"
			return r
		}
		signed := parts[0]
		sigEntity := splitEntity(parts[1])
		sigBody, err := decodeBody(sigEntity.get("Content-Transfer-Encoding"), sigEntity.body)
		if err != nil {
			r.Problem = "the signature cannot be read"
			return r
		}
		r.Inner = signed
		if r.Protocol == "pgp" {
			k, err := pgpVerifyDetached(signed, string(sigBody), kr.PGP)
			if err != nil {
				r.Problem = verifyProblem(err)
				return r
			}
			r.Valid, r.Trusted = true, k.Public != ""
			r.Signer = Signer{k.Name, k.Email, k.ID}
			if k.Public == "" {
				r.Problem = "signed by a key you do not have"
				r.Valid = false
			}
			r.SignerPGP = &k
			return r
		}
		k, trusted, err := smimeVerifyDetached(signed, sigBody, kr.SMIME)
		if err != nil {
			r.Problem = verifyProblem(err)
			return r
		}
		r.Valid, r.Trusted = true, trusted
		r.Signer = Signer{k.Name, k.Email, k.ID}
		r.SignerKey = &k
		if !trusted {
			r.Problem = "the signature is valid, but the certificate is not vouched for by a known authority: keep it to trust this sender"
		}
		return r
	case ct == "multipart/encrypted":
		parts, err := splitMultipart(e.body, params["boundary"])
		if err != nil || len(parts) < 2 {
			r.Problem = "the encrypted message is not built correctly"
			return r
		}
		data := splitEntity(parts[1])
		plain, signed, ok, signer, err := pgpDecrypt(string(data.body), kr.PGP)
		if err != nil {
			r.Problem = err.Error()
			return r
		}
		r.Decrypted, r.Inner = true, plain
		r.Signed, r.Valid = signed, ok
		if ok {
			r.Trusted = true
			r.Signer = Signer{signer.Name, signer.Email, signer.ID}
		} else if signed {
			r.Problem = "the message is signed, but with a key you do not have"
		}
		return r
	case ct == "application/pkcs7-mime" || ct == "application/x-pkcs7-mime":
		der, err := decodeBody(e.get("Content-Transfer-Encoding"), e.body)
		if err != nil {
			r.Problem = "the message cannot be read"
			return r
		}
		if r.Signed { // opaque signing: the content is inside
			p7, err := pkcs7.Parse(der)
			if err != nil {
				r.Problem = verifyProblem(err)
				return r
			}
			if err := p7.Verify(); err != nil {
				r.Problem = verifyProblem(err)
				return r
			}
			r.Inner = p7.Content
			if c := p7.GetOnlySigner(); c != nil {
				k, _ := toSMIME(c, nil, nil)
				r.Valid, r.Signer, r.SignerKey = true, Signer{k.Name, k.Email, k.ID}, &k
				for _, kk := range kr.SMIME {
					if kk.ID == k.ID {
						r.Trusted = true
					}
				}
			}
			return r
		}
		plain, err := smimeDecrypt(der, kr.SMIME)
		if err != nil {
			r.Problem = err.Error()
			return r
		}
		r.Decrypted = true
		// the content may itself be a signed message
		if in := Inspect(plain); in.Signed {
			sub := Open(plain, kr)
			sub.Encrypted, sub.Decrypted = true, true
			if sub.Inner == nil {
				sub.Inner = plain
			}
			return sub
		}
		r.Inner = plain
		return r
	}
	return r
}

func verifyProblem(err error) string {
	if err == nil {
		return ""
	}
	if bytes.Contains([]byte(err.Error()), []byte("key")) && bytes.Contains([]byte(err.Error()), []byte("unknown")) {
		return "signed by a key you do not have"
	}
	return "the signature does not match the message: it was changed after it was signed, or signed with a key you do not have (" + err.Error() + ")"
}

var _ = errors.New
