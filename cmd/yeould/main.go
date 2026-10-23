package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mrchypark/yeoul/pkg/httpapi"
	"github.com/mrchypark/yeoul/pkg/yeoul"
)

type options struct {
	databasePath string
	listen       string
	create       bool
	token        string
	loopback     bool
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage(os.Stdout)
			return
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parseOptions(args []string, getenv func(string) string) (options, error) {
	var opts options
	flags := optionFlagSet(&opts, io.Discard)
	if err := flags.Parse(args); err != nil {
		return opts, err
	}
	if flags.NArg() != 0 {
		return opts, errors.New("unexpected positional arguments")
	}
	if strings.TrimSpace(opts.databasePath) == "" {
		return opts, errors.New("--db is required")
	}
	host, _, err := net.SplitHostPort(opts.listen)
	if err != nil {
		return opts, fmt.Errorf("invalid --listen address: %w", err)
	}
	opts.loopback = host == "localhost"
	if ip := net.ParseIP(host); ip != nil {
		opts.loopback = ip.IsLoopback()
	}
	opts.token = getenv("YEOUL_HTTP_TOKEN")
	if !opts.loopback && opts.token == "" {
		return opts, errors.New("YEOUL_HTTP_TOKEN is required when --listen is not loopback")
	}
	return opts, nil
}

func optionFlagSet(opts *options, output io.Writer) *flag.FlagSet {
	flags := flag.NewFlagSet("yeould", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&opts.databasePath, "db", "", "database path (required)")
	flags.StringVar(&opts.listen, "listen", "127.0.0.1:8080", "HTTP listen address")
	flags.BoolVar(&opts.create, "create", false, "create the database if it does not exist")
	flags.Usage = func() {
		fmt.Fprintln(output, "Usage: yeould --db PATH [--listen HOST:PORT] [--create]")
		fmt.Fprintln(output, "YEOUL_HTTP_TOKEN is required for non-loopback listeners and enables auth on loopback.")
		fmt.Fprintln(output, "Options:")
		flags.PrintDefaults()
	}
	return flags
}

func printUsage(output io.Writer) {
	optionFlagSet(&options{}, output).Usage()
}

func run(ctx context.Context, args []string, getenv func(string) string) (err error) {
	opts, err := parseOptions(args, getenv)
	if err != nil {
		return err
	}
	engine, err := openDaemonEngine(ctx, opts)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	listener, err := net.Listen("tcp", opts.listen)
	if err != nil {
		return errors.Join(fmt.Errorf("listen: %w", err), engine.Close(context.Background()))
	}
	return serve(ctx, opts, engine, listener)
}

func openDaemonEngine(ctx context.Context, opts options) (yeoul.Engine, error) {
	return yeoul.Open(ctx, yeoul.Config{
		Driver:          "",
		DatabasePath:    opts.databasePath,
		CreateIfMissing: opts.create,
	})
}

func serve(ctx context.Context, opts options, engine yeoul.Engine, listener net.Listener) (err error) {
	defer func() {
		if closeErr := listener.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			err = errors.Join(err, closeErr)
		}
		err = errors.Join(err, engine.Close(context.Background()))
	}()
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		return fmt.Errorf("inspect listener address: %w", err)
	}
	server := &http.Server{
		Addr:              opts.listen,
		Handler:           httpapi.NewHandler(engine, httpapi.Config{BearerToken: opts.token, RequireAuth: opts.token != "" || !opts.loopback, LoopbackOnly: opts.loopback, ListenPort: port}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	serveResult := make(chan error, 1)
	go func() { serveResult <- server.Serve(listener) }()

	select {
	case serveErr := <-serveResult:
		if errors.Is(serveErr, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", serveErr)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if shutdownErr := server.Shutdown(shutdownCtx); shutdownErr != nil {
			_ = server.Close()
			return fmt.Errorf("shutdown HTTP server: %w", shutdownErr)
		}
		serveErr := <-serveResult
		if !errors.Is(serveErr, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", serveErr)
		}
		return nil
	}
}
