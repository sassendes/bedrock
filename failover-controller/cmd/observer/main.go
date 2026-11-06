package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sassendes/bedrock/failover-controller/act"
	"github.com/sassendes/bedrock/failover-controller/cli"
	"github.com/sassendes/bedrock/failover-controller/metrics"
	"github.com/sassendes/bedrock/failover-controller/observer"
	"github.com/sassendes/bedrock/failover-controller/store"
	"github.com/sassendes/bedrock/failover-controller/transport"
)

func main() {
	fs := flag.CommandLine
	common := cli.Register(fs)
	st := store.DefaultConfig()
	cfg := observer.Config{}
	var passwordFile, certFile, keyFile, caFile, serverName string
	fs.StringVar(&cfg.Node, "node", cli.Env("NODE_NAME", ""), "name of the node this observer runs on")
	fs.StringVar(&cfg.Slot, "slot", cli.Env("OBSERVER_SLOT", ""), "which of the node's observers this is")
	fs.StringVar(&cfg.SlotLabel, "slot-label", "bedrock.io/observer-slot", "pod label holding an observer's slot")
	fs.StringVar(&cfg.Selector, "observer-selector", "app.kubernetes.io/name=bedrock-observer", "label selector of the observer pods")
	fs.IntVar(&cfg.ExchangePort, "exchange-port", 8443, "port of the peer exchange")
	fs.DurationVar(&cfg.ProbeInterval, "probe-interval", 2*time.Second, "time between rounds")
	fs.DurationVar(&cfg.SuspectInterval, "suspect-probe-interval", 500*time.Millisecond, "time between rounds while a target looks down")
	fs.DurationVar(&cfg.AskTimeout, "ask-timeout", 1500*time.Millisecond, "how long to wait for peer answers")
	fs.IntVar(&cfg.Check.Port, "pg-port", 5432, "postgres port")
	fs.StringVar(&cfg.Check.Database, "pg-database", "app", "database holding bedrock_heartbeat")
	fs.StringVar(&cfg.Check.User, "pg-user", "bedrock_observer", "login of the observer")
	fs.StringVar(&passwordFile, "pg-password-file", "/etc/bedrock/pg/password", "file holding the observer password")
	fs.StringVar(&cfg.Check.TLSMode, "pg-sslmode", "verify-ca", "postgres sslmode")
	caFlags := cli.RegisterCA(fs)
	fs.StringVar(&cfg.Check.RootCA, "pg-ca-bundle", "/run/bedrock/pg-ca.crt", "where the joined ca bundle is written")
	fs.DurationVar(&cfg.Check.Timeout, "check-timeout", time.Second, "deadline of one strong check")
	fs.StringVar(&certFile, "tls-cert", "/etc/bedrock/exchange/tls.crt", "exchange certificate")
	fs.StringVar(&keyFile, "tls-key", "/etc/bedrock/exchange/tls.key", "exchange key")
	fs.StringVar(&caFile, "tls-ca", "/etc/bedrock/exchange/ca.crt", "exchange ca")
	fs.StringVar(&serverName, "tls-server-name", "bedrock-observer", "name in the exchange certificate")
	fs.StringVar(&st.VotePrefix, "vote-prefix", st.VotePrefix, "prefix of vote objects")
	fs.StringVar(&st.RoleLabel, "role-label", st.RoleLabel, "label marking vote and claim objects")
	flag.Parse()
	common.Finish()
	log := cli.Logger()

	if cfg.Node == "" {
		log.Error("node name is empty, set NODE_NAME from spec.nodeName")
		os.Exit(1)
	}
	if cfg.Slot == "" || strings.ContainsAny(cfg.Slot, "-/.") {
		log.Error("slot must be set and hold no dash, slash or dot", "slot", cfg.Slot)
		os.Exit(1)
	}
	cfg.Namespace = common.Namespace
	st.Namespace = common.Namespace
	pw, err := os.ReadFile(passwordFile)
	if err != nil {
		log.Error("read password", "err", err)
		os.Exit(1)
	}
	cfg.Check.Password = strings.TrimSpace(string(pw))

	serverTLS, clientTLS, err := transport.LoadTLS(certFile, keyFile, caFile, serverName)
	if err != nil {
		log.Error("exchange tls", "err", err)
		os.Exit(1)
	}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		log.Error("generate key", "err", err)
		os.Exit(1)
	}

	dyn, kube := common.Clients(log)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	var cas *cli.CABundle
	if cfg.Check.TLSMode != "disable" {
		cas, err = cli.NewCABundle(ctx, caFlags.Source(kube, common.Namespace), cfg.Check.RootCA)
		if err != nil {
			log.Error("database cas", "err", err)
			os.Exit(1)
		}
	}
	m := metrics.New()
	tr := transport.NewHTTP(clientTLS, cfg.AskTimeout)
	o := observer.New(kube, dyn, common.Act.Discovery, act.New(dyn, kube, common.Act), store.New(kube, st), cfg, key, tr, tr, m, log)

	health := cli.NewHealth(3*cfg.ProbeInterval + cfg.AskTimeout + 2*cfg.Check.Timeout)
	common.Serve(m, health, log)
	if cas != nil {
		go cas.Watch(ctx, 30*time.Second, log)
	}

	srv := &http.Server{
		Addr:              ":" + strconv.Itoa(cfg.ExchangePort),
		Handler:           transport.NewHandler(o.Exchange(), 64, 1<<20, 2*cfg.Check.Timeout+cfg.AskTimeout),
		TLSConfig:         serverTLS,
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
	}
	go func() {
		if err := srv.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			log.Error("exchange server", "err", err)
			os.Exit(1)
		}
	}()
	log.Info("observer started", "id", o.ID())
	o.Run(ctx, health.Mark)
	_ = srv.Close()
}
