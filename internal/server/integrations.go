// integrations.go is the HTTP surface for external-service integrations.
// An integration is an inbound event source — Gmail, Slack, GitHub, Telegram,
// Discord, Linear, Stripe or a generic webhook — that can fire the routines
// bound to it.
//
// Every integration owns one webhook secret and one authentication scheme.
// The secret is handed out exactly once (at creation, and again when it is
// rotated) and is never listed. The scheme decides how the caller proves it
// may fire the integration: a bearer header, a URL key, or one of the
// HMAC signature schemes the big providers use.
package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/florianleibert/flobot/internal/executor"
	"github.com/florianleibert/flobot/internal/store"
)

// maxFireBody bounds the webhook body flobot reads. Provider payloads are
// small; a bigger body is a mistake or an attack, not a use case.
const maxFireBody = 256 << 10

// signatureTolerance is how old a signed request may be. It blunts replay
// attacks: a captured signature is useless once it falls out of the window.
const signatureTolerance = 5 * time.Minute

// providerInfo describes one integration kind for the UI: what it is for,
// which authentication schemes fit it, and the steps to connect it.
type providerInfo struct {
	Kind string `json:"kind"`
	// Label is the provider's display name.
	Label string `json:"label"`
	// Blurb is one sentence on what the provider fires.
	Blurb string `json:"blurb"`
	// Glyph is a short mark shown in the UI.
	Glyph string `json:"glyph"`
	// DefaultAuth is the scheme the UI pre-selects.
	DefaultAuth string `json:"default_auth"`
	// AuthModes lists the schemes that work with this provider.
	AuthModes []string `json:"auth_modes"`
	// Steps is the connect checklist, one line per step.
	Steps []string `json:"steps"`
}

// authModeInfo describes one authentication scheme for the UI.
type authModeInfo struct {
	ID string `json:"id"`
	// Label is the scheme's display name.
	Label string `json:"label"`
	// Hint explains where the credential goes.
	Hint string `json:"hint"`
}

