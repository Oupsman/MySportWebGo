package mail

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSMTP is an SMTP server of the tests: STARTTLS or implicit TLS, AUTH PLAIN, one message per connection
type fakeSMTP struct {
	addr     string
	tlsCfg   *tls.Config // the server's
	client   *tls.Config // trusting the server's certificate
	implicit bool
	user     string
	password string

	mu       sync.Mutex
	messages []received
}

type received struct {
	from, to string
	data     string
	tls      bool
	authed   bool
}

// selfSigned returns a certificate for 127.0.0.1, and a client configuration trusting it
func selfSigned(t *testing.T) (*tls.Config, *tls.Config) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "127.0.0.1"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}},
		&tls.Config{RootCAs: pool, ServerName: "127.0.0.1"}
}

func startFakeSMTP(t *testing.T, implicit bool, user, password string) *fakeSMTP {
	t.Helper()
	server, client := selfSigned(t)
	f := &fakeSMTP{tlsCfg: server, client: client, implicit: implicit, user: user, password: password}
	var ln net.Listener
	var err error
	if implicit {
		ln, err = tls.Listen("tcp", "127.0.0.1:0", server)
	} else {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	f.addr = ln.Addr().String()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(conn)
		}
	}()
	return f
}

func (f *fakeSMTP) serve(conn net.Conn) {
	defer conn.Close()
	r, w := bufio.NewReader(conn), conn
	msg := received{tls: f.implicit}
	reply := func(s string) { w.Write([]byte(s + "\r\n")) }
	reply("220 fake ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.TrimRight(line, "\r\n")
		upper := strings.ToUpper(cmd)
		switch {
		case strings.HasPrefix(upper, "EHLO"):
			reply("250-fake")
			if !msg.tls {
				reply("250-STARTTLS")
			}
			reply("250 AUTH PLAIN")
		case upper == "STARTTLS":
			reply("220 go ahead")
			tlsConn := tls.Server(conn, f.tlsCfg)
			if err := tlsConn.Handshake(); err != nil {
				return
			}
			conn, r, w = tlsConn, bufio.NewReader(tlsConn), tlsConn
			msg.tls = true
		case strings.HasPrefix(upper, "AUTH PLAIN"):
			raw, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(cmd[len("AUTH PLAIN"):]))
			parts := strings.Split(string(raw), "\x00")
			if len(parts) == 3 && parts[1] == f.user && parts[2] == f.password {
				msg.authed = true
				reply("235 ok")
			} else {
				reply("535 bad credentials")
			}
		case strings.HasPrefix(upper, "MAIL FROM:"):
			msg.from = strings.Trim(cmd[len("MAIL FROM:"):], "<> ")
			reply("250 ok")
		case strings.HasPrefix(upper, "RCPT TO:"):
			msg.to = strings.Trim(cmd[len("RCPT TO:"):], "<> ")
			reply("250 ok")
		case upper == "DATA":
			reply("354 end with .")
			var data strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				data.WriteString(l)
			}
			msg.data = data.String()
			f.mu.Lock()
			f.messages = append(f.messages, msg)
			f.mu.Unlock()
			reply("250 queued")
		case upper == "QUIT":
			reply("221 bye")
			return
		default:
			reply("250 ok")
		}
	}
}

func (f *fakeSMTP) received() []received {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]received(nil), f.messages...)
}
