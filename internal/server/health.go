package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/florianleibert/flobot/internal/amesh"
	"github.com/florianleibert/flobot/internal/executor"
	"github.com/florianleibert/flobot/internal/routes"
)

const probeTimeout = 8 * time.Second

// Probe is a /v1/models target. URLs are server-side only.
type Probe struct {
	ID   string
	URL  string
	Auth bool // attach MODEL_API_KEY when present (muse)
}

// ProbeResult is returned by GET /api/v1/health/models. Never includes keys.
type ProbeResult struct {
	ID           string   `json:"id"`
	OK           bool     `json:"ok"`
	IDs          []string `json:"ids,omitempty"`
	Error        string   `json:"error,omitempty"`
	QwenMismatch bool     `json:"qwen_mismatch,omitempty"`
}

// MuseProbeURL is the only non-mesh endpoint flobot probes directly.
const MuseProbeURL = "https://api.meta.ai/v1"

func (s *Server) healthModels(w http.ResponseWriter, r *http.Request) {
	if len(s.probes) > 0 {
		out := make([]ProbeResult, len(s.probes))
		var wg sync.WaitGroup
		for i, p := range s.probes {
			wg.Add(1)
			go func(i int, p Probe) {
				defer wg.Done()
				out[i] = s.probeModels(r.Context(), p)
			}(i, p)
		}
		wg.Wait()
		writeJSON(w, map[string]any{"probes": out})
		return
	}
	// No explicit probes: report health for the documented routes. Mesh-backed
	// routes (glm, qwen) resolve their endpoint from the live amesh catalog
	// instead of hardcoded IPs; local is unpinned; muse keeps its API probe.
	writeJSON(w, map[string]any{"probes": s.probeRoutes(r.Context())})
}

// probeRoutes builds ProbeResults for every documented route.
func (s *Server) probeRoutes(ctx context.Context) []ProbeResult {
	models, catErr := (&amesh.Client{Addr: s.ameshAddr}).Models(ctx)
	out := make([]ProbeResult, 0, 4)
	for _, rt := range routes.DefaultRoutes() {
		switch rt.ID {
		case "local":
			out = append(out, ProbeResult{ID: rt.ID, IDs: []string{}, Error: "unpinned"})
		case "muse":
			out = append(out, s.probeModels(ctx, Probe{ID: rt.ID, URL: MuseProbeURL, Auth: true}))
		default:
			if catErr != nil {
				out = append(out, ProbeResult{ID: rt.ID, IDs: []string{}, Error: "amesh catalog: " + catErr.Error()})
				continue
			}
			m, ok := amesh.Find(models, rt.Model)
			if !ok {
				out = append(out, ProbeResult{ID: rt.ID, IDs: []string{}, Error: "not in amesh catalog"})
				continue
			}
			if !amesh.Healthy(m.Health) {
				out = append(out, ProbeResult{ID: rt.ID, IDs: []string{}, Error: "health " + m.Health})
				continue
			}
			if strings.TrimSpace(m.BaseURL) == "" {
				out = append(out, ProbeResult{ID: rt.ID, IDs: []string{}, Error: "no endpoint"})
				continue
			}
			out = append(out, s.probeModels(ctx, Probe{ID: rt.ID, URL: m.BaseURL}))
		}
	}
	return out
}

func (s *Server) probeModels(parent context.Context, p Probe) ProbeResult {
	res := ProbeResult{ID: p.ID, IDs: []string{}}
	ctx, cancel := context.WithTimeout(parent, probeTimeout)
	defer cancel()
	url := strings.TrimRight(p.URL, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		res.Error = "request"
		return res
	}
	if p.Auth {
		if k, err := executor.ReadModelAPIKey(s.home); err == nil {
			req.Header.Set("Authorization", "Bearer "+k)
		}
	}
	client := s.client
	if client == nil {
		client = &http.Client{Timeout: probeTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		res.Error = "unreachable"
		return res
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		res.Error = "read body"
		return res
	}
	if resp.StatusCode != http.StatusOK {
		res.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
		return res
	}
	ids := parseModelIDs(body)
	res.IDs = ids
	res.OK = true
	if strings.HasPrefix(p.ID, "qwen") && !hasQwen38(ids) {
		res.QwenMismatch = true
		res.Error = "endpoint does not serve qwen3.8"
	}
	return res
}

func parseModelIDs(body []byte) []string {
	var wrap struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wrap); err != nil {
		return nil
	}
	out := make([]string, 0, len(wrap.Data))
	for _, row := range wrap.Data {
		id := strings.TrimSpace(row.ID)
		if id != "" {
			out = append(out, id)
		}
	}
	return out
}

func hasQwen38(ids []string) bool {
	for _, id := range ids {
		low := strings.ToLower(id)
		if strings.Contains(low, "qwen3.8") || strings.Contains(low, "qwen3-8") {
			return true
		}
	}
	return false
}