// providers is the catalogue of integration kinds. It is the single source of
// truth for the connect gallery: the UI renders it, and the server validates
// creation against it.
var providers = []providerInfo{
	{
		Kind:        store.IntegrationGmail,
		Label:       "Gmail",
		Glyph:       "M",
		Blurb:       "New mail in a watched mailbox fires a routine.",
		DefaultAuth: store.AuthBearer,
		AuthModes:   []string{store.AuthBearer, store.AuthQuery},
		Steps: []string{
			"Paste the companion Apps Script from docs/integrations-gmail.md.",
			"Deploy it as a web app and copy its /exec URL.",
			"Paste the URL and the secret into the Apps Script.",
			"Bind a routine — the next matching mail fires it.",
		},
	},
	{
		Kind:        store.IntegrationSlack,
		Label:       "Slack",
		Glyph:       "S",
		Blurb:       "Messages, mentions and reactions fire a routine.",
		DefaultAuth: store.AuthSlack,
		AuthModes:   []string{store.AuthSlack, store.AuthBearer, store.AuthQuery},
		Steps: []string{
			"Create a Slack app and enable the Events API.",
			"Set the request URL to flobot's webhook URL.",
			"Copy the app's signing secret into the secret field.",
			"Subscribe to the events you care about, then bind a routine.",
		},
	},
	{
		Kind:        store.IntegrationGitHub,
		Label:       "GitHub",
		Glyph:       "G",
		Blurb:       "Issues, pull requests, releases and checks fire a routine.",
		DefaultAuth: store.AuthHMAC,
		AuthModes:   []string{store.AuthHMAC, store.AuthBearer, store.AuthQuery, store.AuthStandard},
		Steps: []string{
			"Open the repo → Settings → Webhooks → Add webhook.",
			"Paste flobot's webhook URL, content type application/json.",
			"Choose a secret and paste the same value into flobot.",
			"Select the events, then bind a routine.",
		},
	},
	{
		Kind:        store.IntegrationTelegram,
		Label:       "Telegram",
		Glyph:       "T",
		Blurb:       "Bot messages and commands fire a routine — chatops from your phone.",
		DefaultAuth: store.AuthBearer,
		AuthModes:   []string{store.AuthBearer, store.AuthQuery},
		Steps: []string{
			"Create a bot with @BotFather and copy its token.",
			"POST the webhook URL to setWebhook for your bot.",
			"Paste the token into the secret field.",
			"Bind a routine — every message to the bot fires it.",
		},
	},
	{
		Kind:        store.IntegrationDiscord,
		Label:       "Discord",
		Glyph:       "D",
		Blurb:       "Bot interactions and channel events fire a routine.",
		DefaultAuth: store.AuthBearer,
		AuthModes:   []string{store.AuthBearer, store.AuthQuery},
		Steps: []string{
			"Create an application in the Discord developer portal.",
			"Paste flobot's webhook URL as the interactions endpoint.",
			"Paste the bot token into the secret field.",
			"Bind a routine.",
		},
	},
	{
		Kind:        store.IntegrationLinear,
		Label:       "Linear",
		Glyph:       "L",
		Blurb:       "Issue events fire a routine — triage without opening the app.",
		DefaultAuth: store.AuthHMAC,
		AuthModes:   []string{store.AuthHMAC, store.AuthBearer, store.AuthQuery},
		Steps: []string{
			"Open Linear → Settings → Webhooks and add a webhook.",
			"Paste flobot's webhook URL.",
			"Set a signing secret and paste the same value into flobot.",
			"Bind a routine.",
		},
	},
	{
		Kind:        store.IntegrationStripe,
		Label:       "Stripe",
		Glyph:       "$",
		Blurb:       "Payments, refunds and disputes fire a routine.",
		DefaultAuth: store.AuthStripe,
		AuthModes:   []string{store.AuthStripe, store.AuthBearer, store.AuthQuery},
		Steps: []string{
			"Open the Stripe dashboard → Developers → Webhooks.",
			"Add an endpoint with flobot's webhook URL.",
			"Copy the endpoint signing secret into the secret field.",
			"Bind a routine.",
		},
	},
	{
		Kind:        store.IntegrationWebhook,
		Label:       "Webhook",
		Glyph:       "∞",
		Blurb:       "Anything that can POST JSON — your script, a cron, a device.",
		DefaultAuth: store.AuthBearer,
		AuthModes: []string{
			store.AuthBearer, store.AuthQuery, store.AuthHMAC,
			store.AuthSlack, store.AuthStripe, store.AuthStandard,
		},
		Steps: []string{
			"Copy the webhook URL and the secret below.",
			"POST JSON to the URL — the body becomes the agent's context.",
			"Use the snippet tabs if you prefer a ready-made request.",
			"Bind a routine — the next event fires it.",
		},
	},
}

// authModes describes every authentication scheme for the UI.
var authModes = []authModeInfo{
	{store.AuthBearer, "Bearer header", "Authorization: Bearer <secret> — curl, n8n, Zapier, Make."},
	{store.AuthQuery, "URL key", "?key=<secret> — for providers that cannot set headers."},
	{store.AuthHMAC, "HMAC (GitHub)", "X-Hub-Signature-256: sha256=<hex> over the raw body."},
	{store.AuthSlack, "Slack signature", "X-Slack-Signature: v0=<hex> over v0:<timestamp>:<body>."},
	{store.AuthStripe, "Stripe signature", "Stripe-Signature: t=<ts>,v1=<hex> over <ts>.<body>."},
	{store.AuthStandard, "standard-webhooks", "webhook-id / webhook-timestamp / webhook-signature headers."},
}

// providerFor returns the catalogue entry for a kind.
func providerFor(kind string) providerInfo {
	for _, p := range providers {
		if p.Kind == kind {
			return p
		}
	}
	return providerInfo{Kind: kind, Label: kind, Blurb: "Custom event source.", DefaultAuth: store.AuthBearer, AuthModes: []string{store.AuthBearer}}
}

