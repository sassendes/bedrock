package observe

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/sassendes/bedrock/failover-controller/proof"
)

type Result string

const (
	ResultUp         Result = "up"
	ResultDown       Result = "down"
	ResultNotPrimary Result = "not-primary"
)

// Proof is the evidence behind a Result. Every field is checked by dumb code, never interpreted.
type Proof struct {
	Observer   string        `json:"observer"`
	Target     string        `json:"target"`
	Result     Result        `json:"result"`
	Nonce      string        `json:"nonce"`
	EchoNonce  string        `json:"echoNonce"`
	InRecovery bool          `json:"inRecovery"`
	LSN        string        `json:"lsn"`
	SystemID   string        `json:"systemId"`
	Timeline   int64         `json:"timeline"`
	Latency    time.Duration `json:"latencyNs"`
	Reason     string        `json:"reason"`
	Signed     *proof.Signed `json:"signed,omitempty"`
	SignError  string        `json:"signError,omitempty"`
}

type CheckConfig struct {
	Host     string
	Port     int
	Database string
	User     string
	Password string
	Timeout  time.Duration
	TLSMode  string
	RootCA   string
}

// Only a primary that can actually write passes; a standby fails as read only.
const heartbeatSQL = `
WITH hb AS (
    INSERT INTO bedrock_heartbeat (observer, nonce, at)
    VALUES ($1, $2, now())
    ON CONFLICT (observer) DO UPDATE SET nonce = $2, at = now()
    RETURNING nonce
)
SELECT hb.nonce,
       pg_is_in_recovery(),
       pg_current_wal_lsn()::text,
       (SELECT system_identifier FROM pg_control_system())::text,
       (SELECT timeline_id FROM pg_control_checkpoint())
FROM hb`

const proveSQL = `SELECT key_id, instance, system_id, timeline, lsn, in_recovery, at_ms, mac FROM bedrock_prove($1, $2)`

const sqlStateReadOnly = "25006"
const sqlStateQueryCanceled = "57014"

func strongCheck(ctx context.Context, cfg CheckConfig, observer, target string) Proof {
	nonce, err := newNonce()
	if err != nil {
		return Proof{Observer: observer, Target: target, Result: ResultDown, Reason: "nonce: " + err.Error()}
	}
	return StrongCheckWithNonce(ctx, cfg, observer, target, nonce)
}

// A hung instance that still accepts connections times out and is voted down.
func StrongCheckWithNonce(ctx context.Context, cfg CheckConfig, observer, target, nonce string) Proof {
	p := Proof{Observer: observer, Target: target, Result: ResultDown, Nonce: nonce}

	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout+cfg.Timeout/2)
	defer cancel()

	start := time.Now()
	conn, err := connect(ctx, cfg)
	if err != nil {
		p.Latency = time.Since(start)
		p.Reason = "connect: " + err.Error()
		return p
	}
	defer conn.Close(context.Background())

	p.Signed, p.SignError = sign(ctx, conn, nonce, observer)

	err = conn.QueryRow(ctx, heartbeatSQL, observer, nonce).Scan(&p.EchoNonce, &p.InRecovery, &p.LSN, &p.SystemID, &p.Timeline)
	p.Latency = time.Since(start)

	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr) && pgErr.Code == sqlStateReadOnly:
		p.Result = ResultNotPrimary
		p.InRecovery = true
		p.Reason = "target is read only, not a primary"
		return p
	case errors.As(err, &pgErr) && pgErr.Code == sqlStateQueryCanceled:
		p.Reason = "statement timeout: the instance answered but could not write in time"
		return p
	case errors.Is(err, context.DeadlineExceeded):
		p.Reason = "timeout: no answer within the check deadline"
		return p
	case err != nil:
		p.Reason = "query: " + err.Error()
		return p
	}

	switch {
	case p.EchoNonce != nonce:
		p.Reason = fmt.Sprintf("nonce mismatch: sent %s, got %s", nonce, p.EchoNonce)
	case p.InRecovery:
		p.Result = ResultNotPrimary
		p.Reason = "pg_is_in_recovery() is true"
	default:
		p.Result = ResultUp
	}
	return p
}

func newNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func NewNonce() (string, error) { return newNonce() }

// sign asks postgres to sign the challenge. It runs before the write, so a standby signs too:
// it can only ever sign that it is in recovery.
func sign(ctx context.Context, conn *pgx.Conn, challenge, observer string) (*proof.Signed, string) {
	s := proof.Signed{Challenge: challenge, Observer: observer}
	err := conn.QueryRow(ctx, proveSQL, challenge, observer).Scan(&s.KeyID, &s.Instance, &s.SystemID, &s.Timeline, &s.LSN, &s.InRecovery, &s.AtMs, &s.MAC)
	if err != nil {
		return nil, err.Error()
	}
	return &s, ""
}

type ReceiverState struct {
	Status string
	AgeMs  int64
	Err    string
}

// Receiver reads the instance's WAL receiver through bedrock_receiver. No row means no stream at all.
func Receiver(ctx context.Context, cfg CheckConfig) ReceiverState {
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout+cfg.Timeout/2)
	defer cancel()
	conn, err := connect(ctx, cfg)
	if err != nil {
		return ReceiverState{Err: err.Error()}
	}
	defer conn.Close(context.Background())
	var r ReceiverState
	err = conn.QueryRow(ctx, "SELECT status, coalesce(age_ms, -1) FROM bedrock_receiver()").Scan(&r.Status, &r.AgeMs)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReceiverState{}
	}
	if err != nil {
		return ReceiverState{Err: err.Error()}
	}
	return r
}

func connect(ctx context.Context, cfg CheckConfig) (*pgx.Conn, error) {
	dsn := ""
	if cfg.TLSMode != "" {
		dsn = "sslmode=" + cfg.TLSMode
	}
	if cfg.RootCA != "" {
		dsn += " sslrootcert=" + cfg.RootCA
	}
	connCfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	connCfg.Host = cfg.Host
	connCfg.Port = uint16(cfg.Port)
	connCfg.Database = cfg.Database
	connCfg.User = cfg.User
	connCfg.Password = cfg.Password
	connCfg.ConnectTimeout = cfg.Timeout
	connCfg.RuntimeParams["statement_timeout"] = strconv.FormatInt(cfg.Timeout.Milliseconds(), 10)
	connCfg.RuntimeParams["application_name"] = "bedrock-observer"
	connCfg.Fallbacks = nil
	return pgx.ConnectConfig(ctx, connCfg)
}
