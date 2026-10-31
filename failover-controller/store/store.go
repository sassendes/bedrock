package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sassendes/bedrock/failover-controller/observe"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type Config struct {
	Namespace   string
	VotePrefix  string
	ClaimPrefix string
	RoleLabel   string
}

func DefaultConfig() Config {
	return Config{Namespace: "bedrock-failover", VotePrefix: "vote-", ClaimPrefix: "claim-", RoleLabel: "bedrock.io/failover-role"}
}

var ErrConflict = errors.New("claim changed under us")

type Store struct {
	kube kubernetes.Interface
	cfg  Config
}

func New(kube kubernetes.Interface, cfg Config) *Store { return &Store{kube: kube, cfg: cfg} }

func (s *Store) cms() interface {
	Get(context.Context, string, metav1.GetOptions) (*corev1.ConfigMap, error)
	Create(context.Context, *corev1.ConfigMap, metav1.CreateOptions) (*corev1.ConfigMap, error)
	Update(context.Context, *corev1.ConfigMap, metav1.UpdateOptions) (*corev1.ConfigMap, error)
	List(context.Context, metav1.ListOptions) (*corev1.ConfigMapList, error)
} {
	return s.kube.CoreV1().ConfigMaps(s.cfg.Namespace)
}

type Ballots struct {
	Node    string
	PubKey  string
	Votes   map[string]observe.Vote
	Version string
}

// PublishVotes writes this node's ballots into its own object. Admission ties the object's name to the
// node in the writer's token, so no observer can write for another.
func (s *Store) PublishVotes(ctx context.Context, node, pubKey string, votes map[string]observe.Vote) error {
	b, err := json.Marshal(votes)
	if err != nil {
		return err
	}
	name := s.cfg.VotePrefix + node
	data := map[string]string{"node": node, "pubkey": pubKey, "votes": string(b)}
	cm, err := s.cms().Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = s.cms().Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{s.cfg.RoleLabel: "vote"}},
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

func (s *Store) ListBallots(ctx context.Context) ([]Ballots, error) {
	list, err := s.cms().List(ctx, metav1.ListOptions{LabelSelector: s.cfg.RoleLabel + "=vote"})
	if err != nil {
		return nil, err
	}
	var out []Ballots
	for _, cm := range list.Items {
		node := strings.TrimPrefix(cm.Name, s.cfg.VotePrefix)
		if cm.Data["node"] != node {
			continue
		}
		b := Ballots{Node: node, PubKey: cm.Data["pubkey"], Votes: map[string]observe.Vote{}, Version: cm.ResourceVersion}
		if err := json.Unmarshal([]byte(cm.Data["votes"]), &b.Votes); err != nil {
			continue
		}
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Node < out[j].Node })
	return out, nil
}

type ClaimState string

const (
	ClaimOpen   ClaimState = "open"
	ClaimClosed ClaimState = "closed"
	ClaimHalted ClaimState = "halted"
)

type Step string

const (
	StepClaimed   Step = "claimed"
	StepFencing   Step = "fencing"
	StepFenced    Step = "fenced"
	StepPromoting Step = "promoting"
	StepPromoted  Step = "promoted"
	StepRepointed Step = "repointed"
	StepFlipped   Step = "flipped"
)

type Claim struct {
	Target    string            `json:"target"`
	Epoch     int64             `json:"epoch"`
	State     ClaimState        `json:"state"`
	Step      Step              `json:"step"`
	Decider   string            `json:"decider"`
	Candidate string            `json:"candidate"`
	Followers []string          `json:"followers"`
	Nodes     []string          `json:"nodes"`
	StartedAt time.Time         `json:"startedAt"`
	StepAt    time.Time         `json:"stepAt"`
	Reason    string            `json:"reason"`
	Times     map[string]string `json:"times"`
	Notes     []string          `json:"notes"`
	version   string
}

func (s *Store) claimName(target string) string {
	return s.cfg.ClaimPrefix + strings.ReplaceAll(target, "/", ".")
}

func decodeClaim(cm *corev1.ConfigMap) (*Claim, error) {
	var c Claim
	if err := json.Unmarshal([]byte(cm.Data["claim"]), &c); err != nil {
		return nil, fmt.Errorf("claim %s is unreadable: %w", cm.Name, err)
	}
	c.version = cm.ResourceVersion
	return &c, nil
}

func (s *Store) GetClaim(ctx context.Context, target string) (*Claim, error) {
	cm, err := s.cms().Get(ctx, s.claimName(target), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return decodeClaim(cm)
}

func (s *Store) ListClaims(ctx context.Context) ([]*Claim, error) {
	list, err := s.cms().List(ctx, metav1.ListOptions{LabelSelector: s.cfg.RoleLabel + "=claim"})
	if err != nil {
		return nil, err
	}
	var out []*Claim
	for i := range list.Items {
		c, err := decodeClaim(&list.Items[i])
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Target < out[j].Target })
	return out, nil
}

// Open is the compare and swap: it succeeds only if the claim object is exactly the one we read,
// or does not exist yet. Two deciders can never both open a run.
func (s *Store) Open(ctx context.Context, c *Claim, prev *Claim) error {
	if prev != nil && prev.State == ClaimOpen {
		return ErrConflict
	}
	body, err := json.Marshal(c)
	if err != nil {
		return err
	}
	name := s.claimName(c.Target)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{s.cfg.RoleLabel: "claim"}},
		Data:       map[string]string{"claim": string(body)},
	}
	var out *corev1.ConfigMap
	if prev == nil {
		out, err = s.cms().Create(ctx, cm, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			return ErrConflict
		}
	} else {
		cm.ResourceVersion = prev.version
		out, err = s.cms().Update(ctx, cm, metav1.UpdateOptions{})
		if apierrors.IsConflict(err) {
			return ErrConflict
		}
	}
	if err != nil {
		return err
	}
	c.version = out.ResourceVersion
	return nil
}

func (s *Store) Save(ctx context.Context, c *Claim) error {
	body, err := json.Marshal(c)
	if err != nil {
		return err
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: s.claimName(c.Target), ResourceVersion: c.version, Labels: map[string]string{s.cfg.RoleLabel: "claim"}},
		Data:       map[string]string{"claim": string(body)},
	}
	out, err := s.cms().Update(ctx, cm, metav1.UpdateOptions{})
	if apierrors.IsConflict(err) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	c.version = out.ResourceVersion
	return nil
}
