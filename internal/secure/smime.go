package secure

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/smallstep/pkcs7"
	pkcs12 "software.sslmate.com/src/go-pkcs12"
)

func serialHex(c *x509.Certificate) string { return hex.EncodeToString(c.SerialNumber.Bytes()) }

func certEmail(c *x509.Certificate) string {
	if len(c.EmailAddresses) > 0 {
		return c.EmailAddresses[0]
	}
	return c.Subject.CommonName
}

func pemCert(c *x509.Certificate) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw}))
}

func pemKey(k crypto.PrivateKey) (string, error) {
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), nil
}

func parseCert(s string) (*x509.Certificate, error) {
	b, _ := pem.Decode([]byte(s))
	if b == nil {
		return nil, errors.New("the stored certificate cannot be read")
	}
	return x509.ParseCertificate(b.Bytes)
}

func parseKey(s string) (crypto.PrivateKey, error) {
	b, _ := pem.Decode([]byte(s))
	if b == nil {
		return nil, errors.New("the stored private key cannot be read")
	}
	k, err := x509.ParsePKCS8PrivateKey(b.Bytes)
	if err != nil {
		return x509.ParsePKCS1PrivateKey(b.Bytes)
	}
	return k, nil
}

func toSMIME(c *x509.Certificate, key crypto.PrivateKey, chain []*x509.Certificate) (SMIMEKey, error) {
	k := SMIMEKey{ID: serialHex(c), Name: c.Subject.CommonName, Email: certEmail(c), Cert: pemCert(c), Expires: c.NotAfter.UTC().Format(time.RFC3339),
		Self: c.CheckSignatureFrom(c) == nil && c.Subject.String() == c.Issuer.String()}
	if key != nil {
		pk, err := pemKey(key)
		if err != nil {
			return k, err
		}
		k.Key = pk
	}
	for _, ch := range chain {
		k.Chain += pemCert(ch)
	}
	return k, nil
}

// GenerateSMIME makes a self-signed S/MIME certificate and key (RSA 3072, two years). Anyone can verify its signatures
// once they hold the certificate, but it is not vouched for by a certificate authority.
func GenerateSMIME(name, email string) (SMIMEKey, error) {
	if email == "" {
		return SMIMEKey{}, errors.New("an email address is needed")
	}
	key, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		return SMIMEKey{}, err
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 100))
	if name == "" {
		name = email
	}
	tpl := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: name, Organization: nil}, EmailAddresses: []string{email},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(2, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageContentCommitment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageEmailProtection}, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return SMIMEKey{}, err
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		return SMIMEKey{}, err
	}
	return toSMIME(c, key, nil)
}

// ImportSMIME reads a PKCS 12 file (.p12, .pfx) with its password, or a PEM file holding a certificate and, if it is
// the person's own, its private key.
func ImportSMIME(data []byte, pass string) ([]SMIMEKey, error) {
	if !strings.Contains(string(data), "-----BEGIN") {
		key, cert, chain, err := pkcs12.DecodeChain(data, pass)
		if err != nil {
			return nil, fmt.Errorf("could not open that file (wrong password?): %w", err)
		}
		k, err := toSMIME(cert, key, chain)
		return []SMIMEKey{k}, err
	}
	var certs []*x509.Certificate
	var key crypto.PrivateKey
	rest := data
	for {
		var b *pem.Block
		b, rest = pem.Decode(rest)
		if b == nil {
			break
		}
		switch {
		case b.Type == "CERTIFICATE":
			c, err := x509.ParseCertificate(b.Bytes)
			if err != nil {
				return nil, err
			}
			certs = append(certs, c)
		case strings.HasSuffix(b.Type, "PRIVATE KEY"):
			k, err := x509.ParsePKCS8PrivateKey(b.Bytes)
			if err != nil {
				k, err = x509.ParsePKCS1PrivateKey(b.Bytes)
			}
			if err != nil {
				return nil, errors.New("that private key is encrypted or not in a form this app reads: use a .p12 file instead")
			}
			key = k
		}
	}
	if len(certs) == 0 {
		return nil, errors.New("no certificate found in that file")
	}
	var out []SMIMEKey
	for i, c := range certs {
		var k crypto.PrivateKey
		if i == 0 {
			k = key
		}
		sk, err := toSMIME(c, k, nil)
		if err != nil {
			return nil, err
		}
		out = append(out, sk)
	}
	return out, nil
}

func smimeSignDetached(content []byte, k SMIMEKey) ([]byte, error) {
	cert, err := parseCert(k.Cert)
	if err != nil {
		return nil, err
	}
	key, err := parseKey(k.Key)
	if err != nil {
		return nil, err
	}
	sd, err := pkcs7.NewSignedData(content)
	if err != nil {
		return nil, err
	}
	sd.SetDigestAlgorithm(pkcs7.OIDDigestAlgorithmSHA256)
	var chain []*x509.Certificate
	rest := []byte(k.Chain)
	for {
		var b *pem.Block
		b, rest = pem.Decode(rest)
		if b == nil {
			break
		}
		if c, err := x509.ParseCertificate(b.Bytes); err == nil {
			chain = append(chain, c)
		}
	}
	if err := sd.AddSignerChain(cert, key, chain, pkcs7.SignerInfoConfig{}); err != nil {
		return nil, err
	}
	sd.Detach()
	return sd.Finish()
}

// smimeVerifyDetached checks a detached signature. trusted is true when the signer's certificate chains to a root this
// computer trusts, or is one the person imported.
func smimeVerifyDetached(content, sig []byte, known []SMIMEKey) (signer SMIMEKey, trusted bool, err error) {
	p7, err := pkcs7.Parse(sig)
	if err != nil {
		return SMIMEKey{}, false, err
	}
	p7.Content = content
	if err := p7.Verify(); err != nil {
		return SMIMEKey{}, false, err
	}
	c := p7.GetOnlySigner()
	if c == nil {
		return SMIMEKey{}, false, errors.New("the signature names no signer")
	}
	signer, _ = toSMIME(c, nil, nil)
	for _, k := range known {
		if k.ID == signer.ID {
			trusted = true
		}
	}
	if !trusted {
		pool, _ := x509.SystemCertPool()
		inter := x509.NewCertPool()
		for _, ic := range p7.Certificates {
			inter.AddCert(ic)
		}
		if pool != nil {
			if _, err := c.Verify(x509.VerifyOptions{Roots: pool, Intermediates: inter, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageEmailProtection}}); err == nil {
				trusted = true
			}
		}
	}
	return signer, trusted, nil
}

func smimeEncrypt(content []byte, to []SMIMEKey) ([]byte, error) {
	var certs []*x509.Certificate
	for _, k := range to {
		c, err := parseCert(k.Cert)
		if err != nil {
			return nil, err
		}
		certs = append(certs, c)
	}
	pkcs7.ContentEncryptionAlgorithm = pkcs7.EncryptionAlgorithmAES256GCM
	return pkcs7.Encrypt(content, certs)
}

func smimeDecrypt(der []byte, own []SMIMEKey) ([]byte, error) {
	p7, err := pkcs7.Parse(der)
	if err != nil {
		return nil, err
	}
	for _, k := range own {
		if !k.HasPrivate() {
			continue
		}
		c, err := parseCert(k.Cert)
		if err != nil {
			continue
		}
		key, err := parseKey(k.Key)
		if err != nil {
			continue
		}
		if plain, err := p7.Decrypt(c, key); err == nil {
			return plain, nil
		}
	}
	return nil, errors.New("this message was not encrypted to a certificate you have")
}
