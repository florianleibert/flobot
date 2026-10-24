package server

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/florianleibert/flobot/internal/amesh"
	"github.com/florianleibert/flobot/internal/executor"
)

// hubModels lists the joined hubs and every model service (local + enrolled
// hub) from the amesh CLI. This is the discovery surface the dock app uses to
// show hubs and models with their ready/stopped state. A missing amesh binary
// or an unreachable hub is a rendered state (ok=false), not a 500.
func (s *Server) hubModels(w http.ResponseWriter, r *http.Request) {
	hubs, hubErr := amesh.Hubs(r.Context())
	services, svcErr := amesh.Services(r.Context())
	writeJSON(w, map[string]any{
		"schema":   "flobot.hubs.v1",
		"hubs":     hubs,
		"models":   services,
		"ok":       hubErr == nil && svcErr == nil,
		"hub_error": errString(hubErr),
		"model_error": errString(svcErr),
	})
}

// modelStart starts a local model service via `flocode localmodel start
// --profile <profile>`. The profile is the amesh service profile (e.g.
// "qwen3-coder-30b-a3b-xl"); it is not a secret. The start is async: the
// supervisor spawns the backend and polls its health endpoint, so this
// returns immediately with the spawn outcome and the caller polls
// /api/v1/models to watch state flip to "ready".
func (s *Server) modelStart(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		http.Error(w, `{"error":"model id required"}`, http.StatusBadRequest)
		return
	}
	services, err := amesh.Services(r.Context())
	if err != nil {
		http.Error(w, `{"error":"`+strings.ReplaceAll(err.Error(), `"`, `'`)+`"}`, http.StatusBadGateway)
		return
	}
	var svc *amesh.Service
	for i := range services {
		if services[i].ID == id {
			svc = &services[i]
			break
		}
	}
	if svc == nil {
		http.Error(w, `{"error":"unknown model"}`, http.StatusNotFound)
		return
	}
	if !strings.EqualFold(svc.Location, "local") {
		http.Error(w, `{"error":"only local models can be started from here; hub models are started on their node"}`, http.StatusBadRequest)
		return
	}
	if strings.EqualFold(svc.State, "ready") {
		writeJSON(w, map[string]any{"ok": true, "id": id, "state": "ready", "already": true})
		return
	}
	profile := strings.TrimSpace(svc.Profile)
	if profile == "" {
		http.Error(w, `{"error":"model has no start profile"}`, http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), modelStartBudget)
	defer cancel()
	if err := startLocalModel(ctx, profile); err != nil {
		http.Error(w, `{"error":"start failed: `+strings.ReplaceAll(err.Error(), `"`, `'`)+`"}`, http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "id": id, "state": "starting", "profile": profile})
}

// startLocalModel runs `flocode localmodel start --profile <profile>` with a
// bounded budget. The supervisor spawns the backend in its own process group,
// persists pid+start-time, and polls the health endpoint until ready, so this
// returns once the spawn is accepted (the model may still be warming up).
func startLocalModel(ctx context.Context, profile string) error {
	bin, err := executor.FindBinary()
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, bin, "localmodel", "start", "--profile", profile)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

const modelStartBudget = 60 * time.Second
