package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

const pgSuperPassword = "super-test-password"
const pgObserverPassword = "observer-test-password"

type pgInstance struct {
	t    *testing.T
	bin  string
	dir  string
	port int
}

func pgBin(t *testing.T) string {
	t.Helper()
	if b := os.Getenv("BEDROCK_PG_BIN"); b != "" {
		return b
	}
	matches, _ := filepath.Glob("/usr/lib/postgresql/*/bin/initdb")
	if len(matches) == 0 {
		t.Skip("no postgres binaries found, set BEDROCK_PG_BIN")
	}
	return filepath.Dir(matches[len(matches)-1])
}

func asPostgres(name string, args ...string) *exec.Cmd {
	if os.Geteuid() == 0 {
		return exec.Command("runuser", append([]string{"-u", "postgres", "--", name}, args...)...)
	}
	return exec.Command(name, args...)
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func runCmd(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", strings.Join(cmd.Args, " "), err, out)
	}
}

func workDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "bedrock-pg-")
	if err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		runCmd(t, exec.Command("chown", "postgres:postgres", dir))
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func startPrimary(t *testing.T) *pgInstance {
	t.Helper()
	bin := pgBin(t)
	root := workDir(t)
	pw := filepath.Join(root, "pw")
	if err := os.WriteFile(pw, []byte(pgSuperPassword), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &pgInstance{t: t, bin: bin, dir: filepath.Join(root, "primary"), port: freePort(t)}
	runCmd(t, asPostgres(filepath.Join(bin, "initdb"), "-D", p.dir, "-U", "postgres", "--pwfile", pw, "--auth-local=trust", "--auth-host=scram-sha-256"))
	hba := "host all all 127.0.0.1/32 scram-sha-256\nhost replication postgres 127.0.0.1/32 scram-sha-256\n"
	appendFile(t, filepath.Join(p.dir, "pg_hba.conf"), hba)
	p.start()
	p.exec(readSQL(t))
	p.exec(fmt.Sprintf("ALTER ROLE bedrock_observer PASSWORD '%s'", pgObserverPassword))
	return p
}

func (p *pgInstance) startStandby() *pgInstance {
	t := p.t
	t.Helper()
	s := &pgInstance{t: t, bin: p.bin, dir: p.dir + "-standby", port: freePort(t)}
	cmd := asPostgres(filepath.Join(p.bin, "pg_basebackup"), "-h", "127.0.0.1", "-p", fmt.Sprint(p.port), "-U", "postgres", "-D", s.dir, "-R", "-X", "stream")
	cmd.Env = append(os.Environ(), "PGPASSWORD="+pgSuperPassword)
	runCmd(t, cmd)
	s.start()
	return s
}

func (p *pgInstance) start() {
	t := p.t
	t.Helper()
	opts := fmt.Sprintf("-p %d -c listen_addresses=127.0.0.1 -c unix_socket_directories=%s", p.port, filepath.Dir(p.dir))
	runCmd(t, asPostgres(filepath.Join(p.bin, "pg_ctl"), "-D", p.dir, "-o", opts, "-l", p.dir+".log", "-w", "start"))
	t.Cleanup(func() {
		_ = asPostgres(filepath.Join(p.bin, "pg_ctl"), "-D", p.dir, "-m", "immediate", "stop").Run()
	})
}

func (p *pgInstance) super(ctx context.Context) *pgx.Conn {
	p.t.Helper()
	conn, err := pgx.Connect(ctx, fmt.Sprintf("postgres://postgres:%s@127.0.0.1:%d/postgres?sslmode=disable", pgSuperPassword, p.port))
	if err != nil {
		p.t.Fatal(err)
	}
	return conn
}

func (p *pgInstance) exec(sql string) {
	p.t.Helper()
	ctx := context.Background()
	conn := p.super(ctx)
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, sql); err != nil {
		p.t.Fatalf("exec %q: %v", sql, err)
	}
}

func (p *pgInstance) observerConfig(timeout time.Duration) CheckConfig {
	return CheckConfig{
		Host: "127.0.0.1", Port: p.port, Database: "postgres",
		User: "bedrock_observer", Password: pgObserverPassword,
		Timeout: timeout, TLSMode: "disable",
	}
}

