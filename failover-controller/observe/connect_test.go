package observe

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func caFile(t *testing.T) string {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "ca"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(p, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// the cluster refused every check with "no encryption": pgx built the config for its default unix socket,
// which never uses TLS, and the tcp host was only set afterwards.
func TestATCPCheckUsesTLSAndNeverFallsBackToPlaintext(t *testing.T) {
	if err := os.MkdirAll("/tmp", 0o1777); err != nil {
		t.Skip("cannot make /tmp, the default socket directory the bug depends on")
	}
	for _, mode := range []string{"verify-ca", "verify-full", "require"} {
		c, err := connConfig(CheckConfig{Host: "10.42.2.177", Port: 5432, TLSMode: mode, RootCA: caFile(t)})
		if err != nil {
			t.Fatal(err)
		}
		if c.TLSConfig == nil {
			t.Fatalf("sslmode=%s to a tcp host built a plaintext connection", mode)
		}
		for _, f := range c.Fallbacks {
			if f.TLSConfig == nil {
				t.Fatalf("sslmode=%s keeps a plaintext fallback", mode)
			}
		}
		if c.Host != "10.42.2.177" || c.Port != 5432 {
			t.Fatalf("host %s port %d", c.Host, c.Port)
		}
	}
}
