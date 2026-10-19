package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/netip"
	"sort"
	"time"
)

type Client struct {
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	PublicKey        string         `json:"public_key"`
	OS               string         `json:"os"`
	Arch             string         `json:"arch"`
	Version          string         `json:"version"`
	Revoked          bool           `json:"revoked"`
	Revision         int64          `json:"revision"`
	ReceivedRevision int64          `json:"received_revision"`
	AppliedRevision  int64          `json:"applied_revision"`
	LastSeen         time.Time      `json:"last_seen"`
	Runtime          []TunnelStatus `json:"runtime"`
	CreatedAt        time.Time      `json:"created_at"`
}
type Tunnel struct {
	ID         string    `json:"id"`
	ClientID   string    `json:"client_id"`
	Name       string    `json:"name"`
	Protocol   string    `json:"protocol"`
	TargetIP   string    `json:"target_ip"`
	TargetPort int       `json:"target_port"`
	Enabled    bool      `json:"enabled"`
	Deleted    bool      `json:"deleted"`
	Generation int64     `json:"generation"`
	Version    int64     `json:"version"`
	CreatedAt  time.Time `json:"created_at"`
}

func (t Tunnel) Validate() error {
	ip, e := netip.ParseAddr(t.TargetIP)
	if e != nil || !ip.Is4() || ip.IsUnspecified() || ip.IsMulticast() || t.TargetIP == "255.255.255.255" {
		return errors.New("target_ip must be an IPv4 unicast address")
	}
	if t.Protocol != "tcp" && t.Protocol != "udp" {
		return errors.New("protocol must be tcp or udp")
	}
	if t.TargetPort < 1 || t.TargetPort > 65535 {
		return errors.New("target_port must be 1..65535")
	}
	return nil
}

