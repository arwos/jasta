package admin

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"

	"go.osspkg.com/goppy/v3/pkg/console"
	"go.osspkg.com/goppy/v3/plugin"
	"golang.org/x/term"

	"go.arwos.org/jasta/internal/auth/owner"
)

const (
	defaultOwnerKeyPath = "/etc/jasta/master.key"
	defaultAdminDBPath  = "/var/lib/jasta/admin.sqlite"
)

func OwnerBootstrapCommand(ctx context.Context, _ plugin.DIResolver, setter console.CommandSetter) {
	setter.Setup("owner-bootstrap", "Create the local owner and enroll mandatory TOTP")
	setter.Flag(func(flags console.FlagsSetter) {
		flags.StringVar("database", defaultAdminDBPath, "SQLite file used by jasta admin")
		flags.StringVar("key-file", defaultOwnerKeyPath, "Root-owned 32-byte TOTP encryption key")
		flags.StringVar("issuer", "Jasta", "TOTP authenticator issuer")
		flags.StringVar("account", "owner", "TOTP authenticator account label")
	})
	setter.ExecFunc(func(databasePath, keyPath, issuer, account string) {
		requireTerminal()
		database, err := openAdminDatabase(ctx, databasePath)
		console.FatalIfErr(err, "open admin database")
		defer database.Close()
		exists, err := owner.OwnerExists(ctx, database.Tag("master"))
		console.FatalIfErr(err, "check owner")
		if exists {
			console.Fatalf("owner is already bootstrapped; use owner-reset first")
		}
		encryptionKey, err := readOwnerEncryptionKey(keyPath)
		console.FatalIfErr(err, "read TOTP encryption key")
		password, err := readPassword("Owner password: ")
		console.FatalIfErr(err, "read password")
		confirmation, err := readPassword("Confirm owner password: ")
		console.FatalIfErr(err, "read password confirmation")
		if password != confirmation {
			console.Fatalf("passwords do not match")
		}
		enrollment, err := owner.BeginEnrollment(issuer, account)
		console.FatalIfErr(err, "begin TOTP enrollment")
		console.Infof("Add this account to your authenticator before continuing:\n%s\nSecret: %s", enrollment.URL, enrollment.Secret)
		passcode, err := readLine("Enter the current six-digit authenticator code: ")
		console.FatalIfErr(err, "read TOTP code")
		codes, err := owner.Bootstrap(ctx, database.Tag("master"), password, enrollment.Secret, passcode, encryptionKey)
		console.FatalIfErr(err, "bootstrap owner")
		console.Infof("Owner created. Save these recovery codes now; they cannot be displayed again:")
		for _, code := range codes {
			console.Infof("%s", code)
		}
	})
}

func OwnerResetCommand(ctx context.Context, _ plugin.DIResolver, setter console.CommandSetter) {
	setter.Setup("owner-reset", "Locally remove the owner, TOTP enrollment, recovery codes, and refresh sessions")
	setter.Flag(func(flags console.FlagsSetter) {
		flags.StringVar("database", defaultAdminDBPath, "SQLite file used by jasta admin")
	})
	setter.ExecFunc(func(databasePath string) {
		requireTerminal()
		confirmation, err := readLine("Type RESET to remove the local owner and all auth sessions: ")
		console.FatalIfErr(err, "read reset confirmation")
		if confirmation != "RESET" {
			console.Fatalf("reset cancelled")
		}
		database, err := openAdminDatabase(ctx, databasePath)
		console.FatalIfErr(err, "open admin database")
		defer database.Close()
		if err = owner.Reset(ctx, database.Tag("master")); err != nil {
			console.FatalIfErr(err, "reset owner")
		}
		console.Infof("Owner authentication state removed; run owner-bootstrap to configure a new owner")
	})
}

func requireTerminal() {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		console.Fatalf("this command requires an interactive terminal")
	}
}

func readPassword(prompt string) (string, error) {
	if _, err := fmt.Fprint(os.Stdout, prompt); err != nil {
		return "", err
	}
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	if _, printErr := fmt.Fprintln(os.Stdout); printErr != nil && err == nil {
		err = printErr
	}
	if err != nil {
		return "", err
	}
	return string(password), nil
}

func readLine(prompt string) (string, error) {
	if _, err := fmt.Fprint(os.Stdout, prompt); err != nil {
		return "", err
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && (err != io.EOF || line == "") {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func readOwnerEncryptionKey(path string) ([]byte, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open key file without following symlinks: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect key file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("key file must be a regular file, not a symlink")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 {
		return nil, errors.New("key file must be owned by root")
	}
	if mode := info.Mode().Perm(); mode != 0600 && mode != 0640 {
		return nil, errors.New("key file permissions must be 0600 or 0640")
	}
	data, err := io.ReadAll(io.LimitReader(file, 1024))
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}
	if len(data) >= 1024 {
		return nil, errors.New("key file is too large")
	}
	if len(data) == 32 {
		return data, nil
	}
	encoded := strings.TrimSpace(string(data))
	decoded, err := hex.DecodeString(encoded)
	if err != nil || len(decoded) != 32 {
		return nil, errors.New("key file must contain 32 raw bytes or 64 hexadecimal characters")
	}
	return decoded, nil
}
