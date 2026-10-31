package transport

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/sassendes/bedrock/failover-controller/observe"
)

const Path = "/exchange"

// HTTP carries envelopes over mutual TLS. TLS only proves a caller is one of the observers;
// who said what is proven by the ed25519 signatures inside the envelope.
type HTTP struct {
	client *http.Client
	mu     sync.RWMutex
	addrs  map[string]string
}

func NewHTTP(clientTLS *tls.Config, timeout time.Duration) *HTTP {
	return &HTTP{
		client: &http.Client{Timeout: timeout, Transport: &http.Transport{TLSClientConfig: clientTLS, MaxIdleConnsPerHost: 2}},
		addrs:  map[string]string{},
	}
}

func (h *HTTP) SetAddrs(addrs map[string]string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.addrs = addrs
}

func (h *HTTP) Send(ctx context.Context, _, to string, env observe.Envelope) error {
	h.mu.RLock()
	addr, ok := h.addrs[to]
	h.mu.RUnlock()
	if !ok {
		return fmt.Errorf("no address for %s", to)
	}
	body, err := json.Marshal(env)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+addr+Path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("%s answered %s", to, resp.Status)
	}
	return nil
}

type Handled interface {
	Handle(context.Context, observe.Envelope)
}

type Handler struct {
	node    Handled
	slots   chan struct{}
	maxBody int64
	timeout time.Duration
}

func NewHandler(node Handled, inflight int, maxBody int64, timeout time.Duration) *Handler {
	return &Handler{node: node, slots: make(chan struct{}, inflight), maxBody: maxBody, timeout: timeout}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != Path {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var env observe.Envelope
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, h.maxBody)).Decode(&env); err != nil {
		http.Error(w, "bad envelope", http.StatusBadRequest)
		return
	}
	if (env.Request == nil) == (env.Answer == nil) {
		http.Error(w, "exactly one of request or answer", http.StatusBadRequest)
		return
	}
	select {
	case h.slots <- struct{}{}:
	default:
		http.Error(w, "busy", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	go func() {
		defer func() { <-h.slots }()
		ctx, cancel := context.WithTimeout(context.Background(), h.timeout)
		defer cancel()
		h.node.Handle(ctx, env)
	}()
}

func LoadTLS(certFile, keyFile, caFile, serverName string) (server, client *tls.Config, err error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, nil, fmt.Errorf("load keypair: %w", err)
	}
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return nil, nil, fmt.Errorf("read ca: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, nil, errors.New("ca file holds no certificate")
	}
	server = &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}
	client = &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
		ServerName:   serverName,
	}
	return server, client, nil
}