type Credential struct {
	ID         string     `json:"id"`
	TunnelID   string     `json:"tunnel_id"`
	ClientID   string     `json:"client_id"`
	Kind       string     `json:"kind"`
	Username   string     `json:"username"`
	Digest     string     `json:"digest"`
	Ciphertext string     `json:"ciphertext"`
	Generation int64      `json:"generation"`
	Revoked    bool       `json:"revoked"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}
type Enrollment struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Digest    string    `json:"digest"`
	ExpiresAt time.Time `json:"expires_at"`
	Revoked   bool      `json:"revoked"`
	RequestID string    `json:"request_id"`
	PublicKey string    `json:"public_key"`
	ClientID  string    `json:"client_id"`
}
type Admin struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"password_hash"`
}
type AdminSession struct {
	ID        string    `json:"id"`
	AdminID   string    `json:"admin_id"`
	CSRF      string    `json:"csrf"`
	ExpiresAt time.Time `json:"expires_at"`
}
type Audit struct {
	ID       string    `json:"id"`
	Actor    string    `json:"actor"`
	Action   string    `json:"action"`
	Resource string    `json:"resource"`
	At       time.Time `json:"at"`
}
type Operation struct {
	ID             string          `json:"id"`
	Resource       string          `json:"resource"`
	PolicyRevision int64           `json:"policy_revision"`
	State          string          `json:"state"`
	Error          string          `json:"error,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	RequestHash    string          `json:"request_hash,omitempty"`
	Result         json.RawMessage `json:"result,omitempty"`
}
type ConfigRecord struct {
	ID         string    `json:"id"`
	ClientID   string    `json:"client_id"`
	Revision   int64     `json:"revision"`
	Ciphertext string    `json:"ciphertext"`
	Hash       string    `json:"hash"`
	CreatedAt  time.Time `json:"created_at"`
}
type Meta struct {
	PolicyRevision int64 `json:"policy_revision"`
}
type State struct {
	Meta        Meta                    `json:"meta"`
	Clients     map[string]Client       `json:"clients"`
	Tunnels     map[string]Tunnel       `json:"tunnels"`
	Credentials map[string]Credential   `json:"credentials"`
	Enrollments map[string]Enrollment   `json:"enrollments"`
	Admins      map[string]Admin        `json:"admins"`
	Sessions    map[string]AdminSession `json:"sessions"`
	Audits      map[string]Audit        `json:"audits"`
	Operations  map[string]Operation    `json:"operations"`
	Configs     map[string]ConfigRecord `json:"configs"`
	Outbox      map[string]Operation    `json:"outbox"`
	Policies    map[string]Policy       `json:"policies"`
}

func NewState() State {
	return State{Clients: map[string]Client{}, Tunnels: map[string]Tunnel{}, Credentials: map[string]Credential{}, Enrollments: map[string]Enrollment{}, Admins: map[string]Admin{}, Sessions: map[string]AdminSession{}, Audits: map[string]Audit{}, Operations: map[string]Operation{}, Configs: map[string]ConfigRecord{}, Outbox: map[string]Operation{}, Policies: map[string]Policy{}}
}

type Secret struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Secret   string `json:"secret"`
}
type TunnelSpec struct {
	Tunnel
	Provider *Secret `json:"provider_credential,omitempty"`
}
type ClientConfig struct {
	SchemaVersion int          `json:"schema_version"`
	ClientID      string       `json:"client_id"`
	Revision      int64        `json:"revision"`
	Hash          string       `json:"config_hash"`
	Tunnels       []TunnelSpec `json:"tunnels"`
}

func (c ClientConfig) HashValue() string {
	c.Hash = ""
	sort.Slice(c.Tunnels, func(i, j int) bool { return c.Tunnels[i].ID < c.Tunnels[j].ID })
	b, _ := json.Marshal(c)
	return Digest(b)
}

func (c ClientConfig) Validate() error {
	if c.SchemaVersion != 1 || c.ClientID == "" || c.Revision < 1 || c.Hash != c.HashValue() {
		return errors.New("invalid config schema/identity/revision/hash")
	}
	seen := map[string]bool{}
	for _, s := range c.Tunnels {
		if s.ID == "" || seen[s.ID] || s.Generation < 1 || s.ClientID != c.ClientID {
			return errors.New("invalid or duplicate tunnel identity")
		}
		seen[s.ID] = true
		if e := s.Validate(); e != nil {
			return e
		}
		if s.Enabled && !s.Deleted && (s.Provider == nil || s.Provider.Username == "" || s.Provider.Secret == "") {
			return errors.New("enabled tunnel requires Provider credential")
		}
	}
	return nil
}
func Digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

type Policy struct {
	Revision    int64        `json:"revision"`
	Tunnels     []Tunnel     `json:"tunnels"`
	Credentials []Credential `json:"credentials"`
	Hash        string       `json:"hash"`
}

func (p Policy) HashValue() string { p.Hash = ""; b, _ := json.Marshal(p); return Digest(b) }

type TunnelStatus struct {
	TunnelID    string    `json:"tunnel_id"`
	Generation  int64     `json:"generation"`
	State       string    `json:"state"`
	Error       string    `json:"error,omitempty"`
	NextRetry   time.Time `json:"next_retry_at,omitempty"`
	RetryCount  int       `json:"retry_count"`
	SessionID   string    `json:"session_id,omitempty"`
	InputBytes  uint64    `json:"input_bytes"`
	OutputBytes uint64    `json:"output_bytes"`
	UpdatedAt   time.Time `json:"updated_at"`
}
type RuntimeSnapshot struct {
	Epoch      string        `json:"epoch"`
	Sequence   uint64        `json:"sequence"`
	Revision   int64         `json:"revision"`
	PolicyHash string        `json:"policy_hash"`
	Sessions   []SessionInfo `json:"sessions"`
	UpdatedAt  time.Time     `json:"updated_at"`
}
type SessionInfo struct {
	ID           string    `json:"id"`
	CredentialID string    `json:"credential_id"`
	TunnelID     string    `json:"tunnel_id"`
	Generation   int64     `json:"generation"`
	Kind         string    `json:"kind"`
	Connected    bool      `json:"connected"`
	OpenedAt     time.Time `json:"opened_at"`
	InputBytes   uint64    `json:"input_bytes"`
	OutputBytes  uint64    `json:"output_bytes"`
}
type Message struct {
	V       int             `json:"v"`
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	Payload json.RawMessage `json:"payload"`
}
