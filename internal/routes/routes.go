// Package routes is the advertised model combination for standalone flobot.
// JSON never includes secrets or base URLs; pins live in flocode config.
package routes

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/florianleibert/flobot/internal/amesh"
)

// Route is a documented model combination. Secrets never appear here.
type Route struct {
	ID      string `json:"id"`
	Role    string `json:"role"`
	Source  string `json:"source"`
	Model   string `json:"model"`
	Purpose string `json:"purpose"`
}

// ErrLocalUnpinned is returned when route id "local" has no pin yet.
var ErrLocalUnpinned = errors.New("local route is unpinned")

// localPin holds the flocode pin for the local MLX route, guarded so HTTP
// handler goroutines can pin/unpin it concurrently with route resolution.
var localPin struct {
	sync.RWMutex
	pin string
}

// LocalPin returns the current pin for the "local" route ("" when unpinned).
func LocalPin() string {
	localPin.RLock()
	defer localPin.RUnlock()
	return localPin.pin
}

// SetLocalPin records the pin for the "local" route. An empty pin unpins it.
func SetLocalPin(pin string) {
	localPin.Lock()
	defer localPin.Unlock()
	localPin.pin = strings.TrimSpace(pin)
}

// IsLocal reports whether id is the "local" route.
func IsLocal(id string) bool { return strings.TrimSpace(id) == "local" }

// PinMap maps route ids to flocode --model pin names. local is omitted
// until a pin exists. Base URLs stay in flocode config, not this map.
var PinMap = map[string]string{
	"glm":  "glm-5.2",
	"qwen": "qwen-3.8",
	"muse": "muse-spark-1.3",
}

// DefaultRoutes is the documented combination: GLM orchestrates, Qwen
// codes, local MLX tools (unpinned), Muse Spark 1.2 plans.
func DefaultRoutes() []Route {
	return []Route{
		{
			ID:      "glm",
			Role:    "orchestrator",
			Source:  "amesh-mesh",
			Model:   "glm-5.2-fp8",
			Purpose: "Multi-agent planning, handoff, and long-horizon review.",
		},
		{
			ID:      "qwen",
			Role:    "coder",
			Source:  "amesh-qwen-node",
			Model:   "qwen3.8-27b-fp8",
			Purpose: "Repo-native coding on a working Qwen 3.8 node.",
		},
		{
			ID:      "local",
			Role:    "tools",
			Source:  "this-mac",
			Model:   "qwen3-coder-30b-a3b-mlx",
			Purpose: "Local tool calls. No mesh hop. Read-only system info by default.",
		},
		{
			ID:      "muse",
			Role:    "chief-of-staff",
			Source:  "meta-model-api",
			Model:   "muse-spark-1.3",
			Purpose: "1M-context planning. Key stays on this Mac; never in the browser.",
		},
	}
}

// DirectBaseURL is the OpenAI-compatible endpoint direct/ routes pin to.
// The API key is injected at spawn time as MODEL_API_KEY; it never appears
// in generated configs, JSON, or logs.
const DirectBaseURL = "https://api.meta.ai/v1"

// DirectKeyEnv is the env var the generated config reads the key from.
const DirectKeyEnv = "MODEL_API_KEY"

// IsDirectRoute reports whether id is a direct/<model-id> route.
func IsDirectRoute(id string) bool {
	return strings.HasPrefix(strings.TrimSpace(id), "direct/")
}

// ModelIDFromDirectRoute returns the model id in a direct/<model-id> route.
func ModelIDFromDirectRoute(id string) string {
	mid := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(id), "direct/"))
	return strings.TrimSuffix(mid, "/")
}

// DirectConfigTOML renders a flocode config pinning one direct model. No
// secrets: the key is read by flocode from MODEL_API_KEY at runtime.
func DirectConfigTOML(modelID string) string {
	pin := amesh.PinName(modelID)
	var b strings.Builder
	b.WriteString("# flobot generated: direct model pin. No secrets.\n")
	b.WriteString("# route direct/" + modelID + "\n")
	fmt.Fprintf(&b, "default_model = %q\n\n", pin)
	fmt.Fprintf(&b, "[models.%s]\n", pin)
	b.WriteString("provider = \"openai-compat\"\n")
	fmt.Fprintf(&b, "model = %q\n", modelID)
	fmt.Fprintf(&b, "base_url = %q\n", DirectBaseURL)
	fmt.Fprintf(&b, "api_key_env = %q\n", DirectKeyEnv)
	return b.String()
}

