// Package mail sends the emails of the application through an SMTP server: a relay of a sending service
// (SendGrid, Postmark, Mailgun, Brevo...) rather than a server of one's own, whose mails end up as spam
package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

// The encryptions of the connection to the SMTP server
const (
	TLSStartTLS = "starttls" // the connection becomes encrypted after its start: port 587, the default
	TLSImplicit = "tls"      // encrypted from the start: port 465
	TLSNone     = "none"     // never encrypted: only towards the local host (a relay on the same machine)
)

// sendTimeout bounds the sending of a message, from the connection to the end
const sendTimeout = 30 * time.Second

// Config is the SMTP server the emails go through. Empty Host: no email.
type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	TLS      string
	From     mail.Address
}

// Enabled tells whether emails can be sent
func (c Config) Enabled() bool {
	return c.Host != ""
}

// variables are the settings of the environment, without their MSW_ prefix of docker compose
var variables = []string{"SMTP_HOST", "SMTP_PORT", "SMTP_USERNAME", "SMTP_PASSWORD", "SMTP_TLS", "MAIL_FROM"}

// ParseConfig reads the settings of the emails from getenv. Without SMTP_HOST (and without any other
// setting), the emails are off. An error names what is missing or inconsistent: the backend refuses to
// start with it, a half configured email would fail later, silently.
func ParseConfig(getenv func(string) string) (Config, error) {
	host := strings.TrimSpace(getenv("SMTP_HOST"))
	if host == "" {
		var set []string
		for _, name := range variables[1:] {
			if getenv(name) != "" {
				set = append(set, name)
			}
		}
		if len(set) > 0 {
			return Config{}, fmt.Errorf("email: %s set without SMTP_HOST: set SMTP_HOST to send emails, or remove them",
				strings.Join(set, ", "))
		}
		return Config{}, nil
	}
	if strings.ContainsAny(host, " \t/:") {
		return Config{}, fmt.Errorf("email: SMTP_HOST is %q: a host name or an IP address only, the port goes "+
			"in SMTP_PORT", host)
	}
	c := Config{Host: host, Username: getenv("SMTP_USERNAME"), Password: getenv("SMTP_PASSWORD"),
		TLS: strings.ToLower(strings.TrimSpace(getenv("SMTP_TLS")))}
	if c.TLS == "" {
		c.TLS = TLSStartTLS
	}
	if c.TLS != TLSStartTLS && c.TLS != TLSImplicit && c.TLS != TLSNone {
		return Config{}, fmt.Errorf("email: SMTP_TLS is %q, it must be %s (port 587), %s (port 465) or %s",
			c.TLS, TLSStartTLS, TLSImplicit, TLSNone)
	}
	port := strings.TrimSpace(getenv("SMTP_PORT"))
	if port == "" {
		return Config{}, fmt.Errorf("email: SMTP_PORT is missing (587 with SMTP_TLS=%s, 465 with %s)", TLSStartTLS, TLSImplicit)
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return Config{}, fmt.Errorf("email: SMTP_PORT is %q, not a port", port)
	}
	c.Port = p
	if (c.Username == "") != (c.Password == "") {
		return Config{}, errors.New("email: SMTP_USERNAME and SMTP_PASSWORD go together: set both, or neither " +
			"for a server without authentication")
	}
	if c.TLS == TLSNone && !isLocal(host) {
		return Config{}, fmt.Errorf("email: SMTP_TLS=%s towards %s: the messages, and the password, would cross the "+
			"network in clear. Use %s or %s, %s is for a relay on the same machine", TLSNone, host, TLSStartTLS,
			TLSImplicit, TLSNone)
	}
	from := strings.TrimSpace(getenv("MAIL_FROM"))
	if from == "" {
		return Config{}, errors.New("email: MAIL_FROM is missing: the sender of the emails, an address of a domain " +
			"verified at your sending service, e.g. MySportWeb <sport@example.com>")
	}
	address, err := mail.ParseAddress(from)
	if err != nil {
		return Config{}, fmt.Errorf("email: MAIL_FROM is %q, not an email address: %v", from, err)
	}
	c.From = *address
	return c, nil
}

