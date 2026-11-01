package store

import (
	"context"
	"sort"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const CheckerObject = "bedrock-checker"

// GetHalts reads the checker's object: a target is halted while the fallback is on and the checker sees
// its database up. Admission denies every decider write while any target is halted.
func (s *Store) GetHalts(ctx context.Context) (map[string]bool, error) {
	cm, err := s.cms().Get(ctx, CheckerObject, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for k, v := range cm.Data {
		if key, field, ok := cut(k); ok && field == "halt" {
			out[key] = v == "true"
		}
	}
	return out, nil
}

func Halted(h map[string]bool) []string {
	var out []string
	for k, v := range h {
		if v {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func (s *Store) PutHalts(ctx context.Context, halts map[string]bool) error {
	data := map[string]string{}
	for target, h := range halts {
		v := "false"
		if h {
			v = "true"
		}
		data[ApprovalKey(target)+".halt"] = v
	}
	return s.upsert(ctx, CheckerObject, "checker", data)
}