// Lookup returns a documented route. Empty id defaults to glm. A "mesh/<id>"
// route resolves to a synthetic Route describing the amesh mesh model; the
// pin is resolved later against the live catalog (ResolveMeshPin).
func Lookup(id string) (Route, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		id = "glm"
	}
	if amesh.IsMeshRoute(id) {
		mid := amesh.ModelIDFromRoute(id)
		if mid == "" {
			return Route{}, false
		}
		return Route{
			ID:      amesh.RouteID(mid),
			Role:    "mesh",
			Source:  "amesh-mesh",
			Model:   mid,
			Purpose: "Any ready model served by an amesh mesh peer.",
		}, true
	}
	if IsDirectRoute(id) {
		mid := ModelIDFromDirectRoute(id)
		if mid == "" {
			return Route{}, false
		}
		return Route{
			ID:      "direct/" + mid,
			Role:    "direct",
			Source:  "meta-model-api",
			Model:   mid,
			Purpose: "Any model on the Meta model API. Key stays on this Mac.",
		}, true
	}
	for _, r := range DefaultRoutes() {
		if r.ID == id {
			return r, true
		}
	}
	return Route{}, false
}

// Validate reports whether id is a known route shape. It returns a precise,
// user-facing reason when the route id is unusable. Pinning and readiness
// are runtime concerns resolved at spawn time (ResolvePin / ResolveMeshPin):
// a bot may be created on "local" before it is pinned, and on a mesh route
// before that model is ready.
//
//   - documented routes (glm, qwen, local, muse): valid by construction.
//   - mesh/<id>: valid when the model id is non-empty.
//   - direct/<id>: valid when the model id is non-empty.
func Validate(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		id = "glm"
	}
	if IsLocal(id) {
		return nil
	}
	if amesh.IsMeshRoute(id) {
		if amesh.ModelIDFromRoute(id) == "" {
			return fmt.Errorf("empty mesh route %q", id)
		}
		return nil
	}
	if IsDirectRoute(id) {
		if ModelIDFromDirectRoute(id) == "" {
			return fmt.Errorf("empty direct route %q", id)
		}
		return nil
	}
	if _, ok := PinMap[id]; ok {
		return nil
	}
	return fmt.Errorf("unknown route %q", id)
}

// ResolvePin maps a route id to a flocode pin name. Base URLs are not
// returned; they live only in flocode config. Mesh routes resolve their pin
// against the live amesh catalog via ResolveMeshPin.
func ResolvePin(routeID string) (string, error) {
	id := strings.TrimSpace(routeID)
	if id == "" {
		id = "glm"
	}
	if id == "local" {
		if pin := LocalPin(); pin != "" {
			return pin, nil
		}
		return "", ErrLocalUnpinned
	}
	if amesh.IsMeshRoute(id) {
		return "", fmt.Errorf("mesh route %q: resolve with ResolveMeshPin", id)
	}
	if IsDirectRoute(id) {
		mid := ModelIDFromDirectRoute(id)
		if mid == "" {
			return "", fmt.Errorf("empty direct route %q", routeID)
		}
		return amesh.PinName(mid), nil
	}
	pin, ok := PinMap[id]
	if !ok {
		return "", fmt.Errorf("unknown route %q", routeID)
	}
	return pin, nil
}

// MeshPin is a resolved mesh route: the flocode pin name plus the generated
// config override that pins the mesh model. Config carries no secrets.
type MeshPin struct {
	Pin    string
	Config string
}

// ResolveMeshPin resolves a mesh/<id> route against the live amesh catalog.
// The returned config is a complete flocode config for the mesh model; the
// caller passes it to the child via FLOCODE_CONFIG. An empty addr uses the
// default amesh-web loopback address.
func ResolveMeshPin(ctx context.Context, routeID, addr string) (MeshPin, error) {
	mid := amesh.ModelIDFromRoute(routeID)
	if mid == "" {
		return MeshPin{}, fmt.Errorf("not a mesh route %q", routeID)
	}
	models, err := (&amesh.Client{Addr: addr}).Models(ctx)
	if err != nil {
		return MeshPin{}, err
	}
	m, ok := amesh.Find(models, mid)
	if !ok {
		return MeshPin{}, fmt.Errorf("mesh model %q not in the amesh catalog", mid)
	}
	if !m.Importable || !amesh.Healthy(m.Health) {
		return MeshPin{}, fmt.Errorf("mesh model %q is not ready (health %q)", mid, m.Health)
	}
	if strings.TrimSpace(m.BaseURL) == "" {
		return MeshPin{}, fmt.Errorf("mesh model %q has no endpoint", mid)
	}
	return MeshPin{Pin: amesh.PinName(m.ID), Config: amesh.ConfigTOML(m)}, nil
}
