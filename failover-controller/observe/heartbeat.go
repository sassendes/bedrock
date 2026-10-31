package main

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
)

type Result string

const (
	ResultUp         Result = "up"
	ResultDown       Result = "down"
	ResultNotPrimary Result = "not-primary"
)

type Proof struct {
	Observer   string
	Target     string
	Result     Result
	Nonce      string
	EchoNonce  string
	InRecovery bool
	LSN        string
	SystemID   string
	Timeline   int64
	Latency    time.Duration
	Reason     string
}

type CheckConfig struct {
	Host     string
	Port     int
	Database string
	User     string
	Password string
	Timeout  time.Duration
	TLSMode  string
}

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

const sqlStateReadOnly = "25006"
const sqlStateQueryCanceled = "57014"

func strongCheck(ctx context.Context, cfg CheckConfig, observer, target string) Proof {
	p := Proof{Observer: observer, Target: target, Result: ResultDown}

	nonce, err := newNonce()
	if err != nil {
		p.Reason = "nonce: " + err.Error()
		return p
	}
	p.Nonce = nonce

	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout+cfg.Timeout/2)
	defer cancel()

	connCfg, err := pgx.ParseConfig("")
	if err != nil {
		p.Reason = "config: " + err.Error()
		return p
	}
	connCfg.Host = cfg.Host
	connCfg.Port = uint16(cfg.Port)
	connCfg.Database = cfg.Database
	connCfg.User = cfg.User
	connCfg.Password = cfg.Password
	connCfg.ConnectTimeout = cfg.Timeout
	connCfg.RuntimeParams["statement_timeout"] = strconv.FormatInt(cfg.Timeout.Milliseconds(), 10)
	connCfg.RuntimeParams["application_name"] = "bedrock-observer"
	if cfg.TLSMode == "disable" {
		connCfg.TLSConfig = nil
		connCfg.Fallbacks = nil
	}

	start := time.Now()
	conn, err := pgx.ConnectConfig(ctx, connCfg)
	if err != nil {
		p.Latency = time.Since(start)
		p.Reason = "connect: " + err.Error()
		return p
	}
	defer conn.Close(context.Background())

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
