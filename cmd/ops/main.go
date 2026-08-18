// Command termduty-ops is the operations command line. It shares the same store
// implementation as the HTTP service and exposes init, import, export, verify,
// rebuild-index and status subcommands against the on-disk data directory.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/google/uuid"

	"termduty/internal/config"
	"termduty/internal/crosscut"
	"termduty/internal/domain"
	"termduty/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	sub := os.Args[1]
	args := os.Args[2:]
	var err error
	switch sub {
	case "init":
		err = runInit(args)
	case "import":
		err = runImport(args)
	case "export":
		err = runExport(args)
	case "verify":
		err = runVerify(args)
	case "rebuild-index":
		err = runRebuild(args)
	case "status":
		err = runStatus(args)
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", sub)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `termduty-ops <subcommand> [flags]

Subcommands:
  init           create the data directory and apply migrations
  import         import readings from a JSONL file into shards
  export         export readings to a JSONL file
  verify         verify shard checksums against the manifest
  rebuild-index  rebuild the reading index from on-disk shards
  status         print storage diagnostics

Common flags: -config <path>, -data <dir>
`)
}

type commonFlags struct {
	configPath string
	dataDir    string
}

func (cf *commonFlags) bind(fs *flag.FlagSet) {
	fs.StringVar(&cf.configPath, "config", "config.yaml", "config file path")
	fs.StringVar(&cf.dataDir, "data", "", "data directory override")
}

// resolveStore loads configuration, applies the data-directory override, opens
// and migrates the store, returning a background context for the command.
func resolveStore(cf *commonFlags) (config.Config, *store.Store, context.Context, error) {
	cfg, err := config.Load(cf.configPath)
	if err != nil {
		return config.Config{}, nil, nil, err
	}
	if cf.dataDir != "" {
		cfg.DataDir = cf.dataDir
		cfg.DBPath = cf.dataDir + "/termduty.db"
		cfg.ShardDir = cf.dataDir + "/shards"
	}
	if err := cfg.Normalize(); err != nil {
		return config.Config{}, nil, nil, err
	}
	log := crosscut.NewLogger(cfg.LogLevel)
	ctx := context.Background()
	st, err := store.New(ctx, cfg.DBPath, cfg.ShardDir, domain.RealClock{}, log)
	return cfg, st, ctx, err
}

// setup parses the common flags (plus an optional extra-flag binder), resolves
// the store and returns a cleanup that closes it, so every subcommand shares
// one validated entry path.
func setup(name string, args []string, bind func(*flag.FlagSet)) (config.Config, *store.Store, context.Context, func(), error) {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	var cf commonFlags
	cf.bind(fs)
	if bind != nil {
		bind(fs)
	}
	if err := fs.Parse(args); err != nil {
		return config.Config{}, nil, nil, nil, err
	}
	cfg, st, ctx, err := resolveStore(&cf)
	if err != nil {
		return config.Config{}, nil, nil, nil, err
	}
	return cfg, st, ctx, func() { _ = st.Close() }, nil
}

func runInit(args []string) error {
	cfg, st, ctx, cleanup, err := setup("init", args, nil)
	if err != nil {
		return err
	}
	defer cleanup()
	v, err := st.Version(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("initialized data dir %s, schema version %d\n", cfg.DataDir, v)
	return nil
}

func runImport(args []string) error {
	var file *string
	_, st, ctx, cleanup, err := setup("import", args, func(fs *flag.FlagSet) {
		file = fs.String("file", "-", "input JSONL file (- for stdin)")
	})
	if err != nil {
		return err
	}
	defer cleanup()
	in, closer, err := openRead(*file)
	if err != nil {
		return err
	}
	defer closer()
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1024*1024), 8*1024*1024)
	count := 0
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var rd domain.Reading
		if err := json.Unmarshal(line, &rd); err != nil {
			return fmt.Errorf("line %d: %w", count+1, err)
		}
		if rd.ID == "" {
			rd.ID = domain.ReadingID(uuid.NewString())
		}
		if _, err := st.Readings().Append(ctx, rd); err != nil {
			return fmt.Errorf("append line %d: %w", count+1, err)
		}
		count++
	}
	if err := sc.Err(); err != nil {
		return err
	}
	fmt.Printf("imported %d readings\n", count)
	return nil
}

func runExport(args []string) error {
	var file *string
	_, st, ctx, cleanup, err := setup("export", args, func(fs *flag.FlagSet) {
		file = fs.String("file", "-", "output JSONL file (- for stdout)")
	})
	if err != nil {
		return err
	}
	defer cleanup()
	out, closer, err := openWrite(*file)
	if err != nil {
		return err
	}
	defer closer()
	f := store.ReadingFilter{Page: domain.Page{Size: 1000}}
	count := 0
	err = st.Readings().Export(ctx, f, func(rd domain.Reading) error {
		if e := json.NewEncoder(out).Encode(rd); e != nil {
			return e
		}
		count++
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Printf("exported %d readings\n", count)
	return nil
}

func runVerify(args []string) error {
	var shard *string
	_, st, ctx, cleanup, err := setup("verify", args, func(fs *flag.FlagSet) {
		shard = fs.String("shard", "", "verify a single shard id (default all)")
	})
	if err != nil {
		return err
	}
	defer cleanup()
	if *shard != "" {
		res, err := st.Readings().VerifyShard(ctx, *shard)
		if err != nil {
			return err
		}
		fmt.Printf("shard %s: count=%d ok=%t", res.ShardID, res.Count, res.OK)
		if res.Error != "" {
			fmt.Printf(" err=%s", res.Error)
		}
		fmt.Println()
		return nil
	}
	manifest, err := st.Readings().Manifest(ctx)
	if err != nil {
		return err
	}
	bad := 0
	for _, m := range manifest {
		res, err := st.Readings().VerifyShard(ctx, m.ShardID)
		if err != nil {
			fmt.Printf("shard %s: ERROR %v\n", m.ShardID, err)
			bad++
			continue
		}
		status := "OK"
		if !res.OK {
			status = "MISMATCH"
			bad++
		}
		fmt.Printf("shard %s: count=%d %s\n", res.ShardID, res.Count, status)
	}
	fmt.Printf("verified %d shards, %d bad\n", len(manifest), bad)
	return nil
}

func runRebuild(args []string) error {
	_, st, ctx, cleanup, err := setup("rebuild-index", args, nil)
	if err != nil {
		return err
	}
	defer cleanup()
	res, err := st.Readings().RebuildIndex(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("rebuilt index: %d shards, %d rows", res.Shards, res.Rows)
	if len(res.Skipped) > 0 {
		fmt.Printf(", %d skipped", len(res.Skipped))
	}
	fmt.Println()
	return nil
}

func runStatus(args []string) error {
	cfg, st, ctx, cleanup, err := setup("status", args, nil)
	if err != nil {
		return err
	}
	defer cleanup()
	printStatus(ctx, st, cfg)
	return nil
}

func printStatus(ctx context.Context, st *store.Store, cfg config.Config) {
	v, _ := st.Version(ctx)
	fmt.Printf("data_dir: %s\n", cfg.DataDir)
	fmt.Printf("db_path:  %s\n", cfg.DBPath)
	fmt.Printf("shard_dir: %s\n", cfg.ShardDir)
	fmt.Printf("schema_version: %d\n", v)
	_, cTotal, _ := st.Collectors().List(ctx, store.CollectorFilter{Page: domain.Page{Size: 1}})
	_, rTotal, _ := st.Rules().List(ctx, store.RuleFilter{Page: domain.Page{Size: 1}})
	counts, _ := st.Alerts().CountByState(ctx)
	readings, _ := st.Readings().Total(ctx)
	pending, _ := st.Ingest().PendingCount(ctx)
	leased, _ := st.Ingest().LeasedCount(ctx)
	failures, _, _ := st.Failures().List(ctx, store.FailureFilter{Page: domain.Page{Size: 1}})
	manifest, _ := st.Readings().Manifest(ctx)
	fmt.Printf("collectors: %d  rules: %d\n", cTotal, rTotal)
	fmt.Printf("alerts: open=%d assigned=%d handling=%d resolved=%d closed=%d revoked=%d\n",
		counts[domain.AlertStateOpen], counts[domain.AlertStateAssigned], counts[domain.AlertStateHandling],
		counts[domain.AlertStateResolved], counts[domain.AlertStateClosed], counts[domain.AlertStateRevoked])
	fmt.Printf("readings_indexed: %d  shards: %d\n", readings, len(manifest))
	fmt.Printf("ingest: pending=%d leased=%d  permanent_failures=%d\n", pending, leased, len(failures))
}

func openRead(path string) (io.Reader, func(), error) {
	if path == "-" {
		return os.Stdin, func() {}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	return f, func() { f.Close() }, nil
}

func openWrite(path string) (io.Writer, func(), error) {
	if path == "-" {
		return os.Stdout, func() {}, nil
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, nil, err
	}
	return f, func() { f.Close() }, nil
}