// integrationView is an integration with the routines bound to it, its fire
// history and everything the UI needs to show it. The webhook secret is
// deliberately absent: it is shown exactly once at creation and never listed.
type integrationView struct {
	ID string `json:"id"`
	// Kind is the provider kind (gmail, slack, …).
	Kind string `json:"kind"`
	// AuthMode is the authentication scheme a caller must use.
	AuthMode string `json:"auth_mode"`
	Name     string `json:"name"`
	Detail   string `json:"detail,omitempty"`
	// URL is the absolute webhook URL a provider posts to.
	URL string `json:"url"`
	// SecretHint is a short, non-reversible tail of the secret so two
	// integrations can be told apart in the UI.
	SecretHint string `json:"secret_hint"`
	CreatedAt  string `json:"created_at"`
	// Routines are the routines this integration fires.
	Routines []store.Routine `json:"routines"`
	// Events are the recorded fires, newest first.
	Events []store.IntegrationEvent `json:"events"`
	// FireCount counts the fires that started a run.
	FireCount int `json:"fire_count"`
	// LastFiredAt is when the integration last fired, RFC3339.
	LastFiredAt string `json:"last_fired_at,omitempty"`
	// Status is idle (never fired), ok (last event fired), error (last
	// event was rejected or failed) or unbound (no routine bound).
	Status string `json:"status"`
	// Provider is the catalogue entry for the kind.
	Provider providerInfo `json:"provider"`
}

// integrationViews enriches integrations with their bound routines and
// fire history. Secrets never leave the store.
func (s *Server) integrationViews(r *http.Request) []integrationView {
	all := s.integrations.ListIntegrations()
	out := make([]integrationView, 0, len(all))
	for _, in := range all {
		out = append(out, s.integrationView(r, in))
	}
	return out
}

// integrationView builds the view for one integration.
func (s *Server) integrationView(r *http.Request, in store.Integration) integrationView {
	routines := s.store.RoutinesForIntegration(in.ID)
	events := s.integrationEvents(in.ID)
	fires := 0
	last := ""
	for _, ev := range events {
		if ev.Status == "fired" {
			fires++
			if last == "" {
				last = ev.CreatedAt
			}
		}
	}
	status := "idle"
	switch {
	case len(events) > 0 && events[0].Status != "fired":
		status = "error"
	case fires > 0:
		status = "ok"
	}
	if len(routines) == 0 {
		status = "unbound"
	}
	authMode := in.AuthMode
	if authMode == "" {
		authMode = store.AuthBearer
	}
	return integrationView{
		ID:          in.ID,
		Kind:        in.Kind,
		AuthMode:    authMode,
		Name:        in.Name,
		Detail:      in.Detail,
		URL:         s.fireURL(r, in.ID),
		SecretHint:  store.SecretHint(in.Secret),
		CreatedAt:   in.CreatedAt,
		Routines:    routines,
		Events:      events,
		FireCount:   fires,
		LastFiredAt: last,
		Status:      status,
		Provider:    providerFor(in.Kind),
	}
}

// fireURL builds the absolute URL a provider posts to. It follows the
// request's host so the URL is exactly what the operator's browser (and
// therefore their provider) can reach.
func (s *Server) fireURL(r *http.Request, id string) string {
	host := "127.0.0.1:4120"
	if r != nil && r.Host != "" {
		host = r.Host
	}
	return "http://" + host + "/api/v1/integrations/" + id + "/fire"
}

// integrationEvents returns the fire events for one integration, newest
// first (the store keeps them oldest-first; the UI reads newest first).
func (s *Server) integrationEvents(id string) []store.IntegrationEvent {
	all := s.integrations.ListEvents()
	out := make([]store.IntegrationEvent, 0, len(all))
	for _, ev := range all {
		// An event belongs to the integration it came from. Events from an
		// older file carry no integration id: those are attributed through
		// the routine they fired.
		if ev.IntegrationID != "" {
			if ev.IntegrationID != id {
				continue
			}
		} else if ev.RoutineID != "" && !s.routineBelongsToIntegration(ev.RoutineID, id) {
			continue
		}
		out = append(out, ev)
	}
	// Newest first.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func (s *Server) routineBelongsToIntegration(routineID, integrationID string) bool {
	for _, rt := range s.store.RoutinesForIntegration(integrationID) {
		if rt.ID == routineID {
			return true
		}
	}
	return false
}

// listIntegrationProviders handles GET /api/v1/integrations/providers. It
// returns the catalogue the connect gallery is built from.
func (s *Server) listIntegrationProviders(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"schema":     "flobot.integration_providers.v1",
		"providers":  providers,
		"auth_modes": authModes,
	})
}

