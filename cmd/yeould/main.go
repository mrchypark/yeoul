package main

import (
	"bytes"
	"context"
	"encoding/json"
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
	databasePath          string
	listen                string
	rhizaBackupConfigPath string
	rhizaBackup           *yeoul.RhizaBackupConfig
	create                bool
	restoreIfMissing      bool
	restoreOnly           bool
	checkConfig           bool
	token                 string
	loopback              bool
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
	if opts.rhizaBackupConfigPath != "" {
		backup, err := readRhizaBackupConfig(opts.rhizaBackupConfigPath)
		if err != nil {
			return opts, err
		}
		opts.rhizaBackup = backup
	}
	if opts.restoreIfMissing && opts.rhizaBackup == nil {
		return opts, errors.New("--restore-if-missing requires --rhiza-backup-config")
	}
	if opts.restoreIfMissing && opts.create {
		return opts, errors.New("--restore-if-missing and --create are mutually exclusive")
	}
	if opts.restoreOnly {
		if opts.checkConfig {
			return opts, errors.New("--restore-only and --check-config are mutually exclusive")
		}
		if !opts.restoreIfMissing {
			return opts, errors.New("--restore-only requires --restore-if-missing")
		}
		if opts.create {
			return opts, errors.New("--restore-only and --create are mutually exclusive")
		}
	}
	if opts.checkConfig {
		return opts, nil
	}
	if strings.TrimSpace(opts.databasePath) == "" {
		return opts, errors.New("--db is required")
	}
	if opts.restoreOnly {
		return opts, nil
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
	flags.StringVar(&opts.rhizaBackupConfigPath, "rhiza-backup-config", "", "JSON file containing Rhiza object-store backup settings")
	flags.BoolVar(&opts.restoreIfMissing, "restore-if-missing", false, "restore a missing Rhiza database from the configured remote archive")
	flags.BoolVar(&opts.restoreOnly, "restore-only", false, "restore the database and exit before starting the HTTP server")
	flags.BoolVar(&opts.checkConfig, "check-config", false, "validate flag combinations and Rhiza config JSON, then exit without opening storage")
	flags.Usage = func() {
		fmt.Fprintln(output, "Usage: yeould --db PATH [--listen HOST:PORT] [--create | --restore-if-missing] [--rhiza-backup-config PATH]")
		fmt.Fprintln(output, "       yeould --db PATH --rhiza-backup-config PATH --restore-if-missing --restore-only")
		fmt.Fprintln(output, "       yeould --check-config [--rhiza-backup-config PATH] [--restore-if-missing]")
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
	if opts.checkConfig {
		fmt.Fprintln(os.Stdout, "configuration syntax and options valid")
		return nil
	}
	if opts.restoreOnly {
		engine, err := openDaemonEngine(ctx, opts)
		if err != nil {
			return fmt.Errorf("restore database: %w", err)
		}
		if err := engine.Close(context.Background()); err != nil {
			return fmt.Errorf("close restored database: %w", err)
		}
		return nil
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
	return yeoul.Open(ctx, daemonConfig(opts))
}

func daemonConfig(opts options) yeoul.Config {
	return yeoul.Config{
		Driver:           daemonStorageDriver(opts),
		DatabasePath:     opts.databasePath,
		CreateIfMissing:  opts.create,
		RhizaBackup:      opts.rhizaBackup,
		RestoreIfMissing: opts.restoreIfMissing,
	}
}

func daemonStorageDriver(opts options) yeoul.StorageDriver {
	if opts.rhizaBackup != nil {
		return yeoul.StorageDriverRhiza
	}
	return ""
}

func readRhizaBackupConfig(path string) (*yeoul.RhizaBackupConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open Rhiza backup config: %w", err)
	}
	defer file.Close()

	const maxConfigBytes = 1 << 20
	data, err := io.ReadAll(io.LimitReader(file, maxConfigBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read Rhiza backup config: %w", err)
	}
	if len(data) > maxConfigBytes {
		return nil, errors.New("Rhiza backup config exceeds 1 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var fields map[string]json.RawMessage
	if err := decoder.Decode(&fields); err != nil {
		return nil, fmt.Errorf("decode Rhiza backup config: %w", err)
	}
	if fields == nil {
		return nil, errors.New("Rhiza backup config must be a JSON object")
	}
	for name := range fields {
		switch name {
		case "Provider", "Endpoint", "Bucket", "Prefix", "Region", "Insecure", "AccessKey", "SecretKey", "SessionToken", "GCSServiceAccount", "AzureStorageAccount", "AzureStorageAccountKey", "AzureConnectionString":
		default:
			return nil, fmt.Errorf("Rhiza backup config contains unknown or non-canonical field %q", name)
		}
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("Rhiza backup config must contain exactly one JSON value")
		}
		return nil, fmt.Errorf("decode Rhiza backup config trailer: %w", err)
	}
	dataDecoder := json.NewDecoder(bytes.NewReader(data))
	dataDecoder.DisallowUnknownFields()
	var backup yeoul.RhizaBackupConfig
	if err := dataDecoder.Decode(&backup); err != nil {
		return nil, fmt.Errorf("decode Rhiza backup config: %w", err)
	}
	return &backup, nil
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
