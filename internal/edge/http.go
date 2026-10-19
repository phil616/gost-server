package edge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/altasci/altascitunnel/internal/model"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

func (e *Engine) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		snap := e.Snapshot()
		providers, visitors := 0, 0
		for _, s := range snap.Sessions {
			if s.Kind == "provider" && s.Connected {
				providers++
			}
			if s.Kind == "visitor" {
				visitors++
			}
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprintf(w, "# TYPE altasci_provider_connections gauge\naltasci_provider_connections %d\n# TYPE altasci_visitor_connections gauge\naltasci_visitor_connections %d\n# TYPE altasci_auth_rejected_total counter\naltasci_auth_rejected_total %d\n# TYPE altasci_relay_input_bytes_total counter\naltasci_relay_input_bytes_total %d\n# TYPE altasci_relay_output_bytes_total counter\naltasci_relay_output_bytes_total %d\naltasci_policy_revision %d\n", providers, visitors, e.authRejected.Load(), e.inputBytes.Load(), e.outputBytes.Load(), snap.Revision)
	})
	m.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	m.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		if e.Policy().Revision < 1 {
			http.Error(w, "no policy", 503)
			return
		}
		w.Write([]byte("ok"))
	})
	m.HandleFunc("GET /internal/v1/policy", func(w http.ResponseWriter, r *http.Request) { respond(w, e.Policy()) })
	m.HandleFunc("PUT /internal/v1/policy", func(w http.ResponseWriter, r *http.Request) {
		var p model.Policy
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<20)).Decode(&p); err != nil {
			http.Error(w, "invalid JSON", 400)
			return
		}
		if err := e.Apply(p); err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		respond(w, e.Policy())
	})
	m.HandleFunc("GET /internal/v1/runtime", func(w http.ResponseWriter, r *http.Request) { respond(w, e.Snapshot()) })
	m.HandleFunc("GET /internal/v1/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		f, ok := w.(http.Flusher)
		if !ok {
			return
		}
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		var last uint64 = ^uint64(0)
		for {
			snap := e.Snapshot()
			if snap.Sequence != last {
				if json.NewEncoder(w).Encode(snap) != nil {
					return
				}
				f.Flush()
				last = snap.Sequence
			}
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
			}
		}
	})
	return m
}
func respond(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

type Client struct {
	HTTP *http.Client
	Base string
}

func NewClient(endpoint string) *Client {
	tr := &http.Transport{}
	base := endpoint
	if strings.HasPrefix(endpoint, "unix://") {
		socket := strings.TrimPrefix(endpoint, "unix://")
		tr.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		}
		base = "http://unix"
	}
	return &Client{HTTP: &http.Client{Transport: tr, Timeout: 5 * time.Second}, Base: strings.TrimRight(base, "/")}
}
func (c *Client) request(ctx context.Context, method, path string, body io.Reader, out any) error {
	req, e := http.NewRequestWithContext(ctx, method, c.Base+path, body)
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	res, e := c.HTTP.Do(req)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("edge HTTP %d: %s", res.StatusCode, b)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}
func (c *Client) Apply(ctx context.Context, p model.Policy) error {
	b, e := json.Marshal(p)
	if e != nil {
		return e
	}
	var ack model.Policy
	if e = c.request(ctx, "PUT", "/internal/v1/policy", strings.NewReader(string(b)), &ack); e != nil {
		return e
	}
	if ack.Revision != p.Revision || ack.Hash != p.Hash {
		return errors.New("invalid edge policy acknowledgement")
	}
	return nil
}
func (c *Client) Snapshot(ctx context.Context) (model.RuntimeSnapshot, error) {
	var s model.RuntimeSnapshot
	e := c.request(ctx, "GET", "/internal/v1/runtime", nil, &s)
	return s, e
}