// listIntegrations handles GET /api/v1/integrations. It returns every
// integration with its bound routines and recent events — never a secret.
func (s *Server) listIntegrations(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"schema":       "flobot.integrations.v1",
		"integrations": s.integrationViews(r),
	})
}

// createIntegration handles POST /api/v1/integrations. It creates an
// integration and returns it with the webhook secret exactly once. The
// secret is not persisted anywhere else and is never listed again.
func (s *Server) createIntegration(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind     string `json:"kind"`
		Name     string `json:"name"`
		Detail   string `json:"detail"`
		AuthMode string `json:"auth_mode"`
	}
	if !decodeJSON(w, r, smallBodyLimit, &body) {
		return
	}
	in, err := s.integrations.CreateIntegration(body.Kind, body.Name, body.Detail, body.AuthMode)
	if err != nil {
		s.httpError(w, err)
		return
	}
	s.hub.publish(store.Event{Kind: "integration", Text: "created " + in.ID})
	view := s.integrationView(r, in)
	writeJSON(w, map[string]any{
		"schema":      "flobot.integration.v1",
		"integration": in,
		"view":        view,
		"url":         view.URL,
	})
}

// deleteIntegration handles DELETE /api/v1/integrations/{id}. It removes the
// integration and unbinds every routine that fired on it.
func (s *Server) deleteIntegration(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.integrations.Integration(id); !ok {
		s.httpError(w, store.ErrNotFound)
		return
	}
	for _, rt := range s.store.RoutinesForIntegration(id) {
		_, _ = s.store.BindRoutine(rt.ID, Actor, "")
	}
	s.integrations.DeleteIntegration(id)
	s.persist()
	s.hub.publish(store.Event{Kind: "integration", Text: "deleted " + id})
	w.WriteHeader(http.StatusNoContent)
}

// rotateIntegration handles POST /api/v1/integrations/{id}/rotate. It
// replaces the webhook secret and returns the new one exactly once — the old
// secret stops working immediately.
func (s *Server) rotateIntegration(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	in, ok := s.integrations.RotateSecret(id)
	if !ok {
		s.httpError(w, store.ErrNotFound)
		return
	}
	s.hub.publish(store.Event{Kind: "integration", Text: "rotated " + id})
	writeJSON(w, map[string]any{
		"schema":      "flobot.integration.v1",
		"integration": in,
		"view":        s.integrationView(r, in),
	})
}

// integrationEventsHandler handles GET /api/v1/integrations/{id}/events. It
// returns the fire history of one integration, newest first.
func (s *Server) integrationEventsHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.integrations.Integration(id); !ok {
		s.httpError(w, store.ErrNotFound)
		return
	}
	writeJSON(w, map[string]any{
		"schema": "flobot.integration_events.v1",
		"events": s.integrationEvents(id),
	})
}

// bindIntegration handles POST /api/v1/routines/{id}/integration. It binds a
// routine to an integration (or unbinds with an empty id). The routine then
// fires when the integration receives an event.
func (s *Server) bindIntegration(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IntegrationID string `json:"integration_id"`
	}
	if !decodeJSON(w, r, smallBodyLimit, &body) {
		return
	}
	id := strings.TrimSpace(body.IntegrationID)
	if id != "" {
		if _, ok := s.integrations.Integration(id); !ok {
			s.httpError(w, fmt.Errorf("unknown integration %q", id))
			return
		}
	}
	rt, err := s.store.BindRoutine(r.PathValue("id"), Actor, id)
	if err != nil {
		s.httpError(w, err)
		return
	}
	s.persist()
	s.hub.publish(store.Event{Kind: "integration", Text: "bound " + rt.ID})
	writeJSON(w, rt)
}

