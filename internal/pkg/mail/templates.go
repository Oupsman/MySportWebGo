package mail

import (
	"bytes"
	"fmt"
	"html/template"
	"net/mail"
	"strings"
	"time"
)

// htmlLayout wraps the HTML version of the emails: one paragraph per line of the text, the link as a button.
// The values are escaped by html/template.
var htmlLayout = template.Must(template.New("email").Parse(`<!doctype html>
<html><body style="margin:0;padding:24px;background:#f4f5f7;font-family:-apple-system,Segoe UI,Roboto,sans-serif;color:#222">
<div style="max-width:560px;margin:0 auto;background:#fff;border-radius:8px;padding:24px">
<h1 style="font-size:20px;margin:0 0 16px">{{.Title}}</h1>
{{range .Paragraphs}}<p style="line-height:1.5;margin:0 0 12px">{{.}}</p>
{{end}}{{if .Link}}<p style="margin:20px 0"><a href="{{.Link}}" style="background:#2a78d6;color:#fff;padding:10px 18px;border-radius:6px;text-decoration:none;display:inline-block">{{.Button}}</a></p>
<p style="font-size:12px;color:#666;word-break:break-all">{{.Link}}</p>
{{end}}{{range .Footer}}<p style="font-size:12px;color:#666;margin:16px 0 0">{{.}}</p>
{{end}}</div></body></html>`))

// content is what an email says: rendered as text and as HTML
type content struct {
	Title      string
	Paragraphs []string
	Link       string
	Button     string
	Footer     []string
}

func (c content) message(to mail.Address, subject string) Message {
	var text strings.Builder
	for _, p := range c.Paragraphs {
		text.WriteString(p + "\n\n")
	}
	if c.Link != "" {
		text.WriteString(c.Link + "\n\n")
	}
	for _, f := range c.Footer {
		text.WriteString("-- \n" + f + "\n")
	}
	var html bytes.Buffer
	if err := htmlLayout.Execute(&html, c); err != nil {
		// the layout is fixed: only a bug of it fails, the text version still goes
		html.Reset()
	}
	return Message{To: to, Subject: subject, Text: text.String(), HTML: html.String()}
}

// recipient is a user as the To of an email
func recipient(username, email string) mail.Address {
	return mail.Address{Name: username, Address: email}
}

// Verification asks a user to confirm their email address
func Verification(username, email, link string, valid time.Duration) Message {
	return content{
		Title: "Confirm your email address",
		Paragraphs: []string{
			fmt.Sprintf("Hello %s,", username),
			fmt.Sprintf("Confirm that %s is your email address on MySportWeb: the password resets and the notifications "+
				"you choose are sent to it. The link is valid for %d hours.", email, int(valid.Hours())),
		},
		Link: link, Button: "Confirm my address",
		Footer: []string{"You didn't ask for it? Ignore this email: the address won't be used."},
	}.message(recipient(username, email), "Confirm your email address")
}

// PasswordReset gives a user the link to choose a new password, asked from the login page
func PasswordReset(username, email, link string, valid time.Duration) Message {
	return content{
		Title: "Choose a new password",
		Paragraphs: []string{
			fmt.Sprintf("Hello %s,", username),
			fmt.Sprintf("Someone, hopefully you, asked to reset the password of your MySportWeb account. The link "+
				"is valid for %d hours, and works once. Your two-factor authentication, if you have it, stays.",
				int(valid.Hours())),
		},
		Link: link, Button: "Choose a new password",
		Footer: []string{"You didn't ask for it? Ignore this email: your password doesn't change."},
	}.message(recipient(username, email), "Reset your MySportWeb password")
}

// Invitation invites someone to register
func Invitation(email, inviter, link string, expires time.Time) Message {
	return content{
		Title: "You're invited to MySportWeb",
		Paragraphs: []string{
			fmt.Sprintf("%s invites you to create an account on their MySportWeb, to keep your sport activities.", inviter),
			fmt.Sprintf("The invitation registers one account with this address, until %s.", expires.Format("January 2, 2006")),
		},
		Link: link, Button: "Create my account",
	}.message(mail.Address{Address: email}, inviter+" invites you to MySportWeb")
}

// Notification is a notification of the application by email, with the link of the page it is about
func Notification(username, email, title, body, link, settings string) Message {
	return content{
		Title:      title,
		Paragraphs: []string{body},
		Link:       link, Button: "Open MySportWeb",
		Footer: []string{"You receive these notifications by email, turn them off in your profile: " + settings},
	}.message(recipient(username, email), title)
}

// Test checks the configuration of the emails
func Test(username, email, host string) Message {
	return content{
		Title: "MySportWeb can send emails",
		Paragraphs: []string{
			fmt.Sprintf("Hello %s,", username),
			fmt.Sprintf("This test went through %s: the emails of MySportWeb work. Check that it didn't land in "+
				"your spam folder: if it did, verify the domain of the sender at your sending service (SPF, DKIM).", host),
		},
	}.message(recipient(username, email), "MySportWeb email test")
}
