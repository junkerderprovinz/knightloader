package vpntest

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

// TLSServer serves h over TLS with a certificate for names, host names or
// addresses, signed by a CA of its own, the way a provider's API sits behind
// a private CA. roots holds that CA, and dial reaches the server whatever
// address it is asked for.
func TLSServer(t testing.TB, h http.Handler, names ...string) (roots *x509.CertPool, dial func(ctx context.Context, network, address string) (net.Conn, error)) {
	t.Helper()
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Test Provider CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, _ := x509.ParseCertificate(caDER)
	leafKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: names[0]},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, n := range names {
		if a, err := netip.ParseAddr(n); err == nil {
			leafTmpl.IPAddresses = append(leafTmpl.IPAddresses, a.AsSlice())
		} else {
			leafTmpl.DNSNames = append(leafTmpl.DNSNames, n)
		}
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, ca, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(h)
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{leafDER, caDER}, PrivateKey: leafKey}}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	roots = x509.NewCertPool()
	roots.AddCert(ca)
	to := srv.Listener.Addr().String()
	return roots, func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, to)
	}
}
