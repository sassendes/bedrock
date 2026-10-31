package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/sassendes/bedrock/failover-controller/act"
	"github.com/sassendes/bedrock/failover-controller/checker"
	"github.com/sassendes/bedrock/failover-controller/cli"
	"github.com/sassendes/bedrock/failover-controller/metrics"
)

func main() {
	fs := flag.CommandLine
	common := cli.Register(fs)
	cfg := checker.DefaultConfig()
	var caDir string
	fs.StringVar(&cfg.Scheme, "scheme", cfg.Scheme, "scheme of the instance manager status server")
	fs.IntVar(&cfg.Port, "port", cfg.Port, "port of the instance manager status server")
	fs.StringVar(&cfg.Path, "path", cfg.Path, "health path of the instance manager")
	fs.DurationVar(&cfg.Interval, "interval", cfg.Interval, "time between rounds")
	fs.DurationVar(&cfg.Timeout, "timeout", cfg.Timeout, "deadline of one health call")
	fs.StringVar(&caDir, "ca-dir", "/etc/bedrock/pg-ca", "directory of database ca certificates")
	flag.Parse()
	common.Finish()
	log := cli.Logger()

	roots, _, err := cli.LoadCADir(caDir)
	if err != nil {
		log.Error("database cas", "err", err)
		os.Exit(1)
	}
	dyn, kube := common.Clients(log)
	m := metrics.New()
	c := checker.New(kube, dyn, common.Act.Discovery, act.New(dyn, kube, common.Act), cfg, roots, m, log)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	common.Serve(m, log)
	c.Run(ctx)
}
