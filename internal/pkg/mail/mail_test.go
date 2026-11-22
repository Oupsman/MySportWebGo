package mail

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	netmail "net/mail"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func env(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func TestParseConfig(t *testing.T) {
	ok := map[string]string{"SMTP_HOST": "smtp.sendgrid.net", "SMTP_PORT": "587", "SMTP_USERNAME": "apikey",
		"SMTP_PASSWORD": "SG.x", "MAIL_FROM": "MySportWeb <sport@example.com>"}
	c, err := ParseConfig(env(ok))
	if err != nil || !c.Enabled() || c.TLS != TLSStartTLS || c.Port != 587 || c.From.Address != "sport@example.com" ||
		c.From.Name != "MySportWeb" {
		t.Fatalf("valid: %+v %v", c, err)
	}
	if c, err := ParseConfig(env(nil)); err != nil || c.Enabled() {
		t.Errorf("nothing set: %+v %v", c, err)
	}
	with := func(changes map[string]string) map[string]string {
		m := map[string]string{}
		for k, v := range ok {
			m[k] = v
		}
		for k, v := range changes {
			m[k] = v
		}
		return m
	}
	for name, test := range map[string]struct {
		values map[string]string
		says   string
	}{
		"settings without host": {map[string]string{"SMTP_PORT": "587", "MAIL_FROM": "a@b.c"}, "SMTP_PORT, MAIL_FROM set without SMTP_HOST"},
		"no port":               {with(map[string]string{"SMTP_PORT": ""}), "SMTP_PORT is missing"},
		"host with port":        {with(map[string]string{"SMTP_HOST": "smtp.sendgrid.net:587"}), "the port goes in SMTP_PORT"},
		"bad port":              {with(map[string]string{"SMTP_PORT": "smtp"}), "not a port"},
		"unknown tls":           {with(map[string]string{"SMTP_TLS": "ssl"}), "SMTP_TLS is \"ssl\""},
		"user without password": {with(map[string]string{"SMTP_PASSWORD": ""}), "SMTP_USERNAME and SMTP_PASSWORD go together"},
		"no encryption":         {with(map[string]string{"SMTP_TLS": "none"}), "in clear"},
		"no sender":             {with(map[string]string{"MAIL_FROM": ""}), "MAIL_FROM is missing"},
		"bad sender":            {with(map[string]string{"MAIL_FROM": "sport at example"}), "not an email address"},
	} {
		_, err := ParseConfig(env(test.values))
		if err == nil || !strings.Contains(err.Error(), test.says) {
			t.Errorf("%s: %v, want it to say %q", name, err, test.says)
		}
	}
	// no encryption towards a relay on the same machine
	local := with(map[string]string{"SMTP_HOST": "127.0.0.1", "SMTP_TLS": "none", "SMTP_USERNAME": "", "SMTP_PASSWORD": ""})
	if _, err := ParseConfig(env(local)); err != nil {
		t.Errorf("local relay: %v", err)
	}
}

func message() Message {
	return Message{To: netmail.Address{Name: "Zoé", Address: "zoe@example.org"}, Subject: "Réinitialisation: your password",
		Text: "Hello Zoé,\nthe link: https://sport.example.com/#/reset-password?code=abc\n", HTML: "<p>Hello Zoé</p>"}
}

// checkMessage parses a delivered message: its headers and both versions
func checkMessage(t *testing.T, data string) {
	t.Helper()
	parsed, err := netmail.ReadMessage(strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	subject, _ := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject"))
	if subject != "Réinitialisation: your password" || parsed.Header.Get("Message-ID") == "" ||
		!strings.Contains(parsed.Header.Get("From"), "sport@example.com") {
		t.Errorf("headers: %v", parsed.Header)
	}
	_, params, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	parts := multipart.NewReader(parsed.Body, params["boundary"])
	var bodies []string
	for {
		part, err := parts.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		content, _ := io.ReadAll(part) // quoted-printable decoded by NextPart
		bodies = append(bodies, part.Header.Get("Content-Type")+"|"+string(content))
	}
	if len(bodies) != 2 || !strings.Contains(bodies[0], "text/plain") ||
		!strings.Contains(bodies[0], "reset-password?code=abc") || !strings.Contains(bodies[1], "<p>Hello Zoé</p>") {
		t.Errorf("bodies: %q", bodies)
	}
}

func smtpFor(t *testing.T, f *fakeSMTP, tlsMode, user, password string) *SMTP {
	host, port, _ := net.SplitHostPort(f.addr)
	p, _ := strconv.Atoi(port)
	return &SMTP{Config: Config{Host: host, Port: p, TLS: tlsMode, Username: user, Password: password,
		From: netmail.Address{Name: "MySportWeb", Address: "sport@example.com"}}, TLSConfig: f.client}
}

func TestSendStartTLS(t *testing.T) {
	f := startFakeSMTP(t, false, "apikey", "secret")
	if err := smtpFor(t, f, TLSStartTLS, "apikey", "secret").Send(context.Background(), message()); err != nil {
		t.Fatal(err)
	}
	got := f.received()
	if len(got) != 1 || !got[0].tls || !got[0].authed || got[0].from != "sport@example.com" || got[0].to != "zoe@example.org" {
		t.Fatalf("received %+v", got)
	}
	checkMessage(t, got[0].data)

	err := smtpFor(t, f, TLSStartTLS, "apikey", "wrong").Send(context.Background(), message())
	if err == nil || !strings.Contains(err.Error(), "check SMTP_USERNAME and SMTP_PASSWORD") {
		t.Errorf("wrong password: %v", err)
	}
}

func TestSendImplicitTLS(t *testing.T) {
	f := startFakeSMTP(t, true, "user", "pass")
	if err := smtpFor(t, f, TLSImplicit, "user", "pass").Send(context.Background(), message()); err != nil {
		t.Fatal(err)
	}
	if got := f.received(); len(got) != 1 || !got[0].tls || !got[0].authed {
		t.Fatalf("received %+v", got)
	}
}

func TestSendUnreachable(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	host, port, _ := net.SplitHostPort(addr)
	p, _ := strconv.Atoi(port)
	s := &SMTP{Config: Config{Host: host, Port: p, TLS: TLSNone, From: netmail.Address{Address: "a@example.com"}}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Send(ctx, message()); err == nil || !strings.Contains(err.Error(), "connecting to") {
		t.Errorf("unreachable: %v", err)
	}
}

func TestComposeRefusesHeaderInjection(t *testing.T) {
	m := message()
	m.Subject = "hi\r\nBcc: victim@example.org"
	if body, err := Compose(netmail.Address{Address: "a@example.com"}, m, time.Now()); err == nil &&
		strings.Contains(string(body), "\r\nBcc:") {
		t.Error("a header injected through the subject")
	}
}

func TestHints(t *testing.T) {
	for name, test := range map[string]struct {
		err  error
		says string
	}{
		"timeout": {fmt.Errorf("dial: %w", context.DeadlineExceeded), "block the outgoing SMTP ports"},
		"dns":     {&net.DNSError{Err: "no such host", Name: "smtp.sendgrid.nett"}, "isn't a known host name"},
		"refused": {errors.New("dial tcp 1.2.3.4:587: connect: connection refused"), "nothing listens"},
		"tls":     {errors.New("tls: first record does not look like a TLS handshake"), "check SMTP_TLS against the port"},
		"auth":    {errors.New("535 5.7.8 Authentication failed"), "check SMTP_USERNAME and SMTP_PASSWORD"},
		"sender":  {errors.New("550 The from address does not match a verified Sender Identity"), "verify the domain"},
		"unknown": {errors.New("something else"), ""},
	} {
		if got := Hint(test.err); test.says == "" && got != "" || !strings.Contains(got, test.says) {
			t.Errorf("%s: %q, want it to say %q", name, got, test.says)
		}
	}
}

// TestSendLogs: a failure is logged with its step and a hint, a success with its duration
func TestSendLogs(t *testing.T) {
	f := startFakeSMTP(t, false, "apikey", "secret")
	var logs bytes.Buffer
	s := smtpFor(t, f, TLSStartTLS, "apikey", "wrong")
	s.Logger = zerolog.New(&logs)
	err := s.Send(context.Background(), message())
	if err == nil || !strings.Contains(err.Error(), "authenticating") {
		t.Fatalf("error %v", err)
	}
	if out := logs.String(); !strings.Contains(out, `"step":"auth"`) || !strings.Contains(out, "smtp: email not sent") ||
		!strings.Contains(out, "Hint:") || strings.Contains(out, "wrong") {
		t.Errorf("logs: %s", out)
	}
	logs.Reset()
	s.Config.Password = "secret"
	if err := s.Send(context.Background(), message()); err != nil {
		t.Fatal(err)
	}
	if out := logs.String(); !strings.Contains(out, "smtp: email accepted by the server") || !strings.Contains(out, `"step":"starttls"`) {
		t.Errorf("logs: %s", out)
	}
}
