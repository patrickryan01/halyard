package secure

import (
	"strings"
	"testing"

	"github.com/patrickryan01/halyard/internal/mailtypes"
	"github.com/patrickryan01/halyard/internal/mime"
)

func base(t *testing.T) []byte {
	t.Helper()
	raw, err := mime.Build(mailtypes.Outgoing{From: mailtypes.Address{Name: "Ana", Email: "ana@x.test"}, To: []mailtypes.Address{{Email: "bo@y.test"}}, Subject: "Plan",
		BodyText: "the secret plan\nfrom ünïcode", BodyHTML: "<p>the secret plan</p>",
		Attachments: []mailtypes.OutAttachment{{Filename: "n.txt", ContentType: "text/plain", Data: []byte("attached")}}}, "<1@x.test>")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func ring(t *testing.T) (*Keyring, *Keyring) {
	t.Helper()
	ana, bo := &Keyring{}, &Keyring{}
	ak, err := GeneratePGP("Ana", "ana@x.test")
	if err != nil {
		t.Fatal(err)
	}
	bk, err := GeneratePGP("Bo", "bo@y.test")
	if err != nil {
		t.Fatal(err)
	}
	ana.AddPGP(ak)
	bo.AddPGP(bk)
	// each learns the other's public key only
	apub, bpub := ak, bk
	apub.Private, bpub.Private = "", ""
	ana.AddPGP(bpub)
	bo.AddPGP(apub)
	as, err := GenerateSMIME("Ana", "ana@x.test")
	if err != nil {
		t.Fatal(err)
	}
	bs, err := GenerateSMIME("Bo", "bo@y.test")
	if err != nil {
		t.Fatal(err)
	}
	ana.AddSMIME(as)
	bo.AddSMIME(bs)
	asp, bsp := as, bs
	asp.Key, bsp.Key = "", ""
	ana.AddSMIME(bsp)
	bo.AddSMIME(asp)
	return ana, bo
}

func innerText(t *testing.T, r Result) string {
	t.Helper()
	pm, err := mime.Parse(r.Inner)
	if err != nil {
		t.Fatal(err)
	}
	return pm.Text
}

func TestPGPSignVerifyAndTamper(t *testing.T) {
	ana, bo := ring(t)
	signed, err := Protect(base(t), Options{Protocol: "pgp", Sign: true, From: "ana@x.test", Recipients: []string{"bo@y.test"}}, ana)
	if err != nil {
		t.Fatal(err)
	}
	if in := Inspect(signed); !in.Signed || in.Protocol != "pgp" || in.Encrypted {
		t.Fatalf("inspect: %+v", in)
	}
	r := Open(signed, bo)
	if !r.Valid || !r.Trusted || r.Signer.Email != "ana@x.test" || !strings.Contains(innerText(t, r), "secret plan") {
		t.Fatalf("%+v", r)
	}
	// the message is still a message a normal client can read: the parse sees the text and the attachment
	if pm, _ := mime.Parse(signed); !strings.Contains(pm.Text, "secret plan") {
		t.Fatal("a signed message is not readable without checking it")
	}
	bad := []byte(strings.Replace(string(signed), "secret plan", "public plan", 1))
	if r := Open(bad, bo); r.Valid || r.Problem == "" {
		t.Fatalf("a changed message passed: %+v", r)
	}
	// a stranger's key: signature cannot be checked
	if r := Open(signed, &Keyring{}); r.Valid || r.Problem == "" {
		t.Fatalf("an unknown signer passed: %+v", r)
	}
}

func TestPGPEncryptDecrypt(t *testing.T) {
	ana, bo := ring(t)
	enc, err := Protect(base(t), Options{Protocol: "pgp", Sign: true, Encrypt: true, From: "ana@x.test", Recipients: []string{"bo@y.test"}}, ana)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(enc), "secret plan") {
		t.Fatal("the plain text is in the encrypted message")
	}
	r := Open(enc, bo)
	if !r.Decrypted || !r.Signed || !r.Valid || r.Signer.Email != "ana@x.test" || !strings.Contains(innerText(t, r), "secret plan") {
		t.Fatalf("%+v", r)
	}
	pm, _ := mime.Parse(r.Inner)
	if len(pm.Attach) != 1 || pm.Attach[0].Filename != "n.txt" {
		t.Fatalf("the attachment did not survive: %+v", pm.Attach)
	}
	// the sender can read their own copy; a third person cannot
	if r := Open(enc, ana); !r.Decrypted {
		t.Fatalf("the sender cannot read their copy: %+v", r)
	}
	other, _ := ring(t)
	if r := Open(enc, other); r.Decrypted || r.Problem == "" {
		t.Fatalf("a stranger opened it: %+v", r)
	}
	// a recipient with no key is named
	if _, err := Protect(base(t), Options{Protocol: "pgp", Encrypt: true, From: "ana@x.test", Recipients: []string{"carl@z.test"}}, ana); err == nil || !strings.Contains(err.Error(), "carl@z.test") {
		t.Fatalf("missing key not named: %v", err)
	}
}

