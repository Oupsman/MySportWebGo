package main

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/vars"
	"fmt"
	"io"
	"os"

	"github.com/joho/godotenv"
)

const adminUsage = `usage: mysportweb admin promote <username>   makes the user an admin
       mysportweb admin demote <username>    makes the admin a user

It connects to the database as the server does (the DB* variables, completed by the .env of the current
directory). In the stack: docker compose exec backend mysportweb admin promote <username>`

// adminCommand runs "mysportweb admin ...": the roles changed from the command line, when no admin can do
// it in the application (the admin lost their password, or left)
func adminCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 || (args[0] != "promote" && args[0] != "demote") {
		fmt.Fprintln(stderr, adminUsage)
		return 2
	}
	_ = godotenv.Load()
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

// runAdmin changes the role of args[1]: admin for promote, user for demote
func runAdmin(db *models.DB, args []string, stdout, stderr io.Writer) int {
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
