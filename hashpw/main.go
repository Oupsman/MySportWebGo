// hashpw resets a user's password, or prints the bcrypt hash of a password.
//
// Usage, from the backend directory:
//
//	go run ./hashpw -user <username>
//	    prompts for the new password (not echoed, asked twice) and writes it in the database
//	    configured in .env, then checks that it can be verified
//	go run ./hashpw 'my-new-password'
//	echo 'my-new-password' | go run ./hashpw
//	    only prints the hash, for UPDATE users SET password = '<hash>' WHERE username = '<username>';
package main

import (
	"MySportWeb/internal/pkg/utils"
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"golang.org/x/term"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	username := flag.String("user", "", "username whose password is reset in the database of .env")
	flag.Parse()

	if *username != "" {
		if err := resetPassword(*username); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	password, err := passwordFromArgsOrStdin()
	if err != nil {
		fmt.Fprintln(os.Stderr, "usage: hashpw -user <username> | hashpw <password> | <password on stdin> hashpw")
		os.Exit(1)
	}
	// same function as the app, so the hash always matches what Login expects
	hash, err := utils.GenerateHashPassword(password)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(hash)
}

func passwordFromArgsOrStdin() (string, error) {
	if flag.NArg() > 0 {
		return flag.Arg(0), nil
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	password := strings.TrimRight(line, "\r\n")
	if password == "" {
		if err == nil {
			err = errors.New("empty password")
		}
		return "", err
	}
	return password, nil
}

// promptPassword reads a password from the terminal without echoing it
func promptPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("reading the password (run it in a terminal): %w", err)
	}
	return string(password), nil
}

func resetPassword(username string) error {
	env, err := godotenv.Read(".env")
	if err != nil {
		return fmt.Errorf("reading .env (run from the backend directory): %w", err)
	}
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		env["DBHOST"], env["DBUSER"], env["DBPASSWORD"], env["DBNAME"], env["DBPORT"])
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return err
	}

	var count int64
	if err := db.Table("users").Where("username = ?", username).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("no user %q in database %s", username, env["DBNAME"])
	}

	password, err := promptPassword("New password: ")
	if err != nil {
		return err
	}
	confirmation, err := promptPassword("Confirm password: ")
	if err != nil {
		return err
	}
	if password == "" || password != confirmation {
		return errors.New("passwords are empty or don't match, nothing changed")
	}

	hash, err := utils.GenerateHashPassword(password)
	if err != nil {
		return err
	}
	err = db.Table("users").Where("username = ?", username).
		Updates(map[string]interface{}{"password": hash, "updated_at": gorm.Expr("now()")}).Error
	if err != nil {
		return err
	}

	// read it back and verify it the way Login does
	var stored string
	if err := db.Table("users").Where("username = ?", username).Select("password").Scan(&stored).Error; err != nil {
		return err
	}
	if !utils.CompareHashPassword(password, stored) {
		return errors.New("the stored hash does not verify, something else changed it")
	}
	fmt.Fprintf(os.Stderr, "password of %q updated in database %s\n", username, env["DBNAME"])
	return nil
}
