package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sassendes/bedrock/failover-controller/act"
	"github.com/sassendes/bedrock/failover-controller/checker"
	"github.com/sassendes/bedrock/failover-controller/cli"
	"github.com/sassendes/bedrock/failover-controller/metrics"
	"github.com/sassendes/bedrock/failover-controller/store"
)

func main() {
	fs := flag.CommandLine
	common := cli.Register(fs)
	cfg := checker.DefaultConfig()
	fs.StringVar(&cfg.Scheme, "scheme", cfg.Scheme, "scheme of the instance manager status server")
	fs.IntVar(&cfg.Port, "port", cfg.Port, "port of the instance manager status server")
	fs.StringVar(&cfg.Path, "path", cfg.Path, "health path of the instance manager")
	fs.DurationVar(&cfg.Interval, "interval", cfg.Interval, "time between rounds")
	fs.DurationVar(&cfg.Timeout, "timeout", cfg.Timeout, "deadline of one health call")
	fs.StringVar(&cfg.LeaseName, "lease", cfg.LeaseName, "the decider's Lease")
	fs.DurationVar(&cfg.RoundStale, "round-stale", cfg.RoundStale, "the verifier counts as stalled once no round closed for this long")
	fs.IntVar(&cfg.PerNode, "observers-per-node", cfg.PerNode, "observers on every node")
	fs.StringVar(&cfg.ObserverSelector, "observer-selector", cfg.ObserverSelector, "label selector of the observer pods")
	caFlags := cli.RegisterCA(fs)
	flag.Parse()
	common.Finish()
	log := cli.Logger()

	cfg.Namespace = common.Namespace
	st := store.DefaultConfig()
	st.Namespace = common.Namespace
	dyn, kube := common.Clients(log)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	cas, err := cli.NewCABundle(ctx, caFlags.Source(kube, common.Namespace), "")
	if err != nil {
		log.Error("database cas", "err", err)
		os.Exit(1)
	}
	m := metrics.New()
	c := checker.New(kube, dyn, common.Act.Discovery, act.New(dyn, kube, common.Act), store.New(kube, st), cfg, cas.Pool, m, log)
	health := cli.NewHealth(3*cfg.Interval + cfg.Timeout)
	common.Serve(m, health, log)
	go cas.Watch(ctx, 30*time.Second, log)
	c.Run(ctx, health.Mark)
}
