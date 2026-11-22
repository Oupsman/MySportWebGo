package main

import (
	"MySportWeb/internal/pkg/apicontrollers"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/vars"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/joho/godotenv"
)

const adminUsage = `usage: mysportweb admin promote <username>     makes the user an admin
       mysportweb admin demote <username>      makes the admin a user
       mysportweb admin reset-2fa <username>   turns off their two-factor authentication (lost phone and
                                               recovery codes): they set it up again, an admin at once
       mysportweb admin reset-password <username>
                                               prints a link, valid 24 hours, to choose a new password

It connects to the database as the server does (the DB* variables, completed by the .env of the current
directory). In the stack: docker compose exec backend mysportweb admin promote <username>`

// adminCommand runs "mysportweb admin ...": the roles changed from the command line, when no admin can do
// it in the application (the admin lost their password, or left)
func adminCommand(args []string, stdout, stderr io.Writer) int {
	known := map[string]bool{"promote": true, "demote": true, "reset-2fa": true, "reset-password": true}
	if len(args) != 2 || !known[args[0]] {
		fmt.Fprintln(stderr, adminUsage)
		return 2
	}
	_ = godotenv.Load()
	// the links of reset-password open the web application
	vars.Domain = os.Getenv("DOMAIN")
	dsn, err := vars.DatabaseDSN(os.Getenv)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	db, err := models.ConnectToDB("postgres", dsn)
	if err != nil {
		// never the DSN: it holds the password of the database
		fmt.Fprintln(stderr, "error: cannot connect to the database:", err)
		return 1
	}
	return runAdmin(db, args, stdout, stderr)
}

// runAdmin changes the role of args[1]: admin for promote, user for demote; or turns off their two-factor
// authentication for reset-2fa
func runAdmin(db *models.DB, args []string, stdout, stderr io.Writer) int {
	if args[0] == "reset-password" {
		var user models.Users
		err := db.Where("username = ?", args[1]).Limit(1).Find(&user).Error
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		if user.ID == 0 {
			fmt.Fprintf(stderr, "error: no user %q (the username is case sensitive)\n", args[1])
			return 1
		}
		link, reset, err := apicontrollers.NewPasswordReset(db, user.ID, nil)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		fmt.Fprintf(stdout, "give %s this link, valid until %s, used once:\n%s\n", args[1],
			reset.ExpiresAt.Format(time.RFC1123), link)
		if user.TOTPEnabledAt != nil {
			fmt.Fprintln(stdout, "their two-factor authentication stays: see reset-2fa if they lost it too")
		}
		return 0
	}
	if args[0] == "reset-2fa" {
		found, err := db.ResetTOTP(args[1])
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		if !found {
			fmt.Fprintf(stderr, "error: no user %q (the username is case sensitive)\n", args[1])
			return 1
		}
		fmt.Fprintf(stdout, "the two-factor authentication of %s is off: they set it up again in their profile\n", args[1])
		return 0
	}
	role := models.RoleAdmin
	if args[0] == "demote" {
		role = models.RoleUser
	}
	found, err := db.SetRole(args[1], role)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if !found {
		fmt.Fprintf(stderr, "error: no user %q (the username is case sensitive)\n", args[1])
		return 1
	}
	fmt.Fprintf(stdout, "%s is now %s\n", args[1], role)
	if hasAdmin, err := db.HasAdmin(); err == nil && !hasAdmin {
		fmt.Fprintln(stdout, "the instance has no admin anymore: its next start logs a setup code to create one")
	}
	return 0
}
