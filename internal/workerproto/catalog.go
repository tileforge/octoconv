// Package workerproto is the operator-approved worker catalog and the TLS session listener.
package workerproto

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tileforge/octoconv/internal/convert"
)

// ErrCapabilityConflict is returned when another worker already owns the
// operation, source format, and target format being approved.
var ErrCapabilityConflict = errors.New("capability format already approved")

var capabilityIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9]*)+$`)

var legacyEngines = map[string]struct{}{
	"image": {}, "document": {}, "av": {}, "cad": {}, "archive": {}, "probe": {},
	"html": {}, "audio": {}, "ocr": {}, "pandoc": {}, "tectonic": {},
}

var outputRoles = map[string]struct{}{
	"primary": {}, "report": {}, "preview": {}, "thumbnail": {}, "measurements": {},
}

// Repo is the Postgres catalog and session store.
type Repo struct {
	pool *pgxpool.Pool
}

// Match is one approved operation, source format, and target format.
type Match struct {
	WorkerID          uuid.UUID
	CapabilityID      string
	CapabilityVersion int
	ClientOperation   string
	LegacyOperation   string
	LegacyEngine      string
	ParameterSchema   []byte
}

// CompareResult lists worker ids whose installed manifest bytes differ from the approved row.
type CompareResult struct {
	Mismatched []uuid.UUID
}

// NewRepo returns a catalog repository for the given pool.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// Approve is the only catalog insert. It stores the exact manifest bytes.
func (r *Repo) Approve(ctx context.Context, workerID uuid.UUID, fileBytes []byte) error {
	if workerID == uuid.Nil {
		return errors.New("approve manifest: worker id is required")
	}
	parsed, err := parseManifest(fileBytes)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(fileBytes)
	digest := hex.EncodeToString(sum[:])
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO worker_identities (worker_id, manifest_sha256, manifest_bytes)
			VALUES ($1, $2, $3)
			ON CONFLICT (worker_id) DO UPDATE
			SET manifest_sha256 = EXCLUDED.manifest_sha256,
			    manifest_bytes = EXCLUDED.manifest_bytes,
			    approved_at = now()`,
			workerID, digest, fileBytes); err != nil {
			return fmt.Errorf("upsert identity: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			DELETE FROM worker_capability_ports
			WHERE (capability_id, capability_version) IN (
				SELECT id, version FROM worker_capabilities WHERE worker_id = $1
			)`, workerID); err != nil {
			return fmt.Errorf("delete ports: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			DELETE FROM worker_capability_formats
			WHERE (capability_id, capability_version) IN (
				SELECT id, version FROM worker_capabilities WHERE worker_id = $1
			)`, workerID); err != nil {
			return fmt.Errorf("delete formats: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM worker_capabilities WHERE worker_id = $1`, workerID); err != nil {
			return fmt.Errorf("delete capability: %w", err)
		}
		var legacyOp, legacyEngine any
		if parsed.hasLegacy {
			legacyOp = parsed.legacyOp
			legacyEngine = parsed.legacyEngine
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO worker_capabilities (
				id, version, worker_id, client_operation, legacy_operation, legacy_engine, parameter_schema
			) VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)`,
			parsed.id, parsed.version, workerID, parsed.clientOperation, legacyOp, legacyEngine, string(parsed.schema),
		); err != nil {
			return fmt.Errorf("insert capability: %w", err)
		}
		for _, format := range parsed.formats {
			if _, err := tx.Exec(ctx, `
				INSERT INTO worker_capability_formats (
					capability_id, capability_version, source_format, target_format, client_operation
				) VALUES ($1, $2, $3, $4, $5)`,
				parsed.id, parsed.version, format.source, format.target, parsed.clientOperation,
			); err != nil {
				if isFormatConflict(err) {
					return fmt.Errorf("approve format: %w", ErrCapabilityConflict)
				}
				return fmt.Errorf("insert format: %w", err)
			}
		}
		for _, port := range parsed.ports {
			if _, err := tx.Exec(ctx, `
				INSERT INTO worker_capability_ports (
					capability_id, capability_version, direction, role, min_count, max_count, ordinal
				) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
				parsed.id, parsed.version, port.direction, port.role, port.min, port.max, port.ordinal,
			); err != nil {
				return fmt.Errorf("insert port: %w", err)
			}
		}
		return nil
	})
}

