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

	"github.com/rs/zerolog"
)

// The encryptions of the connection to the SMTP server
const (
	TLSStartTLS = "starttls" // the connection becomes encrypted after its start: port 587, the default
	TLSImplicit = "tls"      // encrypted from the start: port 465
	TLSNone     = "none"     // never encrypted: only towards the local host (a relay on the same machine)
)

const (
	// sendTimeout bounds the sending of a message, from the connection to the end: under the 60 s the web
	// application waits for the test email, so that it gets the error
	sendTimeout = 40 * time.Second
	// dialTimeout bounds the connection: a blocked port doesn't answer, it times out
	dialTimeout = 15 * time.Second
)

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
	// Logger logs each email: its steps (debug), and its success or failure with the step and a hint
	Logger zerolog.Logger
	// TLSConfig is the one of the encrypted connections, nil for the defaults (the tests trust their server)
	TLSConfig *tls.Config
}

func (s *SMTP) tlsConfig() *tls.Config {
	if s.TLSConfig != nil {
		return s.TLSConfig
	}
	return &tls.Config{ServerName: s.Config.Host, MinVersion: tls.VersionTLS12}
}

// Send sends a message. Each step is logged with its duration, a failure with the step and a hint: the
// errors of SMTP servers are often terse.
func (s *SMTP) Send(ctx context.Context, m Message) error {
	body, err := Compose(s.Config.From, m, time.Now())
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	addr := net.JoinHostPort(s.Config.Host, strconv.Itoa(s.Config.Port))
	started := time.Now()
	log := s.Logger.With().Str("smtp", addr).Str("tls", s.Config.TLS).Str("to", m.To.Address).Logger()
	stage := "connect"
	step := func(next string) {
		log.Debug().Str("step", stage).Dur("elapsed", time.Since(started)).Msg("smtp: step done")
		stage = next
	}
	fail := func(err error) error {
		err = fmt.Errorf("%s (%s, after %s): %w", stageText[stage], addr, time.Since(started).Round(time.Millisecond), err)
		if hint := Hint(err); hint != "" {
			err = fmt.Errorf("%w. %s", err, hint)
		}
		log.Error().Err(err).Str("step", stage).Dur("elapsed", time.Since(started)).Msg("smtp: email not sent")
		return err
	}
	log.Info().Msg("smtp: sending an email")

	dialer := &net.Dialer{Timeout: dialTimeout}
	var conn net.Conn
	if s.Config.TLS == TLSImplicit {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: s.tlsConfig()}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fail(err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}
	step("greeting")
	client, err := smtp.NewClient(conn, s.Config.Host)
	if err != nil {
		conn.Close()
		return fail(err)
	}
	defer client.Close()
	if s.Config.TLS == TLSStartTLS {
		step("starttls")
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fail(fmt.Errorf("the server doesn't offer STARTTLS: set SMTP_TLS=%s with its port 465, or check the port", TLSImplicit))
		}
		if err := client.StartTLS(s.tlsConfig()); err != nil {
			return fail(err)
		}
	}
	if s.Config.Username != "" {
		step("auth")
		if err := client.Auth(smtp.PlainAuth("", s.Config.Username, s.Config.Password, s.Config.Host)); err != nil {
			return fail(err)
		}
	}
	step("sender")
	if err := client.Mail(s.Config.From.Address); err != nil {
		return fail(err)
	}
	step("recipient")
	if err := client.Rcpt(m.To.Address); err != nil {
		return fail(err)
	}
	step("data")
	w, err := client.Data()
	if err != nil {
		return fail(err)
	}
	if _, err := w.Write(body); err != nil {
		return fail(err)
	}
	if err := w.Close(); err != nil {
		return fail(err)
	}
	step("quit")
	// accepted: a failing QUIT doesn't unsend it
	if err := client.Quit(); err != nil {
		log.Debug().Err(err).Msg("smtp: QUIT failed after the message was accepted")
	}
	log.Info().Dur("elapsed", time.Since(started)).Msg("smtp: email accepted by the server")
	return nil
}

// stageText says what failed, by step of Send
var stageText = map[string]string{
	"connect":   "connecting to the SMTP server",
	"greeting":  "reading the greeting of the SMTP server",
	"starttls":  "starting TLS (STARTTLS)",
	"auth":      "authenticating (SMTP_USERNAME, SMTP_PASSWORD)",
	"sender":    "sending the sender (MAIL_FROM)",
	"recipient": "sending the recipient",
	"data":      "sending the message",
	"quit":      "ending the session",
}

// Hint explains the common failures of a connection to an SMTP server, "" for the others
func Hint(err error) string {
	var dnsErr *net.DNSError
	var certErr *tls.CertificateVerificationError
	var netErr net.Error
	text := err.Error()
	switch {
	case errors.As(err, &dnsErr):
		return "Hint: SMTP_HOST isn't a known host name, check its spelling and the DNS of the server"
	case strings.Contains(text, "connection refused"):
		return "Hint: nothing listens on this port of the host, check SMTP_PORT (587 with starttls, 465 with tls)"
	case errors.As(err, &certErr), strings.Contains(text, "x509:"), strings.Contains(text, "tls: "):
		return "Hint: the TLS connection failed, check SMTP_TLS against the port (starttls with 587, tls with 465)"
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return "Hint: no answer in time. Many hosting providers and firewalls block the outgoing SMTP ports " +
			"(25, 465, 587): check that the server can reach the host on this port, e.g. with " +
			"nc -vz <host> <port> from the docker host"
	case strings.Contains(text, "535") || strings.Contains(text, "authentication"):
		return "Hint: the credentials are refused, check SMTP_USERNAME and SMTP_PASSWORD (for SendGrid, the " +
			"username is the word apikey)"
	case strings.Contains(text, "550") || strings.Contains(text, "553") || strings.Contains(text, "554"):
		return "Hint: the server refuses the sender or the message: verify the domain of MAIL_FROM at your sending service"
	}
	return ""
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
