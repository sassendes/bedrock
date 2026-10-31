package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type Set struct{ labels []map[string]string }

func NewSet() *Set { return &Set{} }

func (s *Set) Add(l map[string]string) { s.labels = append(s.labels, l) }

func (s *Set) Len() int { return len(s.labels) }

type Registry struct {
	mu     sync.Mutex
	series map[string]map[string]float64
}

func New() *Registry { return &Registry{series: map[string]map[string]float64{}} }

func key(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	ks := make([]string, 0, len(labels))
	for k := range labels {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	parts := make([]string, 0, len(ks))
	for _, k := range ks {
		parts = append(parts, k+"="+strconv.Quote(labels[k]))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func (r *Registry) Set(name string, labels map[string]string, v float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.series[name] == nil {
		r.series[name] = map[string]float64{}
	}
	r.series[name][key(labels)] = v
}

// Replace swaps a whole family at once, so a page that cleared disappears instead of sticking.
func (r *Registry) Replace(name string, s *Set) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m := map[string]float64{}
	for _, l := range s.labels {
		m[key(l)] = 1
	}
	r.series[name] = m
}

func (r *Registry) Get(name string, labels map[string]string) (float64, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.series[name][key(labels)]
	return v, ok
}

func (r *Registry) Count(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.series[name])
}

func (r *Registry) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, 0, len(r.series))
	for n := range r.series {
		names = append(names, n)
	}
	sort.Strings(names)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	for _, n := range names {
		fmt.Fprintf(w, "# TYPE %s gauge\n", n)
		ks := make([]string, 0, len(r.series[n]))
		for k := range r.series[n] {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		for _, k := range ks {
			fmt.Fprintf(w, "%s%s %s\n", n, k, strconv.FormatFloat(r.series[n][k], 'g', -1, 64))
		}
	}
}
