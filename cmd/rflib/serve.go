package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ryan/rflib/internal/api"
	"github.com/ryan/rflib/internal/app"
	"github.com/ryan/rflib/internal/buildinfo"
	"github.com/ryan/rflib/internal/store"
	"github.com/ryan/rflib/web"
	"github.com/spf13/cobra"
)

func newServeCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "serve", Short: "Serve the web interface and the JSON HTTP API", Args: cobra.NoArgs,
		Long: `Serve the web interface on --addr, and the JSON HTTP API under /api/v1.
GET /api/v1/openapi.yaml describes the API.

Without accounts, it serves your library to whoever has the token: requests
need "Authorization: Bearer TOKEN" when $RFLIB_API_TOKEN is set. Serving on
anything but this machine (a loopback address) requires it, since anyone who
can reach the port could otherwise change the library.

With --accounts, people sign in, each with a library of their own; members
read the shared list, and admins also change it. Make the first admin with
rflib user add NAME --admin; it takes the library you have now. --signup
says who may register: open (anyone), invite (with a code from rflib user
invite), or closed (only accounts from rflib user add).

Sign-in sends passwords, and a session cookie, to the server, so off this
machine it needs HTTPS. rflib doesn't serve HTTPS itself: put it behind a
proxy that does (such as Traefik or Caddy) and pass --behind-tls. Where HTTPS
isn't possible, such as on a mesh network, --plain-http serves sign-in over
plain HTTP anyway, where anyone on the path can read passwords and take
sessions.`}
	cmd.Flags().String("addr", "127.0.0.1:8080", "Address to listen on")
	cmd.Flags().Bool("accounts", false, "Have people sign in, each with their own library")
	cmd.Flags().String("signup", "closed", "With --accounts, who may register: open, invite, or closed")
	cmd.Flags().Bool("behind-tls", false, "With --accounts, rflib is behind a proxy serving HTTPS")
	cmd.Flags().Bool("plain-http", false, "With --accounts, allow sign-in over plain HTTP, unencrypted")
	cmd.MarkFlagsMutuallyExclusive("behind-tls", "plain-http")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		addr := getString(cmd, "addr")
		useAccounts, _ := cmd.Flags().GetBool("accounts")
		behindTLS, _ := cmd.Flags().GetBool("behind-tls")
		plainHTTP, _ := cmd.Flags().GetBool("plain-http")
		token := os.Getenv("RFLIB_API_TOKEN")
		var accounts *api.AccountOptions
		if useAccounts {
			signup, err := app.ParseSignup(getString(cmd, "signup"))
			if err != nil {
				return err
			}
			if !loopback(addr) && !behindTLS && !plainHTTP {
				return fmt.Errorf("serving accounts on %s needs HTTPS: put rflib behind a proxy that serves it and pass --behind-tls, or pass --plain-http to send passwords unencrypted", addr)
			}
			accounts = &api.AccountOptions{Signup: signup, Secure: behindTLS, TrustProxy: behindTLS}
		} else {
			for _, f := range []string{"signup", "behind-tls", "plain-http"} {
				if cmd.Flags().Changed(f) {
					return fmt.Errorf("--%s is for --accounts", f)
				}
			}
			if token == "" && !loopback(addr) {
				return fmt.Errorf("serving on %s needs a token: set RFLIB_API_TOKEN, or listen on 127.0.0.1", addr)
			}
		}
		return withDB(cmd, func(d store.Store) error {
			logger := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), nil))
			if accounts != nil {
				ok, err := app.NewAccounts(d).HasAdmin(ctx(cmd))
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("this database has no admin; make one first with rflib user add NAME --admin")
				}
				if token != "" {
					fmt.Fprintln(cmd.ErrOrStderr(), "With --accounts, $RFLIB_API_TOKEN isn't used: people sign in instead.")
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "Accounts are on; sign-up is %s.\n", accounts.Signup)
				if plainHTTP {
					fmt.Fprintln(cmd.ErrOrStderr(), "Warning: sign-in is over plain HTTP; passwords and sessions cross the network unencrypted.")
				}
			}
			mux := http.NewServeMux()
			mux.Handle(api.Prefix+"/", api.New(d, api.Options{Token: token, Logger: logger, Accounts: accounts}))
			mux.Handle("/", web.Handler())
			srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
			ln, err := net.Listen("tcp", addr)
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			go func() {
				<-ctx.Done()
				shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				_ = srv.Shutdown(shutdown)
			}()
			fmt.Fprintf(cmd.ErrOrStderr(), "Serving rflib %s on http://%s/ (the API is under %s)\n", buildinfo.Get(), ln.Addr(), api.Prefix)
			if !web.Built() {
				fmt.Fprintln(cmd.ErrOrStderr(), "This build has no web interface; run make web, then build rflib again.")
			}
			if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		})
	}
	return cmd
}

// loopback reports whether addr listens only on this machine.
func loopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
