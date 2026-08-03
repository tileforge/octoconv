// Command manage-clients is the operator CLI for provisioning and rotating
// client API keys (create a client, add a secondary key for zero-downtime
// rotation, revoke a key slot), and — QUOT-01/QUOT-04, D-04 — for setting
// per-client consumption limits (concurrent jobs per engine, daily bytes,
// daily job count) and the per-client enforcement_mode switch. Limits are
// deliberately set here rather than through a new REST endpoint: this CLI
// already owns operator-side client mutation.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/apaderin/octoconv/internal/auth"
	"github.com/apaderin/octoconv/internal/clients"
	"github.com/apaderin/octoconv/internal/db"
	"github.com/google/uuid"
)

func main() {
	// Read the pepper once, fail fast if unset (same style as db.Connect's
	// DATABASE_URL check).
	salt := []byte(os.Getenv("API_KEY_SALT"))
	if len(salt) == 0 {
		log.Fatal("API_KEY_SALT must be set")
	}

	if len(os.Args) < 2 {
		usage()
	}

	ctx := context.Background()
	pool, err := db.Connect(ctx)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	repo := clients.NewRepo(pool)

	switch os.Args[1] {
	case "create":
		if len(os.Args) != 3 {
			usage()
		}
		name := os.Args[2]

		raw, err := auth.GenerateKey()
		if err != nil {
			log.Fatalf("generate key: %v", err)
		}
		hash := auth.HashKey(salt, raw)

		id, err := repo.Create(ctx, name, hash)
		if err != nil {
			log.Fatalf("create client: %v", err)
		}

		// This is the only place the raw key ever exists in cleartext
		// (D-03/D-07): print it exactly once via fmt.Println, never
		// log.*, so it never gets a log prefix or hits a log aggregator.
		fmt.Println("client id:", id)
		fmt.Println("api key (save now, shown once):", raw)

	case "add-key":
		if len(os.Args) != 3 {
			usage()
		}
		id, err := uuid.Parse(os.Args[2])
		if err != nil {
			log.Fatalf("invalid client id: %v", err)
		}

		raw, err := auth.GenerateKey()
		if err != nil {
			log.Fatalf("generate key: %v", err)
		}
		hash := auth.HashKey(salt, raw)

		if err := repo.AddSecondaryKey(ctx, id, hash); err != nil {
			log.Fatalf("add secondary key: %v", err)
		}

		fmt.Println("api key (save now, shown once):", raw)

	case "revoke":
		if len(os.Args) != 4 {
			usage()
		}
		id, err := uuid.Parse(os.Args[2])
		if err != nil {
			log.Fatalf("invalid client id: %v", err)
		}
		slot := os.Args[3]

		if err := repo.RevokeKey(ctx, id, slot); err != nil {
			log.Fatalf("revoke key: %v", err)
		}

		log.Println("revoked", slot, "key for client", id)

	case "set-limit":
		// manage-clients set-limit <client-id> <engine> <max-concurrent|-> <max-daily-bytes|-> <max-daily-jobs|->
		// "-" is the ONLY token that means NULL/unlimited (D-02) -- it is
		// never replaced with a large sentinel number. Any other token must
		// parse as a non-negative integer or the command aborts before
		// touching the DB (T-52-01). 0 is a legal, meaningful value for
		// max-concurrent (the engine is denied for this client) and is NOT
		// the same as "-".
		if len(os.Args) != 7 {
			usage()
		}
		id, err := uuid.Parse(os.Args[2])
		if err != nil {
			log.Fatalf("invalid client id: %v", err)
		}
		engine := os.Args[3]

		maxConcurrent, err := parseOptionalInt(os.Args[4])
		if err != nil {
			log.Fatalf("invalid max-concurrent: %v", err)
		}
		maxDailyBytes, err := parseOptionalInt64(os.Args[5])
		if err != nil {
			log.Fatalf("invalid max-daily-bytes: %v", err)
		}
		maxDailyJobs, err := parseOptionalInt64(os.Args[6])
		if err != nil {
			log.Fatalf("invalid max-daily-jobs: %v", err)
		}

		if err := repo.SetLimit(ctx, id, clients.Limit{
			Engine:        engine,
			MaxConcurrent: maxConcurrent,
			MaxDailyBytes: maxDailyBytes,
			MaxDailyJobs:  maxDailyJobs,
		}); err != nil {
			log.Fatalf("set limit: %v", err)
		}

		log.Println("set limit for client", id, "engine", engine)

	case "clear-limit":
		if len(os.Args) != 4 {
			usage()
		}
		id, err := uuid.Parse(os.Args[2])
		if err != nil {
			log.Fatalf("invalid client id: %v", err)
		}
		engine := os.Args[3]

		if err := repo.ClearLimit(ctx, id, engine); err != nil {
			log.Fatalf("clear limit: %v", err)
		}

		log.Println("cleared limit for client", id, "engine", engine)

	case "show-limits":
		if len(os.Args) != 3 {
			usage()
		}
		id, err := uuid.Parse(os.Args[2])
		if err != nil {
			log.Fatalf("invalid client id: %v", err)
		}

		limits, err := repo.ListLimits(ctx, id)
		if err != nil {
			log.Fatalf("list limits: %v", err)
		}
		if len(limits) == 0 {
			fmt.Println("no limits set for client", id, "(unlimited on every engine)")
		}
		for _, l := range limits {
			// A nil pointer prints as the literal "unlimited"; a non-nil
			// pointer prints its number -- an operator must never confuse
			// the two (D-02).
			fmt.Println("engine:", l.Engine,
				"max-concurrent:", formatOptionalInt(l.MaxConcurrent),
				"max-daily-bytes:", formatOptionalInt64(l.MaxDailyBytes),
				"max-daily-jobs:", formatOptionalInt64(l.MaxDailyJobs))
		}

	case "set-mode":
		if len(os.Args) != 4 {
			usage()
		}
		id, err := uuid.Parse(os.Args[2])
		if err != nil {
			log.Fatalf("invalid client id: %v", err)
		}
		mode := os.Args[3]

		if err := repo.SetEnforcementMode(ctx, id, mode); err != nil {
			log.Fatalf("set enforcement mode: %v", err)
		}

		log.Println("set enforcement mode", mode, "for client", id)

	default:
		usage()
	}
}