// fireIntegration handles POST /api/v1/integrations/{id}/fire. It is the
// webhook receiver: a service presents the integration's credential and a
// JSON body, and every routine bound to the integration fires with the body
// as context. The credential authenticates; the body is prompt context,
// never a secret.
func (s *Server) fireIntegration(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	in, ok := s.integrations.Integration(id)
	if !ok {
		s.httpError(w, store.ErrNotFound)
		return
	}
	raw, err := readBody(r, maxFireBody)
	if err != nil {
		s.httpError(w, err)
		return
	}
	if !s.authenticateFire(in, r, raw) {
		s.recordFireEvent(in, store.IntegrationEvent{Status: "rejected", Note: "rejected: bad credential"})
		http.Error(w, "invalid webhook credential", http.StatusUnauthorized)
		return
	}
	text := eventText(in.Kind, raw)
	runs, fired, err := s.fireBoundRoutines(in, text, "fired")
	if err != nil {
		// No routine is bound: the event is still real, so it is recorded
		// and the caller is told exactly what to do.
		s.recordFireEvent(in, store.IntegrationEvent{Status: "ignored", Note: clipNote(err.Error())})
		writeJSONStatus(w, http.StatusAccepted, map[string]any{
			"schema":      "flobot.integration_fire.v1",
			"integration": in.ID,
			"fired":       0,
			"runs":        runs,
			"reason":      err.Error(),
		})
		return
	}
	if fired == 0 {
		s.recordFireEvent(in, store.IntegrationEvent{Status: "ignored", Note: "no routine fired (paused, busy or failed)"})
		writeJSONStatus(w, http.StatusAccepted, map[string]any{
			"schema":      "flobot.integration_fire.v1",
			"integration": in.ID,
			"fired":       0,
			"runs":        runs,
			"reason":      "no routine fired (paused, busy or failed)",
		})
		return
	}
	s.hub.publish(store.Event{Kind: "integration", Text: "fired " + id})
	writeJSON(w, map[string]any{
		"schema":      "flobot.integration_fire.v1",
		"integration": in.ID,
		"fired":       fired,
		"runs":        runs,
	})
}

// testIntegration handles POST /api/v1/integrations/{id}/test. It fires the
// integration from the inside with a realistic sample payload, so the
// operator can prove the whole chain — payload, routine, agent — without
// touching the provider. It is the "Send a test event" button.
func (s *Server) testIntegration(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	in, ok := s.integrations.Integration(id)
	if !ok {
		s.httpError(w, store.ErrNotFound)
		return
	}
	payload := samplePayload(in.Kind)
	text := eventText(in.Kind, payload)
	runs, fired, err := s.fireBoundRoutines(in, text, "test")
	if err != nil {
		s.recordFireEvent(in, store.IntegrationEvent{Status: "ignored", Note: clipNote("test event: " + err.Error())})
		writeJSON(w, map[string]any{
			"schema":  "flobot.integration_test.v1",
			"fired":   0,
			"runs":    runs,
			"preview": text,
			"reason":  err.Error(),
		})
		return
	}
	if fired == 0 {
		s.recordFireEvent(in, store.IntegrationEvent{Status: "ignored", Note: "test event: no routine fired (paused, busy or failed)"})
		writeJSON(w, map[string]any{
			"schema":  "flobot.integration_test.v1",
			"fired":   0,
			"runs":    runs,
			"preview": text,
			"reason":  "no routine fired — bind an enabled routine first",
		})
		return
	}
	s.hub.publish(store.Event{Kind: "integration", Text: "tested " + id})
	writeJSON(w, map[string]any{
		"schema":  "flobot.integration_test.v1",
		"fired":   fired,
		"runs":    runs,
		"preview": text,
	})
}

// fireBoundRoutines starts one run per routine bound to the integration and
// records the fire. It returns the run ids and how many routines fired.
func (s *Server) fireBoundRoutines(in store.Integration, text, status string) ([]string, int, error) {
	routines := s.store.RoutinesForIntegration(in.ID)
	if len(routines) == 0 {
		return nil, 0, fmt.Errorf("integration %q has no routines bound", in.ID)
	}
	var runs []string
	fired := 0
	for _, rt := range routines {
		if rt.Paused {
			continue
		}
		runID, err := s.fireRoutineFromIntegration(rt, text)
		if err != nil {
			s.recordFireEvent(in, store.IntegrationEvent{
				RoutineID: rt.ID,
				Status:    "error",
				Note:      clipNote(status + ": " + err.Error()),
				CreatedAt: time.Now().UTC().Format(time.RFC3339),
			})
			continue
		}
		runs = append(runs, runID)
		fired++
	}
	return runs, fired, nil
}

