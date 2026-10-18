package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/gotd/td/tgerr"
	"github.com/oovets/imessage-tui/config"
	telegramprovider "github.com/oovets/imessage-tui/provider/telegram"
	"github.com/oovets/imessage-tui/termqr"
)

const usage = `usage:
  imessage-tui                          start the client
  imessage-tui telegram login [--phone] log in to a telegram account (QR code by default)
  imessage-tui telegram logout [<id>]   log out and forget an account
  imessage-tui telegram accounts        list logged-in accounts
`

// runCommand runs a subcommand and returns the exit code. These run before
// the TUI and instead of it, so unlike the rest of the app they talk to the
// terminal directly.
func runCommand(args []string) int {
	switch {
	case len(args) == 1 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help"):
		fmt.Print(usage)
		return 0
	case len(args) >= 2 && args[0] == "telegram":
		if err := runTelegram(args[1], args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "imessage-tui: "+err.Error())
			return 1
		}
		return 0
	default:
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
}

func runTelegram(command string, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	switch command {
	case "login":
		usePhone := len(args) == 1 && args[0] == "--phone"
		if len(args) > 1 || (len(args) == 1 && !usePhone) {
			return errors.New("usage: imessage-tui telegram login [--phone]")
		}
		return telegramLogin(ctx, usePhone, newTerminal())
	case "logout":
		return telegramLogout(ctx, args)
	case "accounts":
		accounts, err := config.LoadTelegramAccounts()
		if err != nil {
			return err
		}
		if len(accounts) == 0 {
			fmt.Println("no telegram accounts; run: imessage-tui telegram login")
		}
		for _, account := range accounts {
			fmt.Printf("%s  %s\n", account.ID, account.Name)
		}
		return nil
	default:
		return fmt.Errorf("unknown telegram command %q\n%s", command, usage)
	}
}

func telegramLogin(ctx context.Context, usePhone bool, t *terminal) error {
	creds, err := telegramCredentials(t)
	if err != nil {
		return err
	}
	result, err := telegramprovider.Login(ctx, creds, usePhone, t)
	if err != nil {
		if tgerr.Is(err, "API_ID_INVALID", "API_ID_PUBLISHED_FLOOD") {
			return errors.New("telegram does not accept this api id and hash; check them at https://my.telegram.org")
		}
		return fmt.Errorf("telegram login: %w", err)
	}
	if err := config.SaveTelegramAccount(config.TelegramAccount{
		ID:      result.Account.ID,
		Name:    result.Account.Name,
		APIID:   creds.APIID,
		APIHash: creds.APIHash,
		Session: result.Session,
	}); err != nil {
		// Without the keyring there is nowhere safe to put the session, and
		// a plaintext fallback is exactly what it must not get.
		return fmt.Errorf("logged in, but saving the session to the keyring failed: %w", err)
	}
	fmt.Printf("\nlogged in to telegram as %s (%s)\n", result.Account.Name, result.Account.ID)
	return nil
}

// telegramCredentials finds the application id and hash: the environment,
// then an account already logged in (the same registration serves every
// account), then a prompt.
func telegramCredentials(t *terminal) (telegramprovider.Credentials, error) {
	if id, hash, ok := config.TelegramAPICredentials(); ok {
		return telegramprovider.Credentials{APIID: id, APIHash: hash}, nil
	}
	if accounts, err := config.LoadTelegramAccounts(); err == nil {
		for _, account := range accounts {
			if account.APIID > 0 && account.APIHash != "" {
				return telegramprovider.Credentials{APIID: account.APIID, APIHash: account.APIHash}, nil
			}
		}
	}

	fmt.Println("telegram needs an api id and hash of your own: https://my.telegram.org → API development tools")
	fmt.Println("(or set BB_TELEGRAM_API_ID and BB_TELEGRAM_API_HASH)")
	idText, err := t.ask("api id: ")
	if err != nil {
		return telegramprovider.Credentials{}, err
	}
	id, err := strconv.Atoi(strings.TrimSpace(idText))
	if err != nil || id <= 0 {
		return telegramprovider.Credentials{}, errors.New("the api id is a number")
	}
	hash, err := t.ask("api hash: ")
	if err != nil {
		return telegramprovider.Credentials{}, err
	}
	if hash = strings.TrimSpace(hash); hash == "" {
		return telegramprovider.Credentials{}, errors.New("no api hash given")
	}
	return telegramprovider.Credentials{APIID: id, APIHash: hash}, nil
}

func telegramLogout(ctx context.Context, args []string) error {
	accounts, err := config.LoadTelegramAccounts()
	if err != nil {
		return err
	}
	var account *config.TelegramAccount
	switch {
	case len(args) == 1:
		for i := range accounts {
			if accounts[i].ID == args[0] {
				account = &accounts[i]
			}
		}
		if account == nil {
			return fmt.Errorf("no telegram account %s; see: imessage-tui telegram accounts", args[0])
		}
	case len(args) == 0 && len(accounts) == 1:
		account = &accounts[0]
	case len(args) == 0 && len(accounts) == 0:
		return errors.New("no telegram account is logged in")
	default:
		return errors.New("several accounts are logged in; name one: imessage-tui telegram logout <id>")
	}

	creds := telegramprovider.Credentials{APIID: account.APIID, APIHash: account.APIHash}
	store := config.TelegramSessionStore{AccountID: account.ID}
	if err := telegramprovider.Logout(ctx, creds, store, config.ErrTelegramSessionNotFound); err != nil {
		// Forget it locally anyway: the user asked for it gone, and a
		// session the server already dropped cannot be logged out of.
		fmt.Fprintf(os.Stderr, "telegram did not confirm the logout (%v); forgetting the session anyway\n", err)
	}
	if err := config.RemoveTelegramAccount(account.ID); err != nil {
		return err
	}
	fmt.Printf("logged out of telegram account %s (%s)\n", account.Name, account.ID)
	return nil
}

// terminal is the login conversation on stdin/stdout.
type terminal struct {
	in  *bufio.Reader
	out io.Writer
}

func newTerminal() *terminal {
	return &terminal{in: bufio.NewReader(os.Stdin), out: os.Stdout}
}

func (t *terminal) ask(prompt string) (string, error) {
	fmt.Fprint(t.out, prompt)
	line, err := t.in.ReadString('\n')
	if err != nil && !(errors.Is(err, io.EOF) && line != "") {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func (t *terminal) ShowQR(url string) error {
	code, err := termqr.Render(url)
	if err != nil {
		return err
	}
	// The code is redrawn whenever Telegram rotates the token, so start from
	// a clean screen each time rather than stacking codes.
	fmt.Fprint(t.out, "\x1b[H\x1b[2J")
	fmt.Fprint(t.out, code)
	fmt.Fprintln(t.out, "\nscan with telegram on your phone: settings → devices → link desktop device")
	fmt.Fprintln(t.out, "(no camera? ctrl+c and run: imessage-tui telegram login --phone)")
	return nil
}

func (t *terminal) Phone() (string, error) { return t.ask("phone number, with country code: ") }

func (t *terminal) Code() (string, error) { return t.ask("code from telegram: ") }

func (t *terminal) Password() (string, error) {
	fd := os.Stdin.Fd()
	if !term.IsTerminal(fd) {
		return t.ask("two-step verification password: ")
	}
	fmt.Fprint(t.out, "two-step verification password: ")
	password, err := term.ReadPassword(fd)
	fmt.Fprintln(t.out)
	return string(password), err
}
