package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	lstore "github.com/mrchypark/yeoul/internal/storage/ladybug"
	"github.com/mrchypark/yeoul/pkg/yeoul"
)

type usageError struct {
	message string
}

func (e *usageError) Error() string {
	return e.message
}

type cli struct {
	stdout  io.Writer
	stderr  io.Writer
	confirm bool
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	app := cli{
		stdout: stdout,
		stderr: stderr,
	}
	return app.run(ctx, args)
}

func (c cli) run(ctx context.Context, args []string) error {
	args, c.confirm = stripGlobalConfirm(args)
	if len(args) == 0 {
		c.printRootUsage()
		return nil
	}

	switch args[0] {
	case "help", "-h", "--help":
		c.printRootUsage()
		return nil
	case "init":
		return c.runInit(ctx, args[1:])
	case "migrate":
		return c.runMigrate(ctx, args[1:])
	case "ingest":
		return c.runIngest(ctx, args[1:])
	case "get":
		return c.runGet(ctx, args[1:])
	case "search":
		return c.runSearch(ctx, args[1:])
	case "timeline":
		return c.runTimeline(ctx, args[1:])
	case "provenance":
		return c.runProvenance(ctx, args[1:])
	case "inspect":
		return c.runInspect(ctx, args[1:])
	case "neighborhood":
		return c.runNeighborhood(ctx, args[1:])
	case "fact":
		return c.runFact(ctx, args[1:])
	case "entity":
		return c.runEntity(ctx, args[1:])
	case "policy":
		return c.runPolicy(ctx, args[1:])
	case "admin":
		return c.runAdmin(ctx, args[1:])
	case "bench":
		return c.runBench(ctx, args[1:])
	default:
		return &usageError{message: fmt.Sprintf("unknown command %q\n\n%s", args[0], rootUsage())}
	}
}

func rootUsage() string {
	return strings.TrimSpace(`
Usage:
  yeoul init --db PATH [--force] [--json]
  yeoul migrate --db PATH [--json]
  yeoul ingest episode --db PATH --kind KIND (--content TEXT | --content-file FILE) [flags]
  yeoul ingest file --db PATH --kind KIND --file FILE [flags]
  yeoul ingest json --db PATH --file FILE [--json]
  yeoul ingest batch --db PATH --file FILE [--json]
  yeoul get --db PATH --kind episode|entity|fact|source --id ID [--json]
  yeoul search --db PATH --query TEXT [--type fact,episode,entity] [--limit N] [--include-related] [--json]
  yeoul timeline --db PATH [--entity ID | --fact ID | --episode ID | --source ID] [--event-type TYPES] [--as-of RFC3339] [--from RFC3339] [--to RFC3339] [--descending] [--limit N] [--json]
  yeoul provenance --db PATH (--kind KIND --id ID | --entity ID | --fact ID | --episode ID) [--as-of RFC3339] [--max-depth N] [--json]
  yeoul inspect schema --db PATH [--json]
  yeoul inspect counts --db PATH [--json]
  yeoul neighborhood --db PATH (--entity ID | --fact ID | --episode ID) [--hops N] [--max-nodes N] [--json]
  yeoul entity get --db PATH --id ID [--json]
  yeoul entity merge-preview --db PATH [--json]
  yeoul entity merge --db PATH --target ID --source IDS --reason TEXT [--json] [--confirm]
  yeoul fact get --db PATH --id ID [--json]
  yeoul fact lookup --db PATH [--subject-id IDS] [--predicate PREDS] [--object-id IDS] [--object-text TEXT] [--as-of RFC3339] [--include-inactive] [--limit N] [--json]
  yeoul fact assert --db PATH --predicate PRED --subject-id ID [--object-id ID] [--value-text TEXT] --supporting-episodes IDS [--json]
  yeoul fact supersede --db PATH --id ID --predicate PRED --subject-id ID [--object-id ID] [--value-text TEXT] --supporting-episodes IDS --reason TEXT [--json]
  yeoul fact retract --db PATH --id ID --reason TEXT [--json]
  yeoul policy validate --path PATH [--json]
  yeoul policy show --path PATH [--json]
  yeoul policy list-recipes --path PATH [--json]
  yeoul admin checkpoint --db PATH [--json]
  yeoul admin compact --db PATH [--apply] [--json] [--confirm]
  yeoul admin export --db PATH --out FILE [--json]
  yeoul admin import --db PATH --in FILE [--json]
  yeoul bench ingest --db PATH --episodes N [--facts-per-episode N] [--json]
  yeoul bench query --db PATH --query TEXT [--entity ID] [--fact ID] [--iterations N] [--json]
  yeoul bench lifecycle --db PATH --iterations N [--json]

Commands:
  init            Create or validate a Yeoul database file.
  migrate         Run pending schema migrations.
  ingest episode  Ingest a single episode.
  ingest file     Ingest one episode from a file.
  ingest json     Bulk-ingest episodes, entities, and facts from JSON.
  ingest batch    Alias for bulk JSON ingest.
  get             Fetch one record by kind and ID.
  search          Run a simple text search over records.
  timeline        Inspect time-ordered episode and fact events.
  provenance      Explain supporting provenance for a record.
  inspect         Inspect schema, counts, or records.
  neighborhood    Expand around an anchor record.
  entity          Inspect or manage entity merge markers.
  fact            Manage fact lifecycle operations.
  policy          Validate and inspect policy packs.
  admin           Export and import Yeoul data.
  bench           Run local benchmark commands.
`)
}

