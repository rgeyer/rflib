package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/ryan/rflib/internal/app"
	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/store"
)

func newUserCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "user", Short: "Manage the accounts of a server with accounts (admin)",
		Long: `Accounts are for rflib serve --accounts, where each person signs in and has
a library of their own. These commands work on the database directly, as the
server's admin.

The first account is the admin's, and takes the library you have now:
rflib user add NAME --admin. Later accounts come from the web interface's
sign-up page, as the server's --signup allows, or from rflib user add; each
gets a new library, named after it.

A name is a callsign (its form is checked, but not that you hold it) or a
username. Passwords are read from the terminal without echo, or a line from
standard input when it isn't a terminal.`}
	cmd.AddCommand(newUserListCommand(), newUserAddCommand(), newUserRoleCommand(), newUserPasswordCommand(), newUserRemoveCommand(), newUserInviteCommand())
	return cmd
}

// withAccounts opens the database's accounts.
func withAccounts(cmd *cobra.Command, fn func(*app.Accounts) error) error {
	return withDB(cmd, func(d store.Store) error { return fn(app.NewAccounts(d)) })
}

var userListing = listing[domain.User]{
	fields: []field[domain.User]{
		{name: "name", text: func(u domain.User) string { return u.Name }},
		{name: "display", text: func(u domain.User) string { return u.DisplayName }},
		{name: "email", text: func(u domain.User) string { return u.Email }},
		{name: "role", text: func(u domain.User) string { return string(u.Role) }},
		{name: "status", text: func(u domain.User) string {
			if u.Disabled {
				return "disabled"
			}
			return "active"
		}},
		{name: "callsign", text: func(u domain.User) string {
			if u.Callsign() {
				return "yes (unverified)"
			}
			return ""
		}, json: func(u domain.User) any { return u.Callsign() }},
		{name: "created", text: func(u domain.User) string { return u.CreatedAt.Local().Format("2006-01-02") }, json: func(u domain.User) any { return u.CreatedAt }},
		{name: "id", text: func(u domain.User) string { return u.ID }},
	},
	defaults: []string{"name", "display", "role", "status", "callsign", "created"},
}

func newUserListCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "list", Short: "List accounts", Args: cobra.NoArgs}
	userListing.flags(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := userListing.check(cmd); err != nil {
			return err
		}
		return withAccounts(cmd, func(a *app.Accounts) error {
			us, err := a.Users(ctx(cmd))
			if err != nil {
				return err
			}
			return userListing.write(cmd, us)
		})
	}
	return cmd
}

func newUserAddCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "add NAME", Short: "Add an account", Args: cobra.ExactArgs(1)}
	cmd.Flags().Bool("admin", false, "Make it an admin, who also maintains the shared list")
	cmd.Flags().String("display", "", "Display name")
	cmd.Flags().String("email", "", "Email address")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		admin, _ := cmd.Flags().GetBool("admin")
		u := domain.User{Name: args[0], DisplayName: getString(cmd, "display"), Email: getString(cmd, "email"), Role: domain.RoleMember}
		if admin {
			u.Role = domain.RoleAdmin
		}
		return withAccounts(cmd, func(a *app.Accounts) error {
			if err := a.CheckNewUser(ctx(cmd), u); err != nil {
				return err
			}
			pw, err := newPassword(cmd, "Password for "+u.Name)
			if err != nil {
				return err
			}
			u, err := a.AddUser(ctx(cmd), u, pw)
			if err != nil {
				return err
			}
			lib, err := a.LibraryName(ctx(cmd), u)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Added %s (%s), with the library %s\n", u.Name, u.Role, lib)
			return nil
		})
	}
	return cmd
}

func newUserRoleCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "role NAME", Short: "Change an account's role, or disable it", Args: cobra.ExactArgs(1),
		Long: `--admin and --member set the role; --disable stops the account signing in
and signs it out, and --enable lets it sign in again.`}
	cmd.Flags().Bool("admin", false, "Make it an admin")
	cmd.Flags().Bool("member", false, "Make it a member")
	cmd.Flags().Bool("disable", false, "Stop it signing in")
	cmd.Flags().Bool("enable", false, "Let it sign in again")
	cmd.MarkFlagsMutuallyExclusive("admin", "member")
	cmd.MarkFlagsMutuallyExclusive("disable", "enable")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		admin, _ := cmd.Flags().GetBool("admin")
		member, _ := cmd.Flags().GetBool("member")
		disable, _ := cmd.Flags().GetBool("disable")
		enable, _ := cmd.Flags().GetBool("enable")
		if !admin && !member && !disable && !enable {
			return errors.New("say what to change: --admin, --member, --disable, or --enable")
		}
		return withAccounts(cmd, func(a *app.Accounts) error {
			u, err := a.User(ctx(cmd), args[0])
			if err != nil {
				return err
			}
			role, disabled := u.Role, u.Disabled
			switch {
			case admin:
				role = domain.RoleAdmin
			case member:
				role = domain.RoleMember
			}
			switch {
			case disable:
				disabled = true
			case enable:
				disabled = false
			}
			if u, err = a.SetRole(ctx(cmd), u.ID, role, disabled); err != nil {
				return err
			}
			status := "active"
			if u.Disabled {
				status = "disabled"
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "%s is %s, %s\n", u.Name, u.Role, status)
			return nil
		})
	}
	return cmd
}

func newUserPasswordCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "passwd NAME", Short: "Set an account's password, signing it out everywhere", Args: cobra.ExactArgs(1),
		Long: `Sets a new password for an account, such as for someone who forgot theirs
(there's no reset by email), and ends its sessions.`}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return withAccounts(cmd, func(a *app.Accounts) error {
			u, err := a.User(ctx(cmd), args[0])
			if err != nil {
				return err
			}
			pw, err := newPassword(cmd, "New password for "+u.Name)
			if err != nil {
				return err
			}
			if _, err := a.SetPassword(ctx(cmd), u.ID, pw); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Set %s's password; they're signed out everywhere\n", u.Name)
			return nil
		})
	}
	return cmd
}

func newUserRemoveCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "remove NAME", Short: "Remove an account and its library", Args: cobra.ExactArgs(1),
		Long: `Removes an account and deletes its library, with its channels, radios, and
saved searches. It can't be undone; back the library up first if it's
wanted. --disable on rflib user role keeps everything instead.`}
	cmd.Flags().Bool("yes", false, "Confirm deleting the account's library")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := requireYes(cmd, "account and library"); err != nil {
			return err
		}
		return withAccounts(cmd, func(a *app.Accounts) error {
			u, err := a.RemoveUser(ctx(cmd), args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Removed %s and their library\n", u.Name)
			return nil
		})
	}
	return cmd
}

func newUserInviteCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "invite", Short: "Make an invitation code for one sign-up", Args: cobra.NoArgs,
		Long: `Prints a code that lets one person register while the server's sign-up is
by invitation (rflib serve --accounts --signup invite). It's shown only
now, and lasts two weeks.`}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return withAccounts(cmd, func(a *app.Accounts) error {
			code, expires, err := a.Invite(ctx(cmd))
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), code)
			fmt.Fprintf(cmd.ErrOrStderr(), "Good for one sign-up until %s\n", expires.Local().Format("2006-01-02 15:04"))
			return nil
		})
	}
	return cmd
}

// newPassword reads a new password: twice from a terminal, without echo,
// or one line from standard input otherwise.
func newPassword(cmd *cobra.Command, prompt string) (string, error) {
	in := cmd.InOrStdin()
	if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		read := func(p string) (string, error) {
			fmt.Fprint(cmd.ErrOrStderr(), p+": ")
			b, err := term.ReadPassword(int(f.Fd()))
			fmt.Fprintln(cmd.ErrOrStderr())
			return string(b), err
		}
		pw, err := read(prompt)
		if err != nil {
			return "", err
		}
		again, err := read("Again")
		if err != nil {
			return "", err
		}
		if pw != again {
			return "", errors.New("the passwords don't match")
		}
		return pw, domain.ValidatePassword(pw)
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	pw := strings.TrimRight(line, "\r\n")
	return pw, domain.ValidatePassword(pw)
}
