package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeGateway plays the Kernel2Cloud gateway: TLS with ALPN, the KEY line,
// then an app that answers "app: <line>". takesKey false plays a port whose
// access isn't API key (no ALPN agreed).
func fakeGateway(t *testing.T, goodKey string, takesKey bool) string {
	t.Helper()
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "db.example"}, DNSNames: []string{"db.example"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, _ := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	cfg := &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: k}}}
	if takesKey {
		cfg.NextProtos = []string{ALPN}
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				line, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimSpace(line) != "KEY "+goodKey {
					fmt.Fprint(c, "ERR invalid API key\n")
					return
				}
				fmt.Fprint(c, "OK\n")
				for {
					l, err := r.ReadString('\n')
					if err != nil {
						return
					}
					fmt.Fprintf(c, "app: %s", l)
				}
			}(c)
		}
	}()
	return ln.Addr().String()
}

// syncBuffer is a log the forwarder writes while the test reads it.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// forwarding starts a forwarder to the fake gateway and returns its local address.
func forwarding(t *testing.T, gw, key string) (string, *syncBuffer) {
	t.Helper()
	f, err := newForwarder("db.example", key, gw, true)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	logs := &syncBuffer{}
	go func() { _ = f.serve(ctx, ln, log.New(logs, "", 0)) }()
	return ln.Addr().String(), logs
}

func TestForwardCarriesTheAppProtocol(t *testing.T) {
	gw := fakeGateway(t, "k2c_key_good", true)
	local, _ := forwarding(t, gw, "k2c_key_good")
	c, err := net.Dial("tcp", local)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r := bufio.NewReader(c)
	for _, msg := range []string{"SELECT 1", "SELECT 2"} {
		fmt.Fprintf(c, "%s\n", msg)
		if got, err := r.ReadString('\n'); err != nil || got != "app: "+msg+"\n" {
			t.Fatalf("got %q %v", got, err)
		}
	}
}

func TestForwardReportsAWrongKey(t *testing.T) {
	gw := fakeGateway(t, "k2c_key_good", true)
	local, logs := forwarding(t, gw, "k2c_key_wrong")
	c, err := net.Dial("tcp", local)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	if b, _ := io.ReadAll(c); len(b) != 0 {
		t.Errorf("local client got %q", b)
	}
	c.Close()
	time.Sleep(50 * time.Millisecond)
	if !strings.Contains(logs.String(), "invalid API key") {
		t.Errorf("log: %q", logs.String())
	}
}

func TestCheckRefusesAPortWithoutAPIKeyAccess(t *testing.T) {
	gw := fakeGateway(t, "k2c_key_good", false)
	f, _ := newForwarder("db.example:443", "k2c_key_good", gw, true)
	if err := f.check(context.Background()); err == nil || !strings.Contains(err.Error(), "not a TCP port that takes an API key") {
		t.Fatalf("got %v", err)
	}
	ok := fakeGateway(t, "k2c_key_good", true)
	f, _ = newForwarder("db.example", "k2c_key_good", ok, true)
	if err := f.check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.address != "db.example:443" {
		t.Errorf("address %q: port 443 is the default", f.address)
	}
}

func TestTheGatewayCertificateIsVerified(t *testing.T) {
	gw := fakeGateway(t, "k2c_key_good", true) // self-signed
	f, _ := newForwarder("db.example", "k2c_key_good", gw, false)
	if err := f.check(context.Background()); err == nil {
		t.Fatal("an untrusted certificate was accepted")
	}
}

func TestCommands(t *testing.T) {
	var out, errs bytes.Buffer
	if code := run(context.Background(), []string{"version"}, &out, &errs); code != 0 || out.String() != "k2c dev\n" {
		t.Errorf("version: %d %q", code, out.String())
	}
	if code := run(context.Background(), []string{"nope"}, &out, &errs); code != 2 {
		t.Errorf("unknown command: %d", code)
	}
	t.Setenv("K2C_API_KEY", "")
	errs.Reset()
	if code := run(context.Background(), []string{"forward", "db.example", "5432"}, &out, &errs); code != 2 || !strings.Contains(errs.String(), "K2C_API_KEY") {
		t.Errorf("no key: %d %q", code, errs.String())
	}
}
