package main

// k2c forward speaks a small protocol with the Kernel2Cloud gateway, over TLS
// to the port's address (the gateway's certificate is verified):
//
//  1. TLS with ALPN "k2c-forward/1". The gateway agrees to it only for TCP
//     ports that take an API key.
//  2. The client sends "KEY <api key>\n".
//  3. The gateway answers "OK\n" once it has a ready copy of the deployment
//     connected, or "ERR <reason>\n" and closes.
//  4. After "OK" the connection carries the app's own protocol both ways.

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

// ALPN is the protocol k2c forward asks the gateway for.
const ALPN = "k2c-forward/1"

func forwardCmd(ctx context.Context, args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("forward", flag.ContinueOnError)
	fs.SetOutput(stderr)
	bind := fs.String("bind", "127.0.0.1", "local address to listen on")
	connect := fs.String("connect", "", "connect to this host:port instead of the address (testing)")
	insecure := fs.Bool("insecure", false, "don't verify the gateway's certificate (testing only)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, "usage: k2c forward [flags] <address> <local port>")
		return 2
	}
	logger := log.New(stderr, "", log.Ltime)
	key := strings.TrimSpace(os.Getenv("K2C_API_KEY"))
	if key == "" {
		logger.Print("set K2C_API_KEY to one of your account's API keys (API keys page in the console)")
		return 2
	}
	f, err := newForwarder(fs.Arg(0), key, *connect, *insecure)
	if err != nil {
		logger.Print(err)
		return 2
	}
	// Check the address before listening: TLS and the port type, without a
	// key (that would open a connection to the app).
	if err := f.check(ctx); err != nil {
		logger.Printf("%s: %v", f.address, err)
		return 1
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(*bind, fs.Arg(1)))
	if err != nil {
		logger.Print(err)
		return 1
	}
	logger.Printf("forwarding %s to %s; Ctrl-C to stop", ln.Addr(), f.address)
	if err := f.serve(ctx, ln, logger); err != nil {
		logger.Print(err)
		return 1
	}
	return 0
}

type forwarder struct {
	address string // host:port as given
	dial    string // where to connect
	key     string
	tls     *tls.Config
}

func newForwarder(address, key, connect string, insecure bool) (*forwarder, error) {
	if _, _, err := net.SplitHostPort(address); err != nil {
		address = net.JoinHostPort(address, "443")
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil || host == "" {
		return nil, fmt.Errorf("bad address %q: want host:port, e.g. db-chat-acme.apps.kernel2cloud.ai:443", address)
	}
	dial := address
	if connect != "" {
		dial = connect
	}
	return &forwarder{address: address, dial: dial, key: key, tls: &tls.Config{
		ServerName: host, NextProtos: []string{ALPN}, MinVersion: tls.VersionTLS12,
		InsecureSkipVerify: insecure, //nolint:gosec // only with -insecure, for testing
	}}, nil
}

// serve accepts local connections until ctx ends.
func (f *forwarder) serve(ctx context.Context, ln net.Listener, logger *log.Logger) error {
	go func() { <-ctx.Done(); ln.Close() }()
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go f.carry(ctx, c, logger)
	}
}

func (f *forwarder) carry(ctx context.Context, local net.Conn, logger *log.Logger) {
	defer local.Close()
	remote, err := f.open(ctx)
	if err != nil {
		logger.Printf("connection from %s: %v", local.RemoteAddr(), err)
		return
	}
	defer remote.Close()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(remote, local)
		_ = remote.CloseWrite()
	}()
	_, _ = io.Copy(local, remote)
	if tc, ok := local.(*net.TCPConn); ok {
		_ = tc.CloseWrite()
	}
	wg.Wait()
}

func (f *forwarder) handshake(ctx context.Context) (*tls.Conn, error) {
	d := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 15 * time.Second}, Config: f.tls}
	nc, err := d.DialContext(ctx, "tcp", f.dial)
	if err != nil {
		return nil, err
	}
	c := nc.(*tls.Conn)
	if c.ConnectionState().NegotiatedProtocol != ALPN {
		c.Close()
		return nil, errors.New("this address is not a TCP port that takes an API key (in the console, its access must be API key)")
	}
	return c, nil
}

// check connects and leaves before sending the key.
func (f *forwarder) check(ctx context.Context) error {
	c, err := f.handshake(ctx)
	if err != nil {
		return err
	}
	return c.Close()
}

// open connects and presents the key.
func (f *forwarder) open(ctx context.Context) (*tls.Conn, error) {
	c, err := f.handshake(ctx)
	if err != nil {
		return nil, err
	}
	_ = c.SetDeadline(time.Now().Add(30 * time.Second))
	if _, err := fmt.Fprintf(c, "KEY %s\n", f.key); err != nil {
		c.Close()
		return nil, err
	}
	// Byte by byte: what follows "OK\n" belongs to the app.
	line, err := readLine(c)
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("no answer from Kernel2Cloud: %w", err)
	}
	if msg, bad := strings.CutPrefix(line, "ERR "); bad {
		c.Close()
		return nil, errors.New(msg)
	}
	if line != "OK" {
		c.Close()
		return nil, fmt.Errorf("unexpected answer %q", line)
	}
	_ = c.SetDeadline(time.Time{})
	return c, nil
}

func readLine(r io.Reader) (string, error) {
	var b strings.Builder
	one := make([]byte, 1)
	for b.Len() < 512 {
		if _, err := io.ReadFull(r, one); err != nil {
			return "", err
		}
		if one[0] == '\n' {
			return strings.TrimSpace(b.String()), nil
		}
		b.WriteByte(one[0])
	}
	return "", errors.New("answer too long")
}