// recordFireEvent appends one event to an integration's history.
func (s *Server) recordFireEvent(in store.Integration, ev store.IntegrationEvent) {
	if ev.CreatedAt == "" {
		ev.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	_ = s.integrations.RecordEvent(in, ev)
}

// fireRoutineFromIntegration spawns one run for a routine bound to an
// integration. The event text is prepended as context so the agent knows why
// it is running. It records a fire event on the integration.
func (s *Server) fireRoutineFromIntegration(rt store.Routine, eventText string) (string, error) {
	if s.store.HasRunningRun(rt.BotID) {
		return "", fmt.Errorf("routine %s bot %s already running", rt.ID, rt.BotID)
	}
	bot, err := s.store.GetBot(rt.BotID, Actor)
	if err != nil {
		return "", err
	}
	pin, cfgOverride, err := s.resolveRoute(context.Background(), bot.Route)
	if err != nil {
		return "", err
	}
	prompt := "Automation fire from an external integration.\n\n" + eventText
	if sk, err := s.store.GetSkill(rt.SkillID, Actor); err == nil {
		prompt = "Automation fire from an external integration.\n\nEvent:\n" + eventText + "\n\nFollow this skill how-to:\n" + sk.Body
	}
	run, err := s.store.CreateRun(Actor, bot.ID, prompt, "ask", false, false, "", pin, bot.Route)
	if err != nil {
		return "", err
	}
	s.spawn(run, executor.Request{
		Prompt:         prompt,
		SkillBody:      "",
		Pin:            pin,
		Intent:         "ask",
		Route:          bot.Route,
		Dir:            s.workDirNow(),
		ConfigOverride: cfgOverride,
	})
	if _, err := s.store.TestRun(rt.ID, Actor, run.ID, "integration fire "+run.ID); err != nil {
		return run.ID, err
	}
	in, _ := s.integrations.Integration(rt.IntegrationID)
	if in.ID != "" {
		_ = s.integrations.RecordEvent(in, store.IntegrationEvent{
			RoutineID: rt.ID,
			RunID:     run.ID,
			Status:    "fired",
			Note:      clipNote(eventText),
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
		})
	}
	return run.ID, nil
}

// authenticateFire checks the caller's credential against the integration's
// authentication scheme. Every comparison is constant time.
func (s *Server) authenticateFire(in store.Integration, r *http.Request, raw []byte) bool {
	mode := in.AuthMode
	if mode == "" {
		mode = store.AuthBearer
	}
	switch mode {
	case store.AuthQuery:
		return s.integrations.Authenticate(in.ID, r.URL.Query().Get("key"))
	case store.AuthHMAC:
		return verifyHMACHex(in.Secret, r.Header.Get("X-Hub-Signature-256"), "sha256=", raw)
	case store.AuthSlack:
		ts := r.Header.Get("X-Slack-Request-Timestamp")
		if !freshTimestamp(ts) {
			return false
		}
		payload := "v0:" + ts + ":" + string(raw)
		return verifyHMACHex(in.Secret, r.Header.Get("X-Slack-Signature"), "v0=", []byte(payload))
	case store.AuthStripe:
		return verifyStripeSignature(in.Secret, r.Header.Get("Stripe-Signature"), raw)
	case store.AuthStandard:
		return verifyStandardSignature(in.Secret, r.Header, raw)
	default:
		return s.integrations.Authenticate(in.ID, bearerToken(r))
	}
}

// verifyHMACHex checks a "prefix<hex>" HMAC-SHA256 header against the raw
// body. The comparison is constant time.
func verifyHMACHex(secret, header, prefix string, body []byte) bool {
	sig := strings.TrimSpace(header)
	if !strings.HasPrefix(sig, prefix) {
		return false
	}
	sig = strings.TrimPrefix(sig, prefix)
	want, err := hex.DecodeString(sig)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), want)
}

