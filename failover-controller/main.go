package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	cfg := defaultConfig()
	kubeconfig := flag.String("kubeconfig", os.Getenv("KUBECONFIG"), "path to a kubeconfig, empty means in-cluster")
	drZones := flag.String("dr-zones", "", "comma separated zones that hold dr clusters")
	flag.StringVar(&cfg.ClusterGVR.Group, "cluster-group", cfg.ClusterGVR.Group, "api group of the database cluster resource")
	flag.StringVar(&cfg.ClusterGVR.Version, "cluster-version", cfg.ClusterGVR.Version, "api version of the database cluster resource")
	flag.StringVar(&cfg.ClusterGVR.Resource, "cluster-resource", cfg.ClusterGVR.Resource, "plural resource name of the database cluster")
	flag.StringVar(&cfg.ClusterKind, "cluster-kind", cfg.ClusterKind, "kind that owns a cluster's services")
	flag.StringVar(&cfg.ClusterLabel, "cluster-label", cfg.ClusterLabel, "pod label holding the cluster name")
	flag.StringVar(&cfg.InstanceSelector, "instance-selector", cfg.InstanceSelector, "label selector for database instance pods")
	flag.StringVar(&cfg.ZoneLabel, "zone-label", cfg.ZoneLabel, "node label holding the zone")
	flag.Parse()

	for _, z := range strings.Split(*drZones, ",") {
		if z = strings.TrimSpace(z); z != "" {
			cfg.DRZones[z] = true
		}
	}

	rc, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	dyn := dynamic.NewForConfigOrDie(rc)
	kube := kubernetes.NewForConfigOrDie(rc)

	d, err := discover(context.Background(), dyn, kube, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	for _, f := range d.Families {
		p := f.Primary
		fmt.Printf("family %s\n", p.Key())
		fmt.Printf("  primary  %-28s %d/%d ready  current %s\n", p.Key(), p.ReadyInstances, p.Instances, p.CurrentPrimary)
		for _, group := range [][]Member{f.Live, f.Delayed, f.DR} {
			for _, m := range group {
				fmt.Printf("  %-8s %-28s %d/%d ready  zones %v  delay %s\n", m.Role, m.Key(), m.ReadyInstances, m.Instances, m.Zones, m.ApplyDelay)
			}
		}
	}
	for _, pr := range d.Problems {
		fmt.Printf("problem  %s: %s\n", pr.Cluster, pr.Reason)
	}
	if len(d.Problems) > 0 {
		os.Exit(2)
	}
}
