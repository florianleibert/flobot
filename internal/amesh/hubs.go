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
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// HubExecBudget bounds one `amesh hubs list` / `amesh models list` call.
const HubExecBudget = 8 * time.Second

// HubCacheTTL bounds how long a fetched hub/model list is reused so a roster
// render + a start do not each re-exec the amesh binary.
const HubCacheTTL = 15 * time.Second

// hubCache is the process-wide cache for the CLI-backed discovery lists.
var hubCache struct {
	sync.Mutex
	hubs    []Hub
	fetched time.Time
}

// svcCache is the process-wide cache for `amesh models list --json`. It is
// separate from hubCache so refreshing hubs never invalidates the model list
// (and vice versa) — the two lists share no data.
var svcCache struct {
	sync.Mutex
	models  []Service
	fetched time.Time
}

// Hub is one joined hub row from `amesh hubs list --json`.
type Hub struct {
	Name          string `json:"name"`
	URL           string `json:"url"`
	AdminURL      string `json:"admin_url,omitempty"`
	ControllerURL string `json:"controller_url,omitempty"`
	Fingerprint   string `json:"fingerprint,omitempty"`
	TLSPin        string `json:"tls_pin,omitempty"`
	Default       bool   `json:"default"`
	HubVersion    string `json:"hub_version,omitempty"`
	WGUp          bool   `json:"wg_up"`
	WGStatus      *WG    `json:"wg_status,omitempty"`
}

// WG is the WireGuard status embedded in a hub row.
type WG struct {
	Up       bool   `json:"up"`
	IP       string `json:"ip,omitempty"`
	RealIface string `json:"real_iface,omitempty"`
}

// Service is one model service row from `amesh models list --json`.
type Service struct {
	ID            string `json:"id"`
	ModelID       string `json:"model_id,omitempty"`
	Display       string `json:"display,omitempty"`
	Location      string `json:"location"` // "local" or a hub name
	State         string `json:"state"`    // "ready" | "stopped"
	Endpoint      string `json:"endpoint,omitempty"`
	Profile       string `json:"profile,omitempty"`
	ContextTokens int    `json:"context_tokens,omitempty"`
	Backend       string `json:"backend,omitempty"`
}

// ameshBin returns the amesh binary path: AMESH_BIN env, then PATH amesh.
func ameshBin() (string, error) {
	if p := strings.TrimSpace(getenv("AMESH_BIN")); p != "" {
		return p, nil
	}
	return exec.LookPath("amesh")
}

// runJSON runs `amesh <args...> --json` with a bounded budget and decodes the
// first JSON document on stdout. The amesh CLI emits no secrets in these list
// outputs (hub fingerprints and model service rows only).
func runJSON(ctx context.Context, out any, args ...string) error {
	bin, err := ameshBin()
	if err != nil {
		return fmt.Errorf("amesh binary not found (set AMESH_BIN): %w", err)
	}
	cctx, cancel := context.WithTimeout(ctx, HubExecBudget)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, append(args, "--json")...)
	raw, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("amesh %s: %w", strings.Join(args, " "), err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("amesh %s: decode: %w", strings.Join(args, " "), err)
	}
	return nil
}

// Hubs returns the joined hubs from `amesh hubs list --json`, cached for
// HubCacheTTL. An amesh binary that is missing or a hub that is unreachable
// is an error; an empty list is a valid state.
func Hubs(ctx context.Context) ([]Hub, error) {
	hubCache.Lock()
	if time.Since(hubCache.fetched) < HubCacheTTL {
		hubs := hubCache.hubs
		hubCache.Unlock()
		return hubs, nil
	}
	hubCache.Unlock()
	var hubs []Hub
	if err := runJSON(ctx, &hubs, "hubs", "list"); err != nil {
		return nil, err
	}
	hubCache.Lock()
	hubCache.hubs = hubs
	hubCache.fetched = time.Now()
	hubCache.Unlock()
	return hubs, nil
}

// Services returns the model services from `amesh models list --json`,
// cached for HubCacheTTL. This is the discovery surface the dock app uses to
// show every local + enrolled-hub model and its ready/stopped state.
func Services(ctx context.Context) ([]Service, error) {
	svcCache.Lock()
	if time.Since(svcCache.fetched) < HubCacheTTL {
		models := svcCache.models
		svcCache.Unlock()
		return models, nil
	}
	svcCache.Unlock()
	var body struct {
		Models []Service `json:"models"`
	}
	if err := runJSON(ctx, &body, "models", "list"); err != nil {
		return nil, err
	}
	if body.Models == nil {
		return []Service{}, nil
	}
	svcCache.Lock()
	svcCache.models = body.Models
	svcCache.fetched = time.Now()
	svcCache.Unlock()
	return body.Models, nil
}

// ReadyServices filters services to the ready ones (local + hub).
func ReadyServices(services []Service) []Service {
	var out []Service
	for _, s := range services {
		if strings.EqualFold(strings.TrimSpace(s.State), "ready") {
			out = append(out, s)
		}
	}
	return out
}
