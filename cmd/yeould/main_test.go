package main

import (
	"context"
	"errors"
	"flag"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mrchypark/yeoul/pkg/yeoul"
)

type closeTrackingEngine struct {
	yeoul.Engine
	closed chan struct{}
}

func (e *closeTrackingEngine) Close(ctx context.Context) error {
	err := e.Engine.Close(ctx)
	close(e.closed)
	return err
}

func TestParseOptions(t *testing.T) {
	getenv := func(string) string { return "" }
	opts, err := parseOptions([]string{"--db", "/tmp/yeoul-test"}, getenv)
	if err != nil {
		t.Fatal(err)
	}
	if opts.listen != "127.0.0.1:8080" || opts.create || !opts.loopback || opts.token != "" {
		t.Fatalf("default options = %+v", opts)
	}
	for _, args := range [][]string{{}, {"--db", "/tmp/db", "--token", "secret"}} {
		if _, err := parseOptions(args, getenv); err == nil {
			t.Fatalf("parseOptions(%v) unexpectedly succeeded", args)
		}
	}
	if _, err := parseOptions([]string{"--db", "/tmp/db", "--listen", "0.0.0.0:8080"}, getenv); err == nil {
		t.Fatal("non-loopback bind succeeded without token")
	}
	opts, err = parseOptions([]string{"--db", "/tmp/db", "--listen", "[::]:8080", "--create"}, func(key string) string {
		if key == "YEOUL_HTTP_TOKEN" {
			return "test-token"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.loopback || !opts.create || opts.token != "test-token" {
		t.Fatalf("remote options = %+v", opts)
	}
}

func TestHelpPrintsDaemonUsageWithoutStarting(t *testing.T) {
	if _, err := parseOptions([]string{"--help"}, func(string) string { return "" }); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("help parse error = %v, want flag.ErrHelp", err)
	}
	var output strings.Builder
	printUsage(&output)
	for _, want := range []string{"Usage: yeould --db PATH", "--listen", "--create", "YEOUL_HTTP_TOKEN"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("help output missing %q: %s", want, output.String())
		}
	}
	if err := run(context.Background(), []string{"--help"}, func(string) string { return "" }); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("run --help error = %v, want flag.ErrHelp", err)
	}
}

func TestServeCancellationClosesEngineAndReleasesListener(t *testing.T) {
	engine, err := yeoul.Open(context.Background(), yeoul.Config{InMemory: true})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = engine.Close(context.Background())
		t.Fatal(err)
	}
	address := listener.Addr().String()
	trackedEngine := &closeTrackingEngine{Engine: engine, closed: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() {
		finished <- serve(ctx, options{listen: address, loopback: true, token: "local-token"}, trackedEngine, listener)
	}()

	transport := &http.Transport{DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: time.Second, Transport: transport}
	deadline := time.Now().Add(5 * time.Second)
	for {
		request, _ := http.NewRequest(http.MethodGet, "http://"+address+"/v1/health", nil)
		request.Header.Set("Authorization", "Bearer local-token")
		response, requestErr := client.Do(request)
		if requestErr == nil {
			_ = response.Body.Close()
			if response.StatusCode != http.StatusOK {
				cancel()
				t.Fatalf("health status = %d", response.StatusCode)
			}
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("server did not become ready: %v", requestErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	unauthorized, err := client.Get("http://" + address + "/v1/health")
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	_ = unauthorized.Body.Close()
	if unauthorized.StatusCode != http.StatusUnauthorized {
		cancel()
		t.Fatalf("configured loopback token was not enforced: status=%d", unauthorized.StatusCode)
	}

	cancel()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("serve returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not stop after context cancellation")
	}
	select {
	case <-trackedEngine.closed:
	default:
		t.Fatal("engine was not closed during shutdown")
	}

	reopened, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("listener was not released: %v", err)
	}
	_ = reopened.Close()
}

func TestDaemonAutoSelectsStorageForExistingAndNewDatabases(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	legacyPath := filepath.Join(root, "existing-lattice")
	legacy, err := yeoul.Open(ctx, yeoul.Config{
		Driver:          yeoul.StorageDriverLattice,
		DatabasePath:    legacyPath,
		CreateIfMissing: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.IngestEpisode(ctx, yeoul.EpisodeInput{ID: "kept", Kind: "note", Content: "existing data"}); err != nil {
		_ = legacy.Close(ctx)
		t.Fatal(err)
	}
	if err := legacy.Close(ctx); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name   string
		path   string
		create bool
	}{
		{name: "existing Lattice database", path: legacyPath},
		{name: "new database", path: filepath.Join(root, "new-database"), create: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine, err := openDaemonEngine(ctx, options{databasePath: tc.path, create: tc.create})
			if err != nil {
				t.Fatalf("daemon automatic storage open: %v", err)
			}
			defer engine.Close(ctx)
			snapshot, err := yeoul.Snapshot(ctx, engine)
			if err != nil {
				t.Fatalf("snapshot after automatic storage open: %v", err)
			}
			if tc.name == "existing Lattice database" && snapshot.Episodes["kept"].Content != "existing data" {
				t.Fatalf("existing Lattice data was not preserved: %+v", snapshot.Episodes)
			}
		})
	}
}
