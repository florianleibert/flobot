// Package amesh is the read-only client for the local amesh-web mesh model
// catalog. flobot never imports the amesh module; it speaks HTTP to the
// loopback amesh-web surface (GET /api/v2/mesh/models) and turns every ready
// mesh model into a selectable flobot route with id "mesh/<model-id>".
//
// The mesh catalog carries no credentials: mesh nodes serve on the private
// network and the catalog document is signed by the hub. flobot only reads
// id, display, base_url, and health.
package amesh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// DefaultWebAddr is the amesh-web loopback listener (defaultWebListen in
// amesh-web main.go). Overridable with AMESH_WEB_ADDR.
const DefaultWebAddr = "127.0.0.1:18765"

// FetchBudget bounds one catalog fetch.
const FetchBudget = 8 * time.Second

// CacheTTL bounds how long a fetched catalog is reused. amesh-web itself
// caches its signed target discovery for 60s; flobot reuses the document
// briefly so a roster render + a run start do not each re-sign and
// re-fetch the whole catalog.
const CacheTTL = 15 * time.Second

// cache is the process-wide catalog cache. One entry: the catalog is small
// and per-address overrides (tests) bypass it via Client.Addr.
var cache struct {
	sync.Mutex
	addr     string
	models   []Model
	fetched  time.Time
	fetching bool
}

// Model is one row of the amesh mesh model catalog. Only the fields flobot
// uses; unknown fields are ignored.
type Model struct {
	ID            string `json:"id"`
	Display       string `json:"display,omitempty"`
	BaseURL       string `json:"base_url,omitempty"`
	NodeID        string `json:"node_id,omitempty"`
	Health        string `json:"health"`
	Where         string `json:"where"`
	ContextTokens int    `json:"context_tokens,omitempty"`
	Importable    bool   `json:"importable"`
	BlockedReason string `json:"import_blocked_reason,omitempty"`
	StatusText    string `json:"status_text,omitempty"`
}

// RouteID is the flobot route id for a mesh model: "mesh/<model-id>".
func RouteID(modelID string) string { return "mesh/" + strings.TrimSpace(modelID) }

// ModelIDFromRoute returns the mesh model id for a mesh/<id> route, or "".
func ModelIDFromRoute(route string) string {
	r := strings.TrimSpace(route)
	if !strings.HasPrefix(r, "mesh/") {
		return ""
	}
	return strings.TrimPrefix(r, "mesh/")
}

// IsMeshRoute reports whether route refers to a mesh model.
func IsMeshRoute(route string) bool { return ModelIDFromRoute(route) != "" }

// Client fetches the mesh model catalog from amesh-web.
type Client struct {
	// Addr is the amesh-web loopback address. Empty uses DefaultWebAddr.
	Addr string
	// HTTP is the client. Nil uses a loopback-budgeted default.
	HTTP *http.Client
}

// WebAddr resolves the amesh-web address: AMESH_WEB_ADDR, then DefaultWebAddr.
func WebAddr() string {
	if a := strings.TrimSpace(getenv("AMESH_WEB_ADDR")); a != "" {
		return a
	}
	return DefaultWebAddr
}

var getenv = os.Getenv

// Models fetches the ready mesh models. A hub that is joined but unreachable
// is returned as an error with the reason; an empty list is a valid state.
// Results are cached for CacheTTL; an explicit Client.Addr bypasses the cache.
func (c *Client) Models(ctx context.Context) ([]Model, error) {
	addr := c.addr()
	if c != nil && strings.TrimSpace(c.Addr) != "" {
		return c.fetch(ctx, addr)
	}
	cache.Lock()
	if cache.addr == addr && time.Since(cache.fetched) < CacheTTL {
		models := cache.models
		cache.Unlock()
		return models, nil
	}
	cache.Unlock()
	models, err := c.fetch(ctx, addr)
	if err != nil {
		return nil, err
	}
	cache.Lock()
	cache.addr = addr
	cache.models = models
	cache.fetched = time.Now()
	cache.Unlock()
	return models, nil
}

// ErrUnreachable wraps any amesh-web transport failure so callers can
// distinguish "mesh backend down" (502) from bad input (400).
var ErrUnreachable = errors.New("amesh-web unreachable")

func (c *Client) fetch(ctx context.Context, addr string) ([]Model, error) {
	url := "http://" + addr + "/api/v2/mesh/models"
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: FetchBudget}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrUnreachable, addr, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("amesh-web %s: status %d", addr, resp.StatusCode)
	}
	var body struct {
		Schema     string  `json:"schema"`
		Hub        string  `json:"hub,omitempty"`
		Models     []Model `json:"models"`
		Reason     string  `json:"reason,omitempty"`
		ReasonCode string  `json:"reason_code,omitempty"`
	}
	// Bound the body: the catalog is a signed document, not untrusted input,
	// but a loopback service should never be able to OOM this process.
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&body); err != nil {
		return nil, fmt.Errorf("amesh-web %s: decode: %w", addr, err)
	}
	if body.Schema != "" && body.Schema != "amesh.mesh_model_catalog.v1" {
		return nil, fmt.Errorf("amesh-web %s: unexpected schema %q", addr, body.Schema)
	}
	return body.Models, nil
}

func (c *Client) addr() string {
	if c != nil && strings.TrimSpace(c.Addr) != "" {
		return strings.TrimSpace(c.Addr)
	}
	return WebAddr()
}

// Ready filters models to the ready/importable ones. The live amesh-web
// catalog reports health "ok" with status_text "ready"; older catalogs
// used health "ready". Both are accepted.
func Ready(models []Model) []Model {
	var out []Model
	for _, m := range models {
		if m.Importable && Healthy(m.Health) {
			out = append(out, m)
		}
	}
	return out
}

// Healthy reports whether a catalog health value means usable. The live
// catalog uses "ok"; "ready" is kept for compatibility.
func Healthy(h string) bool {
	h = strings.ToLower(strings.TrimSpace(h))
	return h == "ok" || h == "ready"
}

// Find returns the model with the given mesh model id.
func Find(models []Model, id string) (Model, bool) {
	id = strings.TrimSpace(id)
	for _, m := range models {
		if m.ID == id {
			return m, true
		}
	}
	return Model{}, false
}