// Lookup returns the single approved match for an operation and format pair.
func (r *Repo) Lookup(ctx context.Context, operation, source, target string) (Match, bool, error) {
	source = convert.NormalizeFormat(source)
	target = convert.NormalizeFormat(target)
	var match Match
	var legacyOp, legacyEngine *string
	err := r.pool.QueryRow(ctx, `
		SELECT c.worker_id, c.id, c.version, c.client_operation,
		       c.legacy_operation, c.legacy_engine, c.parameter_schema
		FROM worker_capability_formats f
		JOIN worker_capabilities c
		  ON c.id = f.capability_id AND c.version = f.capability_version
		WHERE f.client_operation = $1 AND f.source_format = $2 AND f.target_format = $3`,
		operation, source, target,
	).Scan(
		&match.WorkerID,
		&match.CapabilityID,
		&match.CapabilityVersion,
		&match.ClientOperation,
		&legacyOp,
		&legacyEngine,
		&match.ParameterSchema,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Match{}, false, nil
	}
	if err != nil {
		return Match{}, false, fmt.Errorf("lookup capability: %w", err)
	}
	if legacyOp != nil {
		match.LegacyOperation = *legacyOp
	}
	if legacyEngine != nil {
		match.LegacyEngine = *legacyEngine
	}
	return match, true, nil
}

// AccountingKey returns the legacy engine string, or the capability id when there is no legacy engine.
func AccountingKey(legacyEngine, capabilityID string) string {
	if legacyEngine != "" {
		return legacyEngine
	}
	return capabilityID
}

// ValidateParameters checks instance against a parameter schema.
func ValidateParameters(schema []byte, instance any) error {
	resolved, err := resolveParameterSchema(schema)
	if err != nil {
		return fmt.Errorf("validate parameters: %w", err)
	}
	if err := resolved.Validate(instance); err != nil {
		return fmt.Errorf("validate parameters: %w", err)
	}
	return nil
}

// CompareInstalled hashes manifest files under dir and does not insert or update catalog rows.
func (r *Repo) CompareInstalled(ctx context.Context, dir string) (CompareResult, error) {
	root := filepath.Clean(dir)
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return CompareResult{}, fmt.Errorf("read manifest dir: %w", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return CompareResult{}, fmt.Errorf("read manifest dir: %w", err)
	}
	result := CompareResult{Mismatched: []uuid.UUID{}}
	for _, entry := range entries {
		name := entry.Name()
		id, err := uuid.Parse(name)
		if err != nil || id.String() != name {
			return CompareResult{}, errors.New("manifest dir: invalid child")
		}
		child := filepath.Join(root, name, "manifest.json")
		cleanChild := filepath.Clean(child)
		rel, err := filepath.Rel(root, cleanChild)
		if err != nil || !filepath.IsLocal(rel) {
			return CompareResult{}, errors.New("manifest dir: path escapes")
		}
		resolved, err := filepath.EvalSymlinks(cleanChild)
		if err != nil {
			return CompareResult{}, fmt.Errorf("read manifest: %w", err)
		}
		resolvedRel, err := filepath.Rel(resolvedRoot, resolved)
		if err != nil || !filepath.IsLocal(resolvedRel) {
			return CompareResult{}, errors.New("manifest dir: path escapes")
		}
		body, err := os.ReadFile(resolved)
		if err != nil {
			return CompareResult{}, fmt.Errorf("read manifest: %w", err)
		}
		sum := sha256.Sum256(body)
		digest := hex.EncodeToString(sum[:])
		var stored string
		err = r.pool.QueryRow(ctx, `
			SELECT manifest_sha256 FROM worker_identities WHERE worker_id = $1`, id).Scan(&stored)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return CompareResult{}, fmt.Errorf("read identity: %w", err)
		}
		if stored != digest {
			result.Mismatched = append(result.Mismatched, id)
		}
	}
	return result, nil
}

type parsedManifest struct {
	id              string
	version         int
	clientOperation string
	hasLegacy       bool
	legacyOp        string
	legacyEngine    string
	schema          json.RawMessage
	formats         []storedFormat
	ports           []storedPort
}

type storedPort struct {
	direction string
	role      string
	min       int
	max       int
	ordinal   int
}

type storedFormat struct {
	source string
	target string
}

type manifestDoc struct {
	Capabilities []capabilityDoc `json:"capabilities"`
	Extensions   json.RawMessage `json:"extensions"`
}

type capabilityDoc struct {
	ID              string          `json:"id"`
	Version         json.Number     `json:"version"`
	Legacy          *legacyDoc      `json:"legacy"`
	ClientOperation string          `json:"client_operation"`
	Inputs          []portDoc       `json:"inputs"`
	Outputs         []portDoc       `json:"outputs"`
	ParameterSchema json.RawMessage `json:"parameter_schema"`
	Formats         []formatDoc     `json:"formats"`
}

type legacyDoc struct {
	Operation string `json:"operation"`
	Engine    string `json:"engine"`
}

type portDoc struct {
	Role string      `json:"role"`
	Min  json.Number `json:"min"`
	Max  json.Number `json:"max"`
}