func TestSMIMESignVerifyEncrypt(t *testing.T) {
	ana, bo := ring(t)
	signed, err := Protect(base(t), Options{Protocol: "smime", Sign: true, From: "ana@x.test", Recipients: []string{"bo@y.test"}}, ana)
	if err != nil {
		t.Fatal(err)
	}
	r := Open(signed, bo)
	if !r.Valid || !r.Trusted || r.Signer.Email != "ana@x.test" || !strings.Contains(innerText(t, r), "secret plan") {
		t.Fatalf("%+v", r)
	}
	// a signer whose certificate is unknown: valid but not trusted
	r = Open(signed, &Keyring{})
	if !r.Valid || r.Trusted || r.Problem == "" || r.SignerKey == nil {
		t.Fatalf("self signed, unknown: %+v", r)
	}
	bad := []byte(strings.Replace(string(signed), "secret plan", "public plan", 1))
	if r := Open(bad, bo); r.Valid {
		t.Fatalf("a changed message passed: %+v", r)
	}
	enc, err := Protect(base(t), Options{Protocol: "smime", Sign: true, Encrypt: true, From: "ana@x.test", Recipients: []string{"bo@y.test"}}, ana)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(enc), "secret plan") {
		t.Fatal("plain text in the encrypted message")
	}
	r = Open(enc, bo)
	if !r.Decrypted || !r.Valid || !r.Encrypted || !strings.Contains(innerText(t, r), "secret plan") {
		t.Fatalf("%+v", r)
	}
	other, _ := ring(t)
	if r := Open(enc, other); r.Decrypted {
		t.Fatal("a stranger opened it")
	}
}

func TestImportExport(t *testing.T) {
	k, _ := GeneratePGP("Ana", "ana@x.test")
	keys, err := ImportPGP([]byte(k.Private), "")
	if err != nil || len(keys) != 1 || !keys[0].HasPrivate() || keys[0].Email != "ana@x.test" {
		t.Fatalf("%v %v", keys, err)
	}
	pub, err := ImportPGP([]byte(k.Public), "")
	if err != nil || pub[0].HasPrivate() {
		t.Fatalf("%v %v", pub, err)
	}
	if _, err := ImportPGP([]byte("nonsense"), ""); err == nil {
		t.Fatal("nonsense imported")
	}
	s, _ := GenerateSMIME("Ana", "ana@x.test")
	got, err := ImportSMIME([]byte(s.Cert+s.Key), "")
	if err != nil || !got[0].HasPrivate() || got[0].Email != "ana@x.test" || got[0].ID != s.ID {
		t.Fatalf("%v %v", got, err)
	}
	kr := &Keyring{}
	kr.AddPGP(k)
	kr.AddSMIME(s)
	if p := kr.Public(); p.PGP[0].Private != "" || p.SMIME[0].Key != "" {
		t.Fatal("a private key leaves in the public view")
	}
	if !kr.Remove(k.ID) || len(kr.PGP) != 0 {
		t.Fatal("remove")
	}
}