func readSQL(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("heartbeat.sql")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func appendFile(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

func TestHeartbeatAgainstRealPostgres(t *testing.T) {
	primary := startPrimary(t)
	standby := primary.startStandby()
	ctx := context.Background()

	t.Run("healthy primary votes up with full proof", func(t *testing.T) {
		p := strongCheck(ctx, primary.observerConfig(2*time.Second), "node-1", "db/primary")
		if p.Result != ResultUp {
			t.Fatalf("want up, got %+v", p)
		}
		if p.EchoNonce != p.Nonce || len(p.Nonce) != 32 {
			t.Errorf("nonce not echoed: %+v", p)
		}
		if p.InRecovery || p.LSN == "" || p.SystemID == "" || p.Timeline < 1 {
			t.Errorf("incomplete proof: %+v", p)
		}
	})

	t.Run("every check uses a fresh nonce", func(t *testing.T) {
		a := strongCheck(ctx, primary.observerConfig(2*time.Second), "node-1", "db/primary")
		b := strongCheck(ctx, primary.observerConfig(2*time.Second), "node-1", "db/primary")
		if a.Nonce == b.Nonce {
			t.Fatal("nonce reused")
		}
	})

	t.Run("standby is reported as not primary, not as up", func(t *testing.T) {
		p := strongCheck(ctx, standby.observerConfig(2*time.Second), "node-1", "db/standby")
		if p.Result != ResultNotPrimary {
			t.Fatalf("want not-primary, got %+v", p)
		}
	})

	t.Run("standby shares the primary's system identifier", func(t *testing.T) {
		var sysID string
		conn := standby.super(ctx)
		defer conn.Close(ctx)
		if err := conn.QueryRow(ctx, "SELECT system_identifier::text FROM pg_control_system()").Scan(&sysID); err != nil {
			t.Fatal(err)
		}
		p := strongCheck(ctx, primary.observerConfig(2*time.Second), "node-1", "db/primary")
		if sysID != p.SystemID {
			t.Fatalf("system id %s on standby, %s on primary", sysID, p.SystemID)
		}
	})

	t.Run("hung write votes down even though the port answers", func(t *testing.T) {
		strongCheck(ctx, primary.observerConfig(2*time.Second), "node-hang", "db/primary")
		blocker := primary.super(ctx)
		defer blocker.Close(ctx)
		tx, err := blocker.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if _, err := tx.Exec(ctx, "SELECT 1 FROM bedrock_heartbeat WHERE observer = 'node-hang' FOR UPDATE"); err != nil {
			t.Fatal(err)
		}
		p := strongCheck(ctx, primary.observerConfig(500*time.Millisecond), "node-hang", "db/primary")
		if p.Result != ResultDown || !strings.Contains(p.Reason, "statement timeout") {
			t.Fatalf("want down by statement timeout, got %+v", p)
		}
	})

	t.Run("nothing listening votes down", func(t *testing.T) {
		cfg := primary.observerConfig(time.Second)
		cfg.Port = freePort(t)
		p := strongCheck(ctx, cfg, "node-1", "db/gone")
		if p.Result != ResultDown || !strings.HasPrefix(p.Reason, "connect:") {
			t.Fatalf("want down on connect, got %+v", p)
		}
	})

	t.Run("wrong password votes down", func(t *testing.T) {
		cfg := primary.observerConfig(time.Second)
		cfg.Password = "nope"
		if p := strongCheck(ctx, cfg, "node-1", "db/primary"); p.Result != ResultDown {
			t.Fatalf("want down, got %+v", p)
		}
	})

	t.Run("observer login can touch nothing but its heartbeat", func(t *testing.T) {
		primary.exec("CREATE TABLE IF NOT EXISTS accounts (id int, balance numeric); INSERT INTO accounts VALUES (1, 100)")
		cfg := primary.observerConfig(time.Second)
		conn, err := pgx.Connect(ctx, fmt.Sprintf("postgres://bedrock_observer:%s@127.0.0.1:%d/postgres?sslmode=disable", cfg.Password, cfg.Port))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close(ctx)
		for _, sql := range []string{
			"SELECT * FROM accounts",
			"UPDATE accounts SET balance = 0",
			"DELETE FROM bedrock_heartbeat",
			"SELECT at FROM bedrock_heartbeat",
			"CREATE TABLE evil (x int)",
		} {
			if _, err := conn.Exec(ctx, sql); err == nil {
				t.Errorf("observer was allowed: %s", sql)
			}
		}
	})
}
