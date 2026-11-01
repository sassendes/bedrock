package store

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	RoundObject     = "bedrock-round"
	ApprovalsObject = "bedrock-approvals"
)

// Round is written only by the verifier: this round's challenge and what the last round proved.
type Round struct {
	Number    int64               `json:"number"`
	Challenge string              `json:"challenge"`
	IssuedAt  time.Time           `json:"issuedAt"`
	ValidUps  map[string][]string `json:"validUps"`
	Signers   map[string][]string `json:"signers"`
	Outcomes  map[string]string   `json:"outcomes"`
	Split     map[string]bool     `json:"split"`
	ProvenLSN map[string]string   `json:"provenLsn"`
	DeadSince map[string]DeadMark `json:"deadSince"`
}

// DeadMark is when the verifier first proved this instance dead, by round number, so the cnpg window
// is counted in rounds and survives a verifier restart.
type DeadMark struct {
	Instance string `json:"instance"`
	Round    int64  `json:"round"`
}

func (s *Store) GetRound(ctx context.Context) (*Round, error) {
	cm, err := s.cms().Get(ctx, RoundObject, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var r Round
	if err := json.Unmarshal([]byte(cm.Data["round"]), &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *Store) PutRound(ctx context.Context, r Round) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return s.upsert(ctx, RoundObject, "round", map[string]string{"round": string(b)})
}

func (s *Store) upsert(ctx context.Context, name, role string, data map[string]string) error {
	cm, err := s.cms().Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = s.cms().Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{s.cfg.RoleLabel: role}},
			Data:       data,
		}, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	cm.Data = data
	_, err = s.cms().Update(ctx, cm, metav1.UpdateOptions{})
	return err
}

type ApprovalState string

const (
	ApprovalValid   ApprovalState = "valid"
	ApprovalRevoked ApprovalState = "revoked"
	ApprovalClosed  ApprovalState = "closed"
)

type Approval struct {
	State    ApprovalState
	Epoch    int64
	Round    int64
	Instance string
}

// ApprovalKey is the one slot per target. Admission reads <key>.state and <key>.epoch, so a run
// overwrites the last one and an old epoch can never match again.
func ApprovalKey(target string) string { return strings.ReplaceAll(target, "/", ".") }

func (s *Store) GetApprovals(ctx context.Context) (map[string]Approval, error) {
	cm, err := s.cms().Get(ctx, ApprovalsObject, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return map[string]Approval{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]Approval{}
	for k, v := range cm.Data {
		key, field, ok := cut(k)
		if !ok {
			continue
		}
		a := out[key]
		switch field {
		case "state":
			a.State = ApprovalState(v)
		case "epoch":
			a.Epoch, _ = strconv.ParseInt(v, 10, 64)
		case "round":
			a.Round, _ = strconv.ParseInt(v, 10, 64)
		case "instance":
			a.Instance = v
		}
		out[key] = a
	}
	return out, nil
}

func cut(k string) (string, string, bool) {
	i := strings.LastIndex(k, ".")
	if i <= 0 {
		return "", "", false
	}
	return k[:i], k[i+1:], true
}

func (s *Store) SetApproval(ctx context.Context, target string, a Approval) error {
	cm, err := s.cms().Get(ctx, ApprovalsObject, metav1.GetOptions{})
	data := map[string]string{}
	if err == nil {
		for k, v := range cm.Data {
			data[k] = v
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}
	key := ApprovalKey(target)
	data[key+".state"] = string(a.State)
	data[key+".epoch"] = strconv.FormatInt(a.Epoch, 10)
	data[key+".round"] = strconv.FormatInt(a.Round, 10)
	data[key+".instance"] = a.Instance
	if apierrors.IsNotFound(err) {
		_, err = s.cms().Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: ApprovalsObject, Labels: map[string]string{s.cfg.RoleLabel: "approvals"}},
			Data:       data,
		}, metav1.CreateOptions{})
		return err
	}
	cm.Data = data
	_, err = s.cms().Update(ctx, cm, metav1.UpdateOptions{})
	if apierrors.IsConflict(err) {
		return ErrConflict
	}
	return err
}