func usage() {
	log.Fatalf("usage: manage-clients <create <name>|add-key <client-id>|revoke <client-id> <primary|secondary>|" +
		"set-limit <client-id> <engine> <max-concurrent|-> <max-daily-bytes|-> <max-daily-jobs|->|" +
		"clear-limit <client-id> <engine>|show-limits <client-id>|set-mode <client-id> <observe|enforce>>")
}

// parseOptionalInt parses a set-limit numeric argument: the literal "-"
// means NULL/unlimited (nil), anything else must be a non-negative int.
func parseOptionalInt(s string) (*int, error) {
	if s == "-" {
		return nil, nil
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return nil, fmt.Errorf("%q is not an integer or \"-\": %w", s, err)
	}
	if v < 0 {
		return nil, fmt.Errorf("%q must not be negative", s)
	}
	return &v, nil
}

// parseOptionalInt64 is parseOptionalInt's int64 counterpart, used for the
// byte/job-count limits.
func parseOptionalInt64(s string) (*int64, error) {
	if s == "-" {
		return nil, nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%q is not an integer or \"-\": %w", s, err)
	}
	if v < 0 {
		return nil, fmt.Errorf("%q must not be negative", s)
	}
	return &v, nil
}

func formatOptionalInt(v *int) string {
	if v == nil {
		return "unlimited"
	}
	return strconv.Itoa(*v)
}

func formatOptionalInt64(v *int64) string {
	if v == nil {
		return "unlimited"
	}
	return strconv.FormatInt(*v, 10)
}
