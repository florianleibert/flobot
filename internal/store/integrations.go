// integrations.go persists external-service integrations (Gmail, Slack,
// GitHub, generic webhooks) and the webhook secrets that let them fire
// flobot routines.
//
// The file is written 0600 (directory 0700) and never contains an Owner
// field or anything that looks like a secret — except the integration's own
// webhook secret, which is what the file exists to hold. The same
// assertNoLeaks check the roster uses would reject a secret on write, so
// this store applies its own leak policy: the secret field is allowed, and
// everything else is still checked.
package store

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Integration kinds. The kind decides how an event fires a routine, which
// payload shape is expected, and which authentication scheme the caller must
// use. Every kind is an *inbound* source: something outside flobot posts an
// event and the bound routines fire.
const (
	// IntegrationWebhook is a generic authenticated webhook. Any service
	// that can POST a JSON body can fire a routine.
	IntegrationWebhook = "webhook"
	// IntegrationGmail is a Gmail mailbox watched via the Google Apps Script
	// companion. It fires on new mail matching a label/query.
	IntegrationGmail = "gmail"
	// IntegrationSlack is a Slack workspace event (message, reaction, app
	// mention) delivered to the webhook URL.
	IntegrationSlack = "slack"
	// IntegrationGitHub is a GitHub repository event (issue, pull request,
	// release, check run) delivered to the webhook URL.
	IntegrationGitHub = "github"
	// IntegrationTelegram is a Telegram bot update delivered to the
	// webhook URL.
	IntegrationTelegram = "telegram"
	// IntegrationDiscord is a Discord interaction delivered to the webhook
	// URL.
	IntegrationDiscord = "discord"
	// IntegrationLinear is a Linear issue event delivered to the webhook
	// URL.
	IntegrationLinear = "linear"
	// IntegrationStripe is a Stripe account event (payment, refund,
	// dispute) delivered to the webhook URL.
	IntegrationStripe = "stripe"
)

// Authentication schemes for the webhook receiver. The scheme decides how a
// caller proves it may fire the integration. Every scheme is checked in
// constant time.
const (
	// AuthBearer expects "Authorization: Bearer <secret>". The default:
	// curl, n8n, Zapier, Make and most webhook tools can set headers.
	AuthBearer = "bearer"
	// AuthQuery expects the secret in the ?key=<secret> query parameter.
	// For providers that POST to a fixed URL and cannot set headers
	// (Slack incoming webhooks, Zapier catch hooks, IFTTT).
	AuthQuery = "query"
	// AuthHMAC expects an "X-Hub-Signature-256: sha256=<hex>" header
	// holding the HMAC-SHA256 of the raw body. The GitHub scheme.
	AuthHMAC = "hmac"
	// AuthSlack expects Slack's "X-Slack-Signature: v0=<hex>" header:
	// HMAC-SHA256 over "v0:<timestamp>:<body>", with a timestamp
	// tolerance to blunt replay attacks.
	AuthSlack = "slack"
	// AuthStripe expects a "Stripe-Signature: t=<ts>,v1=<hex>" header:
	// HMAC-SHA256 over "<ts>.<body>", with a timestamp tolerance.
	AuthStripe = "stripe"
	// AuthStandard expects the standard-webhooks scheme:
	// "webhook-signature: v1,<base64>" over "<id>.<timestamp>.<body>",
	// with the secret in the whsec_ form.
	AuthStandard = "standard"
)

// AuthModes is every supported authentication scheme.
var AuthModes = []string{AuthBearer, AuthQuery, AuthHMAC, AuthSlack, AuthStripe, AuthStandard}

// ValidAuthMode reports whether mode is a supported authentication scheme.
func ValidAuthMode(mode string) bool {
	for _, m := range AuthModes {
		if mode == m {
			return true
		}
	}
	return false
}

// IntegrationKinds is every supported integration kind.
var IntegrationKinds = []string{
	IntegrationWebhook, IntegrationGmail, IntegrationSlack, IntegrationGitHub,
	IntegrationTelegram, IntegrationDiscord, IntegrationLinear, IntegrationStripe,
}