func (c cli) printRootUsage() {
	fmt.Fprintln(c.stdout, rootUsage())
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func parseFlagSet(fs *flag.FlagSet, usage string, args []string, stdout io.Writer) (bool, error) {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(stdout, usage)
			return true, nil
		}
		return false, &usageError{message: usage}
	}
	return false, nil
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}

	var usageErr *usageError
	if errors.As(err, &usageErr) {
		return 2
	}

	var apiErr *yeoul.Error
	if errors.As(err, &apiErr) {
		switch apiErr.Code {
		case yeoul.ErrConfigInvalid, yeoul.ErrInputInvalid, yeoul.ErrLifecycleInvalid, yeoul.ErrNotSupported:
			return 2
		case yeoul.ErrEntityNotFound, yeoul.ErrFactNotFound, yeoul.ErrSourceNotFound:
			return 3
		case yeoul.ErrQueryFailed:
			return 4
		default:
			return 1
		}
	}

	return 1
}

func printCommandError(w io.Writer, err error) {
	if err == nil {
		return
	}

	var usageErr *usageError
	if errors.As(err, &usageErr) {
		fmt.Fprintln(w, usageErr.message)
		return
	}

	var apiErr *yeoul.Error
	if errors.As(err, &apiErr) {
		fmt.Fprintf(w, "error: %s\n", apiErr.Error())
		if len(apiErr.Details) > 0 {
			data, marshalErr := json.MarshalIndent(apiErr.Details, "", "  ")
			if marshalErr == nil {
				fmt.Fprintf(w, "details: %s\n", string(data))
			}
		}
		return
	}

	fmt.Fprintf(w, "error: %v\n", err)
}

func openReadEngine(ctx context.Context, dbPath string) (yeoul.Engine, error) {
	return yeoul.Open(ctx, yeoul.Config{
		Driver:       yeoul.StorageDriverLadybug,
		DatabasePath: dbPath,
		ReadOnly:     true,
	})
}

func openWriteEngine(ctx context.Context, dbPath string) (yeoul.Engine, error) {
	return yeoul.Open(ctx, yeoul.Config{
		Driver:       yeoul.StorageDriverLadybug,
		DatabasePath: dbPath,
	})
}

func closeEngine(ctx context.Context, eng yeoul.Engine) error {
	if eng == nil {
		return nil
	}
	return eng.Close(ctx)
}

func openRawStore(dbPath string, readOnly bool) (*lstore.Store, error) {
	return lstore.Open(dbPath, readOnly)
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func requireDB(dbPath, usage string) error {
	if strings.TrimSpace(dbPath) == "" {
		return &usageError{message: usage}
	}
	return nil
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func shorten(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 || len(text) <= limit {
		return text
	}
	if limit <= 3 {
		return text[:limit]
	}
	return text[:limit-3] + "..."
}

func readFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func stripGlobalConfirm(args []string) ([]string, bool) {
	out := make([]string, 0, len(args))
	confirm := false
	for _, arg := range args {
		if arg == "--confirm" {
			confirm = true
			continue
		}
		out = append(out, arg)
	}
	return out, confirm
}
