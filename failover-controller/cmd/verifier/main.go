package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/sassendes/bedrock/failover-controller/act"
	"github.com/sassendes/bedrock/failover-controller/cli"
	"github.com/sassendes/bedrock/failover-controller/metrics"
	"github.com/sassendes/bedrock/failover-controller/proof"
	"github.com/sassendes/bedrock/failover-controller/store"
	"github.com/sassendes/bedrock/failover-controller/verifier"
	"github.com/sassendes/bedrock/failover-controller/witness"
)

func main() {
	fs := flag.CommandLine
	common := cli.Register(fs)
	st := store.DefaultConfig()
	cfg := verifier.DefaultConfig()
	wcfg := witness.DefaultConfig()
	var interval, cnpgGrace, catchUp time.Duration
	var keyDir, caDir, passwordFile string
	fs.DurationVar(&interval, "round", 4*time.Second, "length of a round; every round closes on this timer")
	fs.DurationVar(&cnpgGrace, "cnpg-grace", time.Minute, "how long cnpg gets to promote inside a cluster that still has ready standbys")
	fs.DurationVar(&catchUp, "catch-up", 30*time.Second, "how long a candidate behind the last signed position gets to replay from the archive before it is promoted anyway")
	fs.IntVar(&cfg.PerNode, "observers-per-node", cfg.PerNode, "observers on every node, folded into the node's one vote")
	fs.IntVar(&cfg.DeadNeeded, "dead-witnesses", cfg.DeadNeeded, "witnesses that must answer dead to prove a death")
	fs.StringVar(&cfg.ObserverSelector, "observer-selector", cfg.ObserverSelector, "label selector of the observer pods")
	fs.StringVar(&keyDir, "key-dir", "/etc/bedrock/proof-keys", "directory of proof keys, one hex file per key id")
	fs.StringVar(&caDir, "ca-dir", "/etc/bedrock/pg-ca", "directory of database ca certificates")
	fs.StringVar(&passwordFile, "pg-password-file", "/etc/bedrock/pg/password", "file holding the observer password, used to read replica streams")
	fs.IntVar(&wcfg.Check.Port, "pg-port", 5432, "postgres port")
	fs.StringVar(&wcfg.Check.Database, "pg-database", "app", "database holding the bedrock functions")
	fs.StringVar(&wcfg.Check.User, "pg-user", "bedrock_observer", "login used to read replica streams")
	fs.StringVar(&wcfg.Check.TLSMode, "pg-sslmode", "verify-ca", "postgres sslmode")
	fs.StringVar(&wcfg.Check.RootCA, "pg-ca-bundle", "/run/bedrock/pg-ca.crt", "where the joined ca bundle is written")
	fs.DurationVar(&wcfg.Check.Timeout, "check-timeout", time.Second, "deadline of one witness call")
	fs.DurationVar(&wcfg.MaxLag, "max-stream-lag", wcfg.MaxLag, "a replica stream older than this counts as gone")
	fs.StringVar(&wcfg.ManagerScheme, "manager-scheme", wcfg.ManagerScheme, "scheme of the instance manager")
	fs.IntVar(&wcfg.ManagerPort, "manager-port", wcfg.ManagerPort, "port of the instance manager")
	fs.StringVar(&wcfg.ManagerPath, "manager-path", wcfg.ManagerPath, "readiness path of the instance manager")
	fs.StringVar(&wcfg.Container, "container", wcfg.Container, "name of the postgres container")
	fs.StringVar(&st.VotePrefix, "vote-prefix", st.VotePrefix, "prefix of vote objects")
	fs.StringVar(&st.ClaimPrefix, "claim-prefix", st.ClaimPrefix, "prefix of claim objects")
	fs.StringVar(&st.RoleLabel, "role-label", st.RoleLabel, "label marking failover objects")
	flag.Parse()
	common.Finish()
	log := cli.Logger()
	cfg.ObserverNamespace = common.Namespace
	cfg.GraceRounds = int64((cnpgGrace + interval - 1) / interval)
	cfg.CatchUpRounds = int64((catchUp + interval - 1) / interval)
	st.Namespace = common.Namespace

	keys, err := proof.LoadKeys(keyDir)
	if err != nil {
		log.Error("proof keys", "err", err)
		os.Exit(1)
	}
	roots, bundle, err := cli.LoadCADir(caDir)
	if err != nil {
		log.Error("database cas", "err", err)
		os.Exit(1)
	}
	wcfg.Roots = roots
	if err := os.WriteFile(wcfg.Check.RootCA, bundle, 0o600); err != nil {
		log.Error("write ca bundle", "err", err)
		os.Exit(1)
	}
	pw, err := os.ReadFile(passwordFile)
	if err != nil {
		log.Error("read password", "err", err)
		os.Exit(1)
	}
	wcfg.Check.Password = strings.TrimSpace(string(pw))

	dyn, kube := common.Clients(log)
	m := metrics.New()
	actor := act.New(dyn, kube, common.Act)
	v, err := verifier.New(dyn, kube, common.Act.Discovery, store.New(kube, st), actor, witness.New(kube, actor, wcfg), keys, cfg, m, log)
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}
	v.ReadReplayWith(wcfg.Check)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	health := cli.NewHealth(3 * interval)
	common.Serve(m, health, log)
	log.Info("verifier started", "keys", len(keys), "round", interval)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if err := v.Tick(ctx); err != nil {
			log.Error("round", "err", err)
		} else {
			health.Mark()
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