// ValidIntegrationKind reports whether kind is a supported integration kind.
func ValidIntegrationKind(kind string) bool {
	for _, k := range IntegrationKinds {
		if kind == k {
			return true
		}
	}
	return false
}

const (
	// maxIntegrations caps the persisted file. Each collection keeps the
	// newest entries and evicts the oldest, so a long-lived app cannot grow
	// the file without limit.
	maxIntegrations = 100
	// maxEvents caps the per-integration event history kept on disk.
	maxIntegrationEvents = 200
)

// Integration is one external-service connection. The webhook secret is the
// only secret on the type; it is what authenticates a fire request.
type Integration struct {
	ID string `json:"id"`
	// Kind is one of IntegrationKinds. It decides the payload shape and
	// the default authentication scheme.
	Kind string `json:"kind"`
	// AuthMode is the authentication scheme a caller must use: AuthBearer,
	// AuthQuery, AuthHMAC, AuthSlack, AuthStripe or AuthStandard. Empty
	// means AuthBearer.
	AuthMode string `json:"auth_mode,omitempty"`
	// Name is the display name (e.g. "work inbox").
	Name string `json:"name"`
	// Detail is a kind-specific human description (e.g. the Gmail label or
	// query the companion watches).
	Detail string `json:"detail,omitempty"`
	// Secret is the bearer token a caller must present to fire this
	// integration. It is generated at creation and shown exactly once.
	Secret string `json:"secret"`
	// CreatedAt is an RFC3339 timestamp.
	CreatedAt string `json:"created_at"`
}

// IntegrationEvent is one recorded fire of an integration: what fired it and
// what routine (if any) it triggered. It is history, never a secret.
type IntegrationEvent struct {
	ID string `json:"id"`
	// IntegrationID is the integration the event belongs to. Every event
	// carries it: without it an event that fired no routine (a rejected
	// credential, an ignored event) could not be attributed to the
	// integration it came from.
	IntegrationID string `json:"integration_id,omitempty"`
	RoutineID     string `json:"routine_id,omitempty"`
	RunID         string `json:"run_id,omitempty"`
	Status        string `json:"status"`
	Note          string `json:"note,omitempty"`
	CreatedAt     string `json:"created_at"`
}

// integrationsFile is the on-disk shape.
type integrationsFile struct {
	Integrations []Integration      `json:"integrations"`
	Events       []IntegrationEvent `json:"events"`
}

// IntegrationStore persists integrations and their fire history to
// ~/.flobot/integrations.json.
//
// It is a separate file from ~/.flobot/state.json on purpose. state.json's
// contract is "the roster only — runs and events are never persisted", and
// its assertNoLeaks guard refuses any file containing a secret. Integrations
// are the opposite: their whole reason to exist is to hold webhook secrets,
// so they live in their own file with their own leak policy (the secret
// field is allowed, everything else is checked).
type IntegrationStore struct {
	mu           sync.Mutex
	integrations map[string]Integration
	events       []IntegrationEvent

	// path is the integrations file location; empty means the default
	// ~/.flobot/integrations.json.
	path string
}

// NewIntegrationStore returns an empty store at the default path.
func NewIntegrationStore() *IntegrationStore {
	return &IntegrationStore{
		integrations: make(map[string]Integration),
	}
}

// Path returns the integrations file path: $HOME/.flobot/integrations.json,
// or the override set via SetPath. Under go test (FLOBOT_TEST_STATE set), an
// unset path resolves to a temp dir so tests never touch the real home.
func (s *IntegrationStore) Path() string {
	if s.path != "" {
		return s.path
	}
	if p := os.Getenv("FLOBOT_TEST_STATE"); p != "" {
		return filepath.Join(p, "integrations.json")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".flobot", "integrations.json")
	}
	return filepath.Join(home, ".flobot", "integrations.json")
}

