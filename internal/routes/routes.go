// Package routes is the advertised model combination for standalone flobot.
// JSON never includes secrets or base URLs; pins live in flocode config.
package routes

import (
	"errors"
	"fmt"
	"strings"
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

// PinMap maps route ids to flocode --model pin names. local is omitted
// until a pin exists. Base URLs stay in flocode config, not this map.
var PinMap = map[string]string{
	"glm":  "glm-5.2",
	"qwen": "qwen-3.8",
	"muse": "muse-spark-1.2",
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
			Model:   "muse-spark-1.2",
			Purpose: "1M-context planning. Key stays on this Mac; never in the browser.",
		},
	}
}

// Lookup returns a documented route. Empty id defaults to glm.
func Lookup(id string) (Route, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		id = "glm"
	}
	for _, r := range DefaultRoutes() {
		if r.ID == id {
			return r, true
		}
	}
	return Route{}, false
}

// ResolvePin maps a route id to a flocode pin name. Base URLs are not
// returned; they live only in flocode config.
func ResolvePin(routeID string) (string, error) {
	id := strings.TrimSpace(routeID)
	if id == "" {
		id = "glm"
	}
	if id == "local" {
		return "", ErrLocalUnpinned
	}
	pin, ok := PinMap[id]
	if !ok {
		return "", fmt.Errorf("unknown route %q", routeID)
	}
	return pin, nil
}
