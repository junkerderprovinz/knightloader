package remotefs

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"io"
	"math/big"
	"net"
	"testing"
	"time"
)

// vsftpd, ProFTPD and FileZilla Server refuse a data connection that does not
// resume the control connection's TLS session, with "425 Cannot secure data
// connection". The library opens every data connection with the config
// dialFTP builds, so that config has to carry the session from one to the
// next.
func TestFTPSDataConnectionResumesTheControlConnectionsSession(t *testing.T) {
	cert, roots := selfSignedCert(t)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	resumed := make(chan bool, 1)
	go func() {
		control, err := ln.Accept()
		if err != nil {
			return
		}
		defer control.Close()
		// The greeting the client reads is what hands it the session ticket.
		_, _ = io.WriteString(control, "220 ready\r\n")
		data, err := ln.Accept()
		if err != nil {
			return
		}
		defer data.Close()
		dc := data.(*tls.Conn)
		if dc.Handshake() != nil {
			resumed <- false
			return
		}
		resumed <- dc.ConnectionState().DidResume
	}()

	cfg := ftpTLSConfig("127.0.0.1")
	cfg.RootCAs = roots
	control, err := tls.Dial("tcp", ln.Addr().String(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	if _, err := bufio.NewReader(control).ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	data, err := tls.Dial("tcp", ln.Addr().String(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()

	select {
	case ok := <-resumed:
		if !ok {
			t.Fatal("the data connection started a new TLS session, which these servers answer with 425")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the server never saw the data connection")
	}
}

// selfSignedCert is a certificate for 127.0.0.1 and the pool that trusts it.
func selfSignedCert(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1)},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, roots
}