// SetPath overrides the integrations file location (tests).
func (s *IntegrationStore) SetPath(p string) {
	s.path = p
}

// Load reads the integrations file. A missing file is not an error: the
// store starts empty. A corrupted file is handled gracefully — the error is
// returned for the caller to log and the store starts empty instead of
// crashing the app.
func (s *IntegrationStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	raw, err := os.ReadFile(s.Path())
	switch {
	case err == nil:
		var f integrationsFile
		if jerr := json.Unmarshal(raw, &f); jerr != nil {
			s.applyDefaults()
			return fmt.Errorf("store: integrations file %s is corrupted: %w", s.Path(), jerr)
		}
		s.apply(f)
		return nil
	case errors.Is(err, os.ErrNotExist):
		s.applyDefaults()
		return nil
	default:
		s.applyDefaults()
		return fmt.Errorf("store: read integrations: %w", err)
	}
}

func (s *IntegrationStore) apply(f integrationsFile) {
	s.integrations = make(map[string]Integration, len(f.Integrations))
	for _, in := range f.Integrations {
		s.integrations[in.ID] = in
	}
	s.events = append([]IntegrationEvent(nil), f.Events...)
}

func (s *IntegrationStore) applyDefaults() {
	s.integrations = make(map[string]Integration)
	s.events = nil
}

// Save writes the integrations file. The directory is created 0700 and the
// file 0600, written via a temp file and a rename so a crash mid-write
// cannot leave a half-written file behind.
func (s *IntegrationStore) Save() error {
	s.mu.Lock()
	f := integrationsFile{
		Integrations: make([]Integration, 0, len(s.integrations)),
		Events:       append([]IntegrationEvent(nil), s.events...),
	}
	for _, in := range s.integrations {
		f.Integrations = append(f.Integrations, in)
	}
	s.mu.Unlock()

	sort.Slice(f.Integrations, func(i, j int) bool {
		if f.Integrations[i].CreatedAt != f.Integrations[j].CreatedAt {
			return f.Integrations[i].CreatedAt < f.Integrations[j].CreatedAt
		}
		return f.Integrations[i].ID < f.Integrations[j].ID
	})
	sort.Slice(f.Events, func(i, j int) bool { return f.Events[i].CreatedAt < f.Events[j].CreatedAt })
	f.Integrations = evictOldest(f.Integrations, maxIntegrations, func(x Integration) string { return x.CreatedAt })
	f.Events = evictOldest(f.Events, maxIntegrationEvents, func(x IntegrationEvent) string { return x.CreatedAt })

	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("store: marshal integrations: %w", err)
	}
	if err := assertNoLeaksExcept(raw, "secret"); err != nil {
		return err
	}

	path := s.Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("store: mkdir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("store: write integrations: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("store: rename integrations: %w", err)
	}
	return nil
}

// assertNoLeaksExcept is assertNoLeaks with one allowed token: the webhook
// secret field of an integration. Everything else that looks like a secret
// still fails the write.
func assertNoLeaksExcept(raw []byte, allow string) error {
	low := strings.ToLower(string(raw))
	for _, leak := range []string{`"owner"`, "llm_", "api_key", "secret"} {
		if leak == allow {
			continue
		}
		if strings.Contains(low, leak) {
			return fmt.Errorf("store: refusing to persist state containing %q", leak)
		}
	}
	return nil
}

// CreateIntegration stores a new integration with a fresh random webhook
// secret. The secret is returned exactly once; callers must show it to the
// user and never log it. An empty authMode means AuthBearer.
func (s *IntegrationStore) CreateIntegration(kind, name, detail, authMode string) (Integration, error) {
	if !ValidIntegrationKind(kind) {
		return Integration{}, fmt.Errorf("unknown integration kind %q", kind)
	}
	authMode = strings.TrimSpace(authMode)
	if authMode == "" {
		authMode = AuthBearer
	}
	if !ValidAuthMode(authMode) {
		return Integration{}, fmt.Errorf("unknown auth mode %q", authMode)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Integration{}, fmt.Errorf("name required")
	}
	in := Integration{
		ID:        newID("int"),
		Kind:      kind,
		AuthMode:  authMode,
		Name:      name,
		Detail:    strings.TrimSpace(detail),
		Secret:    newSecret(authMode),
		CreatedAt: nowRFC3339(),
	}
	s.mu.Lock()
	s.integrations[in.ID] = in
	s.mu.Unlock()
	return in, s.Save()
}

