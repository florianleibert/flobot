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

	"github.com/florianleibert/flobot/internal/executor"
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

// DefaultProbes are the mesh + Muse endpoints from research round 3.
func DefaultProbes() []Probe {
	return []Probe{
		{ID: "glm", URL: "http://10.30.0.127:13038/v1"},
		{ID: "qwen38", URL: "http://23.183.40.72:8020/v1"},
		{ID: "qwen36", URL: "http://10.10.0.14:8000/v1"},
		{ID: "muse", URL: "https://api.meta.ai/v1", Auth: true},
	}
}

func (s *Server) healthModels(w http.ResponseWriter, r *http.Request) {
	probes := s.probes
	if len(probes) == 0 {
		probes = DefaultProbes()
	}
	out := make([]ProbeResult, len(probes))
	var wg sync.WaitGroup
	for i, p := range probes {
		wg.Add(1)
		go func(i int, p Probe) {
			defer wg.Done()
			out[i] = s.probeModels(r.Context(), p)
		}(i, p)
	}
	wg.Wait()
	writeJSON(w, map[string]any{"probes": out})
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
		res.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
		return res
	}
	if resp.StatusCode != http.StatusOK {
		res.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
		return res
	}
	ids := parseModelIDs(body)
	res.IDs = ids
	res.OK = true
	if p.ID == "qwen38" && !hasQwen38(ids) {
		res.QwenMismatch = true
		res.Error = ":8020 is not qwen3.8"
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
