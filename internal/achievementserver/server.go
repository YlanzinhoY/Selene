package achievementserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"runtime"
	"time"

	backend "github.com/selene-linux/selene/internal/achievement-backend"
	"github.com/selene-linux/selene/internal/achievement-backend/api"
	"github.com/selene-linux/selene/internal/achievement-backend/bootstrap"
	"github.com/selene-linux/selene/internal/achievement-backend/notifier"
)

const (
	DefaultHTTPAddress = "127.0.0.1:48212"
	InternalModeEnv    = "SELENE_INTERNAL_MODE"
	HTTPAddressEnv     = "SELENE_ACHIEVEMENTS_HTTP"
	internalModeValue  = "achievements-server"
)

const shutdownTimeout = 5 * time.Second

// InternalModeRequested reports whether this process was spawned by Selene as
// its private achievement backend. This mode is deliberately not a public CLI
// subcommand, preserving Selene's TUI-only interface.
func InternalModeRequested() bool {
	return os.Getenv(InternalModeEnv) == internalModeValue
}

// InternalEnvironment returns the environment entries that select the private
// server mode in a child Selene process.
func InternalEnvironment(address string) []string {
	return []string{
		InternalModeEnv + "=" + internalModeValue,
		HTTPAddressEnv + "=" + address,
	}
}

// AddressFromEnvironment resolves the loopback HTTP address selected by the
// supervisor.
func AddressFromEnvironment() string {
	if address := os.Getenv(HTTPAddressEnv); address != "" {
		return address
	}
	return DefaultHTTPAddress
}

// Run starts the long-lived local achievement backend and blocks until its
// context is cancelled or the HTTP server fails.
func Run(ctx context.Context, address string) error {
	if runtime.GOOS != "linux" {
		return errors.New("the achievement backend is supported only on Linux")
	}
	if runningAsRoot() {
		return errors.New("the achievement backend must not run as root")
	}
	if err := validateLoopbackAddress(address); err != nil {
		return err
	}
	if err := os.MkdirAll(backend.LogDir, 0o700); err != nil {
		return fmt.Errorf("create achievement log directory: %w", err)
	}

	bootstrap.ConfigureLogger()
	services := bootstrap.NewServices()
	services.Notifier.SetDeliveryMode(notifier.DeliveryDecky)
	if err := bootstrap.StartSharedServices(ctx, services, bootstrap.StartOptions{StartWatcher: false}); err != nil {
		return err
	}

	listener, err := net.Listen("tcp", address)
	if err != nil {
		_ = services.Notifier.ServiceShutdown()
		return fmt.Errorf("listen for achievement API on %s: %w", address, err)
	}

	server := &http.Server{
		Handler:           api.NewRouter(services.Config, services.Steam, services.Watcher, services.Notifier).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	slog.Info("Achievement backend ready", "http", listener.Addr().String())
	if err := services.Watcher.Startup(ctx); err != nil {
		// Health is independent from scanning. Keep the API available so clients
		// can fix settings and the supervisor can keep the session alive.
		slog.Warn("Achievement watcher did not start", "error", err)
	}

	var runErr error
	select {
	case <-ctx.Done():
	case runErr = <-serveErr:
		if runErr != nil {
			runErr = fmt.Errorf("serve achievement API: %w", runErr)
		}
	}

	services.Watcher.Stop()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	shutdownErr := server.Shutdown(shutdownCtx)
	notifierErr := services.Notifier.ServiceShutdown()
	if runErr == nil && ctx.Err() != nil && !errors.Is(ctx.Err(), context.Canceled) {
		runErr = ctx.Err()
	}
	return errors.Join(runErr, shutdownErr, notifierErr)
}

func validateLoopbackAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid achievement HTTP address %q: %w", address, err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("achievement HTTP address must use a loopback IP: %s", address)
	}
	return nil
}