// Integration returns one integration by id.
func (s *IntegrationStore) Integration(id string) (Integration, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	in, ok := s.integrations[id]
	return in, ok
}

// ListIntegrations returns every integration, oldest first.
func (s *IntegrationStore) ListIntegrations() []Integration {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Integration, 0, len(s.integrations))
	for _, in := range s.integrations {
		out = append(out, in)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt < out[j].CreatedAt
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// DeleteIntegration removes an integration. It reports whether one was
// removed.
func (s *IntegrationStore) DeleteIntegration(id string) bool {
	s.mu.Lock()
	_, ok := s.integrations[id]
	delete(s.integrations, id)
	s.mu.Unlock()
	if !ok {
		return false
	}
	_ = s.Save()
	return true
}

// Authenticate reports whether secret matches the integration's webhook
// secret. The comparison is constant-time so a timing probe cannot learn a
// secret byte by byte.
func (s *IntegrationStore) Authenticate(id, secret string) bool {
	in, ok := s.Integration(id)
	if !ok || in.Secret == "" || secret == "" {
		return false
	}
	return subtleEqual(in.Secret, secret)
}

// RecordEvent appends one fire event to the integration's history.
func (s *IntegrationStore) RecordEvent(in Integration, ev IntegrationEvent) error {
	if ev.ID == "" {
		ev.ID = newID("intev")
	}
	if ev.IntegrationID == "" {
		ev.IntegrationID = in.ID
	}
	if ev.CreatedAt == "" {
		ev.CreatedAt = nowRFC3339()
	}
	if ev.Status == "" {
		ev.Status = "fired"
	}
	s.mu.Lock()
	s.events = append(s.events, ev)
	if len(s.events) > maxIntegrationEvents {
		s.events = append(s.events[:0], s.events[len(s.events)-maxIntegrationEvents:]...)
	}
	s.mu.Unlock()
	return s.Save()
}

// ListEvents returns every recorded integration fire, oldest first.
func (s *IntegrationStore) ListEvents() []IntegrationEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]IntegrationEvent(nil), s.events...)
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out
}

// RotateSecret replaces an integration's webhook secret with a fresh one and
// returns the integration. The new secret is returned exactly once, like the
// one handed out at creation: rotating invalidates the old secret
// immediately, so the caller has to re-share it with the provider.
func (s *IntegrationStore) RotateSecret(id string) (Integration, bool) {
	s.mu.Lock()
	in, ok := s.integrations[id]
	if !ok {
		s.mu.Unlock()
		return Integration{}, false
	}
	in.Secret = newSecret(in.AuthMode)
	s.integrations[id] = in
	s.mu.Unlock()
	if err := s.Save(); err != nil {
		return in, true
	}
	return in, true
}

// SecretHint returns a short, non-reversible hint of a secret: enough for a
// human to tell two secrets apart, never enough to use one.
func SecretHint(secret string) string {
	if len(secret) <= 6 {
		return "••••"
	}
	return "••••" + secret[len(secret)-4:]
}

// newSecret generates a webhook secret for the given auth mode. The
// standard-webhooks scheme expects a whsec_-prefixed base64 secret; every
// other scheme gets a 48 character hex string.
func newSecret(authMode string) string {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return newID("sec")
	}
	if authMode == AuthStandard {
		return "whsec_" + base64.StdEncoding.EncodeToString(b[:])
	}
	return hex.EncodeToString(b[:])
}

func subtleEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}