// verifyStripeSignature checks a Stripe-Signature header
// ("t=<unix>,v1=<hex>") over "<timestamp>.<body>". Several v1 values may be
// present during a key rotation; any of them may match. The timestamp must
// be inside the tolerance window.
func verifyStripeSignature(secret, header string, body []byte) bool {
	if header == "" {
		return false
	}
	var ts string
	var sigs []string
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		switch {
		case strings.HasPrefix(part, "t="):
			ts = strings.TrimPrefix(part, "t=")
		case strings.HasPrefix(part, "v1="):
			sigs = append(sigs, strings.TrimPrefix(part, "v1="))
		}
	}
	if ts == "" || len(sigs) == 0 {
		return false
	}
	if !freshTimestamp(ts) {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	want := mac.Sum(nil)
	for _, s := range sigs {
		got, err := hex.DecodeString(s)
		if err != nil {
			continue
		}
		if hmac.Equal(want, got) {
			return true
		}
	}
	return false
}

// verifyStandardSignature checks the standard-webhooks scheme: the
// webhook-id, webhook-timestamp and webhook-signature headers, with the
// secret in its whsec_ base64 form.
func verifyStandardSignature(secret string, h http.Header, body []byte) bool {
	id := h.Get("webhook-id")
	ts := h.Get("webhook-timestamp")
	header := h.Get("webhook-signature")
	if id == "" || ts == "" || header == "" {
		return false
	}
	if !freshTimestamp(ts) {
		return false
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + ts + "."))
	mac.Write(body)
	want := mac.Sum(nil)
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if !strings.HasPrefix(part, "v1,") {
			continue
		}
		got, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(part, "v1,"))
		if err != nil {
			continue
		}
		if hmac.Equal(want, got) {
			return true
		}
	}
	return false
}

// freshTimestamp reports whether a unix timestamp is inside the tolerance
// window. A request that is too old (or from the future) is refused: that
// is what makes a captured signature worthless.
func freshTimestamp(ts string) bool {
	n, err := strconv.ParseInt(strings.TrimSpace(ts), 10, 64)
	if err != nil {
		return false
	}
	d := time.Since(time.Unix(n, 0))
	if d < 0 {
		d = -d
	}
	return d <= signatureTolerance
}

// readBody reads a request body up to limit. A body over the limit is an
// error, not a silent truncation.
func readBody(r *http.Request, limit int64) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("body too large (limit %d bytes)", limit)
	}
	return raw, nil
}

// bearerToken extracts the bearer token from an Authorization header.
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const p = "Bearer "
	if len(h) > len(p) && strings.EqualFold(h[:len(p)], p) {
		return strings.TrimSpace(h[len(p):])
	}
	return ""
}