// isLocal tells whether a host is the local machine
func isLocal(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Message is an email to one recipient, in plain text and in HTML
type Message struct {
	To      mail.Address
	Subject string
	Text    string
	HTML    string
}

// Mailer sends the emails
type Mailer interface {
	Send(ctx context.Context, m Message) error
}

// SMTP sends the emails through the SMTP server of its configuration
type SMTP struct {
	Config Config
	// TLSConfig is the one of the encrypted connections, nil for the defaults (the tests trust their server)
	TLSConfig *tls.Config
}

func (s *SMTP) tlsConfig() *tls.Config {
	if s.TLSConfig != nil {
		return s.TLSConfig
	}
	return &tls.Config{ServerName: s.Config.Host, MinVersion: tls.VersionTLS12}
}

// Send sends a message
func (s *SMTP) Send(ctx context.Context, m Message) error {
	body, err := Compose(s.Config.From, m, time.Now())
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	addr := net.JoinHostPort(s.Config.Host, strconv.Itoa(s.Config.Port))
	dialer := &net.Dialer{}
	var conn net.Conn
	if s.Config.TLS == TLSImplicit {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: s.tlsConfig()}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("connecting to %s: %w", addr, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}
	client, err := smtp.NewClient(conn, s.Config.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("SMTP server %s: %w", addr, err)
	}
	defer client.Close()
	if s.Config.TLS == TLSStartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("SMTP server %s doesn't offer STARTTLS: set SMTP_TLS=%s with its port 465, or check the port", addr, TLSImplicit)
		}
		if err := client.StartTLS(s.tlsConfig()); err != nil {
			return fmt.Errorf("STARTTLS with %s: %w", addr, err)
		}
	}
	if s.Config.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", s.Config.Username, s.Config.Password, s.Config.Host)); err != nil {
			return fmt.Errorf("authentication at %s refused (check SMTP_USERNAME and SMTP_PASSWORD): %w", addr, err)
		}
	}
	if err := client.Mail(s.Config.From.Address); err != nil {
		return fmt.Errorf("sender %s refused (is its domain verified at the service?): %w", s.Config.From.Address, err)
	}
	if err := client.Rcpt(m.To.Address); err != nil {
		return fmt.Errorf("recipient refused: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(body); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("message refused: %w", err)
	}
	return client.Quit()
}

// Compose returns the message as sent: its headers, then its text and HTML versions (multipart/alternative)
func Compose(from mail.Address, m Message, now time.Time) ([]byte, error) {
	var buf bytes.Buffer
	id := make([]byte, 12)
	rand.Read(id)
	domain := "mysportweb"
	if at := strings.LastIndex(from.Address, "@"); at >= 0 {
		domain = from.Address[at+1:]
	}
	mixed := multipart.NewWriter(&buf)
	headers := []struct{ name, value string }{
		{"From", from.String()},
		{"To", m.To.String()},
		{"Subject", mime.QEncoding.Encode("utf-8", m.Subject)},
		{"Date", now.Format(time.RFC1123Z)},
		{"Message-ID", "<" + hex.EncodeToString(id) + "@" + domain + ">"},
		{"MIME-Version", "1.0"},
		{"Content-Type", "multipart/alternative; boundary=" + mixed.Boundary()},
		// the automatic emails: no out of office answer
		{"Auto-Submitted", "auto-generated"},
	}
	var head bytes.Buffer
	for _, h := range headers {
		if strings.ContainsAny(h.value, "\r\n") {
			return nil, fmt.Errorf("header %s with a line break", h.name)
		}
		fmt.Fprintf(&head, "%s: %s\r\n", h.name, h.value)
	}
	head.WriteString("\r\n")
	for _, part := range []struct{ contentType, content string }{
		{"text/plain; charset=utf-8", m.Text},
		{"text/html; charset=utf-8", m.HTML},
	} {
		if part.content == "" {
			continue
		}
		w, err := mixed.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {part.contentType},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		if err != nil {
			return nil, err
		}
		qp := quotedprintable.NewWriter(w)
		// the lines of a message end with CRLF, whatever the text was written with
		lines := strings.ReplaceAll(strings.ReplaceAll(part.content, "\r\n", "\n"), "\n", "\r\n")
		if _, err := qp.Write([]byte(lines)); err != nil {
			return nil, err
		}
		if err := qp.Close(); err != nil {
			return nil, err
		}
	}
	if err := mixed.Close(); err != nil {
		return nil, err
	}
	return append(head.Bytes(), buf.Bytes()...), nil
}
