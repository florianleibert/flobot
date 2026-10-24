package server

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/florianleibert/flobot/internal/amesh"
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

// modelStart warms a model service via `amesh models chat <id> --prompt
// "ping"`. The amesh helper resolves the service id, verifies the current hub
// route/transport, and auto-starts a stopped local model on demand (the local
// lifecycle is browser-paired; a stopped local model may require the owner to
// approve the pairing request). This is the real "start" surface — there is no
// standalone `amesh models start`; models come up on first use.
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
	if strings.EqualFold(svc.State, "ready") {
		writeJSON(w, map[string]any{"ok": true, "id": id, "state": "ready", "already": true})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), modelStartBudget)
	defer cancel()
	if err := warmModel(ctx, id); err != nil {
		http.Error(w, `{"error":"start failed: `+strings.ReplaceAll(err.Error(), `"`, `'`)+`"}`, http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "id": id, "state": "starting"})
}

// warmModel runs `amesh models chat <id> --prompt ping` with a bounded
// budget. A stopped local model comes up on demand; a ready hub model answers
// immediately. The prompt is a fixed non-secret ping.
func warmModel(ctx context.Context, id string) error {
	bin, err := amesh.Bin()
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, bin, "models", "chat", id, "--prompt", "ping")
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