// clipNote bounds an event note so a huge payload cannot bloat the file.
func clipNote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// eventText turns a provider payload into the context the agent receives.
// Provider payloads are machine-shaped; the agent needs one readable
// sentence-ish block. The raw payload is never forwarded verbatim unless the
// provider is unknown.
func eventText(kind string, raw []byte) string {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return clipNote(string(raw))
	}
	switch kind {
	case store.IntegrationGmail:
		if t := str(m["text"]); t != "" {
			return "Gmail: " + t
		}
		from := str(m["from"])
		subject := str(m["subject"])
		snippet := str(m["snippet"])
		if subject == "" && from == "" && snippet == "" {
			return "Gmail: " + clipNote(compactJSON(m))
		}
		return "Gmail: " + joinNonEmpty("from ", from, " — ", subject, " — ", snippet)
	case store.IntegrationSlack:
		if t := str(m["text"]); t != "" {
			return "Slack: " + t
		}
		if ev, ok := m["event"].(map[string]any); ok {
			if t := str(ev["text"]); t != "" {
				return "Slack: " + t
			}
			return "Slack event: " + clipNote(compactJSON(ev))
		}
		return "Slack: " + clipNote(compactJSON(m))
	case store.IntegrationTelegram:
		if msg, ok := m["message"].(map[string]any); ok {
			if t := firstNonEmpty(str(msg["text"]), str(msg["caption"])); t != "" {
				return "Telegram: " + t
			}
			return "Telegram: " + clipNote(compactJSON(msg))
		}
		if post, ok := m["channel_post"].(map[string]any); ok {
			return "Telegram: " + firstNonEmpty(str(post["text"]), clipNote(compactJSON(post)))
		}
		return "Telegram: " + clipNote(compactJSON(m))
	case store.IntegrationDiscord:
		if t := str(m["content"]); t != "" {
			return "Discord: " + t
		}
		return "Discord: " + clipNote(compactJSON(m))
	case store.IntegrationGitHub:
		action := str(m["action"])
		repo := ""
		if r, ok := m["repository"].(map[string]any); ok {
			repo = str(r["full_name"])
		}
		title := ""
		number := ""
		for _, key := range []string{"issue", "pull_request", "release", "discussion"} {
			obj, ok := m[key].(map[string]any)
			if !ok {
				continue
			}
			if t := firstNonEmpty(str(obj["title"]), str(obj["name"]), str(obj["body"])); t != "" {
				title = t
			}
			if n := obj["number"]; n != nil {
				number = fmt.Sprintf("#%v", n)
			}
			break
		}
		sender := ""
		if s, ok := m["sender"].(map[string]any); ok {
			sender = str(s["login"])
		}
		out := joinNonEmpty("GitHub: ", action, " ", repo, " ", number, " ", title)
		if sender != "" {
			out += " (by " + sender + ")"
		}
		if out == "GitHub: " {
			out = "GitHub: " + clipNote(compactJSON(m))
		}
		return out
	case store.IntegrationLinear:
		typ := firstNonEmpty(str(m["type"]), str(m["action"]))
		title := ""
		if d, ok := m["data"].(map[string]any); ok {
			title = firstNonEmpty(str(d["title"]), str(d["name"]), str(d["identifier"]))
		}
		out := joinNonEmpty("Linear: ", typ, " ", title)
		if out == "Linear: " {
			out = "Linear: " + clipNote(compactJSON(m))
		}
		return out
	case store.IntegrationStripe:
		typ := str(m["type"])
		desc := ""
		if d, ok := m["data"].(map[string]any); ok {
			if obj, ok := d["object"].(map[string]any); ok {
				desc = firstNonEmpty(str(obj["description"]), str(obj["statement_descriptor"]))
			}
		}
		out := joinNonEmpty("Stripe: ", typ, " ", desc)
		if out == "Stripe: " {
			out = "Stripe: " + clipNote(compactJSON(m))
		}
		return out
	default:
		if t := str(m["text"]); t != "" {
			return t
		}
		if title := str(m["title"]); title != "" {
			return joinNonEmpty(title, " — ", str(m["body"]))
		}
		return clipNote(compactJSON(m))
	}
}

// samplePayload returns a realistic payload for a provider. It is what the
// "Send a test event" button fires, so the operator can see exactly what the
// agent would receive.
func samplePayload(kind string) []byte {
	var payload string
	switch kind {
	case store.IntegrationGmail:
		payload = `{"from":"you@example.com","subject":"Test mail from flobot","snippet":"This is a test event fired from the flobot UI.","text":"This is a test event fired from the flobot UI."}`
	case store.IntegrationSlack:
		payload = `{"text":"Test message from flobot","channel":"C0000000","user_name":"flobot"}`
	case store.IntegrationGitHub:
		payload = `{"action":"opened","repository":{"full_name":"flobot/demo"},"issue":{"number":1,"title":"Test issue from flobot"},"sender":{"login":"flobot"}}`
	case store.IntegrationTelegram:
		payload = `{"message":{"text":"Test message from flobot","chat":{"id":1}}}`
	case store.IntegrationDiscord:
		payload = `{"content":"Test message from flobot"}`
	case store.IntegrationLinear:
		payload = `{"type":"Issue","action":"create","data":{"title":"Test issue from flobot","identifier":"TEST-1"}}`
	case store.IntegrationStripe:
		payload = `{"type":"payment_intent.succeeded","data":{"object":{"amount":1000,"currency":"usd","description":"Test payment from flobot"}}}`
	default:
		payload = `{"text":"Test event from flobot"}`
	}
	return []byte(payload)
}

// str renders a JSON value as a trimmed string.
func str(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

// firstNonEmpty returns the first non-empty string.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// joinNonEmpty concatenates the non-empty parts.
func joinNonEmpty(parts ...string) string {
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		b.WriteString(p)
	}
	return b.String()
}

// compactJSON renders a decoded payload as compact JSON, for providers whose
// shape flobot does not know in detail.
func compactJSON(m map[string]any) string {
	raw, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(raw)
}
