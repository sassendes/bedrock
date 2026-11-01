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
	"github.com/sassendes/bedrock/failover-controller/decider"
	"github.com/sassendes/bedrock/failover-controller/metrics"
	"github.com/sassendes/bedrock/failover-controller/store"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

func main() {
	fs := flag.CommandLine
	common := cli.Register(fs)
	st := store.DefaultConfig()
	cfg := decider.DefaultConfig()
	p := &cfg.Params
	var leaseName, phases string
	var leaseDuration, renewDeadline, retryPeriod, tick time.Duration
	fs.StringVar(&cfg.Identity, "identity", cli.Env("POD_NAME", ""), "this decider's name, must be the pod name for the admission policy")
	fs.StringVar(&leaseName, "lease", "bedrock-decider", "name of the Lease that picks the decider")
	fs.DurationVar(&leaseDuration, "lease-duration", 15*time.Second, "lease validity")
	fs.DurationVar(&renewDeadline, "renew-deadline", 10*time.Second, "give up leading if not renewed within this")
	fs.DurationVar(&retryPeriod, "retry-period", 2*time.Second, "time between lease attempts")
	fs.DurationVar(&tick, "tick", time.Second, "time between decisions")
	fs.StringVar(&cfg.ObserverSelector, "observer-selector", cfg.ObserverSelector, "label selector of the observer pods")
	fs.StringVar(&cfg.PauseAnnotation, "pause-annotation", cfg.PauseAnnotation, "annotation that pauses failover of a cluster when true")
	fs.StringVar(&phases, "transitional-phases", strings.Join(cfg.TransitionalPhases, ";"), "semicolon separated phases during which the operator is fixing the cluster itself")
	fs.DurationVar(&cfg.FenceVerifyTimeout, "fence-verify-timeout", cfg.FenceVerifyTimeout, "how long a fence may take to verify before halting")
	fs.DurationVar(&cfg.PromoteTimeout, "promote-timeout", cfg.PromoteTimeout, "how long to wait for the promoted cluster to be healthy")
	fs.DurationVar(&cfg.ClaimTimeout, "claim-timeout", cfg.ClaimTimeout, "an unfenced claim older than this halts")
	fs.IntVar(&cfg.PerNode, "observers-per-node", cfg.PerNode, "observers on every node, folded into the node's one vote")
	fs.DurationVar(&cfg.RoundStale, "round-stale", cfg.RoundStale, "an approval is not acted on once the verifier has not closed a round for this long")
	fs.DurationVar(&cfg.ClockOffset, "clock-offset", cfg.ClockOffset, "assumed bound on wall clock offset between the decider and the node controller")
	fs.DurationVar(&p.ProbeInterval, "probe-interval", p.ProbeInterval, "observer probe interval")
	fs.DurationVar(&p.Freshness, "freshness", p.Freshness, "how long a vote counts")
	fs.DurationVar(&p.DeadAfter, "dead-after", p.DeadAfter, "time suspect before dead")
	fs.DurationVar(&p.CnpgGrace, "cnpg-grace", p.CnpgGrace, "time suspect before dead while the cluster still has ready standbys, so cnpg can promote one first")
	fs.DurationVar(&p.Cooldown, "cooldown", p.Cooldown, "no second failover of a target within this")
	fs.DurationVar(&p.CapWindow, "cap-window", p.CapWindow, "window of the failover cap")
	fs.IntVar(&p.CapCount, "cap-count", p.CapCount, "automatic failovers allowed per window, all targets together")
	fs.IntVar(&p.BrakeCount, "brake-count", p.BrakeCount, "suspect targets at once that stop every decision")
	fs.IntVar(&p.MaxAbstain, "max-abstain", p.MaxAbstain, "abstaining or silent observers tolerated per decision")
	fs.StringVar(&st.VotePrefix, "vote-prefix", st.VotePrefix, "prefix of vote objects")
	fs.StringVar(&st.ClaimPrefix, "claim-prefix", st.ClaimPrefix, "prefix of claim objects")
	fs.StringVar(&st.RoleLabel, "role-label", st.RoleLabel, "label marking vote and claim objects")
	flag.Parse()
	common.Finish()
	log := cli.Logger()
	cfg.TransitionalPhases = strings.Split(phases, ";")
	cfg.ObserverNamespace = common.Namespace
	st.Namespace = common.Namespace

	dyn, kube := common.Clients(log)
	m := metrics.New()
	var le *leaderelection.LeaderElector
	leading := func() bool { return le != nil && le.IsLeader() }
	d, err := decider.New(dyn, kube, common.Act.Discovery, store.New(kube, st), act.New(dyn, kube, common.Act), cfg, leading, m, log)
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	health := cli.NewHealth(3*tick + leaseDuration)
	common.Serve(m, health, log)

	lock := &resourcelock.LeaseLock{
		LeaseMeta:  metav1.ObjectMeta{Name: leaseName, Namespace: common.Namespace},
		Client:     kube.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{Identity: cfg.Identity},
	}
	le, err = leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock:            lock,
		LeaseDuration:   leaseDuration,
		RenewDeadline:   renewDeadline,
		RetryPeriod:     retryPeriod,
		ReleaseOnCancel: true,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				log.Info("leading", "identity", cfg.Identity)
				t := time.NewTicker(tick)
				defer t.Stop()
				for {
					if err := d.Tick(ctx); err != nil {
						log.Error("tick", "err", err)
					} else {
						health.Mark()
					}
					select {
					case <-ctx.Done():
						return
					case <-t.C:
					}
				}
			},
			OnStoppedLeading: func() {
				log.Warn("lost the lease, exiting so nothing runs on a stale view", "identity", cfg.Identity)
				os.Exit(0)
			},
		},
	})
	if err != nil {
		log.Error("leader election", "err", err)
		os.Exit(1)
	}
	m.Set("bedrock_failover_leader", nil, 0)
	go func() {
		t := time.NewTicker(tick)
		defer t.Stop()
		for {
			if !leading() {
				health.Mark()
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
	le.Run(ctx)
}