type formatDoc struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func parseManifest(fileBytes []byte) (parsedManifest, error) {
	dec := json.NewDecoder(bytes.NewReader(fileBytes))
	dec.UseNumber()
	dec.DisallowUnknownFields()
	var doc manifestDoc
	if err := dec.Decode(&doc); err != nil {
		return parsedManifest{}, fmt.Errorf("approve manifest: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return parsedManifest{}, errors.New("approve manifest: trailing data")
	}
	if !extensionsObject(doc.Extensions) {
		return parsedManifest{}, errors.New("approve manifest: extensions")
	}
	if len(doc.Capabilities) != 1 {
		return parsedManifest{}, errors.New("approve manifest: capabilities")
	}
	cap := doc.Capabilities[0]
	version, kind := integerToken(cap.Version)
	if kind != numOK || version < 1 || version > 1<<31-1 {
		return parsedManifest{}, errors.New("approve manifest: version")
	}
	if len(cap.ID) == 0 || len(cap.ID) > 128 || !capabilityIDPattern.MatchString(cap.ID) {
		return parsedManifest{}, errors.New("approve manifest: capability id")
	}
	if cap.ClientOperation == "" {
		return parsedManifest{}, errors.New("approve manifest: client operation")
	}
	if err := validatePorts(cap.Inputs, true); err != nil {
		return parsedManifest{}, err
	}
	if err := validatePorts(cap.Outputs, false); err != nil {
		return parsedManifest{}, err
	}
	inputs, err := collectPorts("input", cap.Inputs)
	if err != nil {
		return parsedManifest{}, err
	}
	outputs, err := collectPorts("output", cap.Outputs)
	if err != nil {
		return parsedManifest{}, err
	}
	if _, err := resolveParameterSchema(cap.ParameterSchema); err != nil {
		return parsedManifest{}, fmt.Errorf("approve parameter schema: %w", err)
	}
	if len(cap.Formats) == 0 {
		return parsedManifest{}, errors.New("approve manifest: formats")
	}
	parsed := parsedManifest{
		id:              cap.ID,
		version:         int(version),
		clientOperation: cap.ClientOperation,
		schema:          append(json.RawMessage(nil), cap.ParameterSchema...),
	}
	if cap.Legacy != nil {
		if cap.Legacy.Operation == "" {
			return parsedManifest{}, errors.New("approve manifest: legacy operation")
		}
		if _, ok := legacyEngines[cap.Legacy.Engine]; !ok {
			return parsedManifest{}, errors.New("approve manifest: legacy engine")
		}
		parsed.hasLegacy = true
		parsed.legacyOp = cap.Legacy.Operation
		parsed.legacyEngine = cap.Legacy.Engine
	}
	seen := make(map[string]struct{}, len(cap.Formats))
	for _, format := range cap.Formats {
		source := convert.NormalizeFormat(format.From)
		target := convert.NormalizeFormat(format.To)
		if source == "" || target == "" {
			return parsedManifest{}, errors.New("approve manifest: format")
		}
		key := source + "\x00" + target
		if _, ok := seen[key]; ok {
			return parsedManifest{}, errors.New("approve manifest: duplicate format")
		}
		seen[key] = struct{}{}
		if parsed.hasLegacy {
			engine, ok := convert.Default.EngineForOp(parsed.legacyOp, format.From, format.To)
			if !ok || engine != parsed.legacyEngine {
				return parsedManifest{}, errors.New("approve manifest: legacy pair")
			}
		}
		parsed.formats = append(parsed.formats, storedFormat{source: source, target: target})
	}
	parsed.ports = append(inputs, outputs...)
	return parsed, nil
}

func collectPorts(direction string, ports []portDoc) ([]storedPort, error) {
	out := make([]storedPort, 0, len(ports))
	for i, port := range ports {
		min, minKind := integerToken(port.Min)
		max, maxKind := integerToken(port.Max)
		if minKind != numOK || maxKind != numOK {
			return nil, errors.New("approve manifest: port count")
		}
		out = append(out, storedPort{
			direction: direction,
			role:      port.Role,
			min:       int(min),
			max:       int(max),
			ordinal:   i,
		})
	}
	return out, nil
}

func validatePorts(ports []portDoc, inputs bool) error {
	if len(ports) == 0 {
		return errors.New("approve manifest: ports")
	}
	for _, port := range ports {
		if inputs {
			if port.Role != "source" {
				return errors.New("approve manifest: input role")
			}
		} else if _, ok := outputRoles[port.Role]; !ok {
			return errors.New("approve manifest: output role")
		}
		min, minKind := integerToken(port.Min)
		max, maxKind := integerToken(port.Max)
		if minKind != numOK || maxKind != numOK || min < 0 || max < 0 {
			return errors.New("approve manifest: port count")
		}
	}
	return nil
}

func resolveParameterSchema(raw []byte) (*jsonschema.Resolved, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, errors.New("parameter schema must be an object")
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(trimmed, &schema); err != nil {
		return nil, err
	}
	return schema.Resolve(nil)
}

func isFormatConflict(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return false
	}
	return pgErr.TableName == "worker_capability_formats" && pgErr.ConstraintName != "worker_capability_formats_pkey"
}
