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
	"github.com/ryan/rflib/internal/store"
	"github.com/ryan/rflib/web"
	"github.com/spf13/cobra"
)

func newServeCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "serve", Short: "Serve the web interface and the JSON HTTP API", Args: cobra.NoArgs,
		Long: `Serve the web interface on --addr, and the JSON HTTP API under /api/v1.
GET /api/v1/openapi.yaml describes the API.

Requests need "Authorization: Bearer TOKEN" when $RFLIB_API_TOKEN is set.
Serving on anything but this machine (a loopback address) requires it, since
anyone who can reach the port could otherwise change the library.`}
	cmd.Flags().String("addr", "127.0.0.1:8080", "Address to listen on")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		addr := getString(cmd, "addr")
		token := os.Getenv("RFLIB_API_TOKEN")
		if token == "" && !loopback(addr) {
			return fmt.Errorf("serving on %s needs a token: set RFLIB_API_TOKEN, or listen on 127.0.0.1", addr)
		}
		return withDB(cmd, func(d store.Store) error {
			logger := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), nil))
			mux := http.NewServeMux()
			mux.Handle(api.Prefix+"/", api.New(d, api.Options{Token: token, Logger: logger}))
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
			fmt.Fprintf(cmd.ErrOrStderr(), "Serving rflib on http://%s/ (the API is under %s)\n", ln.Addr(), api.Prefix)
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
