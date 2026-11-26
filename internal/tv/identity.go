package tv

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"errors"
	"math/big"
	"os"
	"time"
)

// Identity uses a persistent key and a replaceable certificate. A TV pins the public key,
// allowing the NAS to change address and renew the certificate without pairing again.
func Identity(path string) (tls.Certificate, string, error) {
	var result tls.Certificate
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return result, "", err
		}
		raw, err = x509.MarshalECPrivateKey(key)
		if err != nil {
			return result, "", err
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return result, "", err
		}
		_, err = f.Write(raw)
		if err = errors.Join(err, f.Sync(), f.Close()); err != nil {
			return result, "", err
		}
	} else if err != nil {
		return result, "", err
	}
	key, err := x509.ParseECPrivateKey(raw)
	if err != nil {
		return result, "", err
	}
	spki, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return result, "", err
	}
	pin := sha256.Sum256(spki)
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return result, "", err
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: "TrackFerry TV"},
		NotBefore: now.Add(-24 * time.Hour), NotAfter: now.AddDate(10, 0, 0),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return result, "", err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, hex.EncodeToString(pin[:]), nil
}
