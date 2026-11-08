package lan

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"mac2win/internal/core"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

type Identity struct {
	Certificate tls.Certificate
	ID          string
}

func LoadIdentity(dir string) (Identity, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return Identity{}, err
	}
	path := filepath.Join(dir, "identity.pem")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		pub, key, e := ed25519.GenerateKey(rand.Reader)
		if e != nil {
			return Identity{}, e
		}
		serial, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
		if e != nil {
			return Identity{}, e
		}
		template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Lantern device"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(20, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}}
		cert, e := x509.CreateCertificate(rand.Reader, template, template, pub, key)
		if e != nil {
			return Identity{}, e
		}
		pk, e := x509.MarshalPKCS8PrivateKey(key)
		if e != nil {
			return Identity{}, e
		}
		data = append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pk})...)
		f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return Identity{}, e
		}
		_, e = f.Write(data)
		closeErr := f.Close()
		if e != nil {
			return Identity{}, e
		}
		if closeErr != nil {
			return Identity{}, closeErr
		}
	} else if err != nil {
		return Identity{}, err
	}
	cert, err := tls.X509KeyPair(data, data)
	if err != nil {
		return Identity{}, err
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return Identity{}, err
	}
	return Identity{Certificate: cert, ID: core.Fingerprint(leaf)}, nil
}
