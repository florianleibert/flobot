// usecases.go is the catalogue of ready-made automations: ten use cases that
// each ship with the teammate, the playbook, the routine and the provider
// that fires them.
//
// Every one of them runs on the local route — this Mac's MLX model — so the
// whole kit works with no mesh hop, no API key and no cloud bill. The
// playbooks are written for a local model: read-only tools, a bounded
// answer, and an explicit "I don't know" instead of a guess.
package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/florianleibert/flobot/internal/store"
)

// useCase is one ready-made automation.
type useCase struct {
	ID string `json:"id"`
	// Name is the display name.
	Name string `json:"name"`
	// Category groups the kit in the UI.
	Category string `json:"category"`
	// Blurb is one sentence on what fires.
	Blurb string `json:"blurb"`
	// Provider is the integration kind that fires it.
	Provider string `json:"provider"`
	// AuthMode is the scheme that provider should use.
	AuthMode string `json:"auth_mode"`
	// Route is the model route the teammate runs on. Always local.
	Route string `json:"route"`
	// BotName and Role name the teammate that is created.
	BotName string `json:"bot_name"`
	Role    string `json:"role"`
	// Tools is the tool policy the routine runs with: "none" for a
	// text-only automation, or a comma-separated read-only allow-list
	// for one that may read files.
	Tools string `json:"tools"`
	// Playbook is the skill body — the how-to the agent follows.
	Playbook string `json:"playbook"`
	// Event is a sample payload for the test fire.
	Event string `json:"event"`
	// Steps is what the operator still has to do outside flobot.
	Steps []string `json:"steps"`
}

// useCases is the kit. Ten automations, ten providers' worth of practice,
// one local model.
var useCases = []useCase{
	{
		ID:       "triage",
		Name:     "Triage a new issue",
		Category: "triage",
		Blurb:    "A new issue arrives — classify it and say what to do next.",
		Provider: store.IntegrationGitHub,
		AuthMode: store.AuthHMAC,
		Route:    "local",
		BotName:  "Triage",
		Role:     "triage",
		Tools:    "none",
		Playbook: `You are a triage teammate running on this Mac.

Answer in plain text. Do not call tools: the event below is all you need.

Given the event below:

1. Classify it as exactly one of: bug, feature request, question, spam, other.
2. One line: what happened, in plain words.
3. One line: the next action, naming the file or command a human should look at first.
4. If the event names no repository or file, say "not enough context" instead of guessing.

Never invent file paths, issue numbers or links that are not in the event.`,
		Event: `{"action":"opened","repository":{"full_name":"flobot/demo"},"issue":{"number":42,"title":"login is broken on Safari"},"sender":{"login":"ana"}}`,
		Steps: []string{
			"Add a GitHub webhook on the repo and paste flobot's URL.",
			"Set the same secret in GitHub and in flobot.",
			"Open an issue — the triage teammate answers in the run.",
		},
	},
	{
		ID:       "chatops",
		Name:     "Chatops from your phone",
		Category: "chatops",
		Blurb:    "A message to the Telegram bot runs a task on this Mac.",
		Provider: store.IntegrationTelegram,
		AuthMode: store.AuthBearer,
		Route:    "local",
		BotName:  "Chatops",
		Role:     "chatops",
		Tools:    "none",
		Playbook: `You are a chatops teammate running on this Mac. Every message from the operator arrives as an event.

You have no tools: you cannot open files or run commands. Answer in plain text.

1. Restate the request in one line.
2. Answer from the event. If it asks for a file or a fact you cannot see, name the exact command a human should run and what it would show.
3. If it asks for a change to the workspace, do not change anything: write the exact command or patch you would run.
4. Keep the whole answer under ten lines.

Never claim you did something you did not do.`,
		Event: `{"message":{"text":"the deploy step failed — what do I check first?","chat":{"id":1}}}`,
		Steps: []string{
			"Create a bot with @BotFather and keep the bot token.",
			"Point setWebhook at flobot's URL with the secret as ?key=.",
			"Message the bot — the answer comes back as a run.",
		},
	},
	{
		ID:       "oncall",
		Name:     "On-call: investigate an alert",
		Category: "on-call",
		Blurb:    "An alert fires a teammate that says what to check first.",
		Provider: store.IntegrationWebhook,
		AuthMode: store.AuthBearer,
		Route:    "local",
		BotName:  "On-call",
		Role:     "investigator",
		Tools:    "none",
		Playbook: `You are the on-call investigator running on this Mac.

Answer in plain text. Do not call tools.

Given the alert:

1. One sentence: what is broken, using only what the alert says.
2. The two checks a human should run first, as read-only shell commands.
3. What would make you escalate.

Do not restart services, do not write files, and do not name a root cause the alert does not support.`,
		Event: `{"alert":"DiskUsageHigh","severity":"warning","host":"tictactoe","text":"disk 7 is at 91% (threshold 85%)"}`,
		Steps: []string{
			"Point your alert manager at flobot's webhook URL.",
			"Put the secret in the Authorization header.",
			"Fire one test alert and read the answer.",
		},
	},
	{
		ID:       "release",
		Name:     "Release notes from a release event",
		Category: "release",
		Blurb:    "A release event becomes notes you can paste anywhere.",
		Provider: store.IntegrationGitHub,
		AuthMode: store.AuthHMAC,
		Route:    "local",
		BotName:  "Notes",
		Role:     "writer",
		Tools:    "none",
		Playbook: `You write release notes from a release event.

Answer in plain text. Do not call tools.

1. One line: what shipped, in the past tense, from the event only.
2. Then "Highlights": one bullet per change the event actually lists, one sentence each, no marketing adjectives. Never repeat a change, and if the event lists one change, write one bullet.
3. Then "Upgrade notes": one line, or "No upgrade notes." when the event carries none.

Use only what the event contains. If it carries no notes, say so instead of inventing them.`,
		Event: `{"action":"released","release":{"tag_name":"v1.4.0","name":"v1.4.0","body":"- faster tree\n- fix crash on rename"},"repository":{"full_name":"flobot/demo"}}`,
		Steps: []string{
			"Subscribe the GitHub webhook to the release event.",
			"Bind the routine and release a tag.",
		},
	},
	{
		ID:       "billing",
		Name:     "Reconcile a payment",
		Category: "billing",
		Blurb:    "A payment event becomes one line of bookkeeping.",
		Provider: store.IntegrationStripe,
		AuthMode: store.AuthStripe,
		Route:    "local",
		BotName:  "Billing",
		Role:     "bookkeeper",
		Tools:    "none",
		Playbook: `You are the billing check running on this Mac.

Answer in plain text. Do not call tools.

Given the payment event:

1. Amount and currency, exactly as received.
2. What the charge is for, in one line.
3. Whether it looks like a duplicate of an earlier charge: compare amount and reference, and when you cannot tell, say "cannot tell".
4. The one action needed, or "no action".

Do not compute totals you cannot see and do not touch any billing system.`,
		Event: `{"type":"payment_intent.succeeded","data":{"object":{"amount":4900,"currency":"usd","description":"Pro plan - March","id":"pi_123"}}}`,
		Steps: []string{
			"Add a Stripe endpoint with flobot's URL.",
			"Paste the endpoint signing secret into flobot.",
			"Bind the routine; every payment event lands in the run log.",
		},
	},
	{
		ID:       "inbox",
		Name:     "Extract facts from an email",
		Category: "inbox",
		Blurb:    "A new mail becomes six lines of structured facts.",
		Provider: store.IntegrationGmail,
		AuthMode: store.AuthBearer,
		Route:    "local",
		BotName:  "Inbox",
		Role:     "reader",
		Tools:    "none",
		Playbook: `You extract facts from an email for a human.

Answer in plain text. Do not call tools.

Reply with exactly these lines, in this order, and nothing else:

SUBJECT: <subject>
FROM: <sender>
WHEN: <date, or "not stated">
AMOUNT: <amount and currency, or "none">
DUE: <date, or "none">
ACTION: <the one thing the reader has to do, or "none">

Start with the SUBJECT line. Never repeat a line, and never repeat the block.
If a field is not in the email, write "not stated". Never infer a date or an amount.`,
		Event: `{"from":"ana@example.com","subject":"Invoice 2213 due Friday","snippet":"Please pay 49.00 EUR by 2026-04-03."}`,
		Steps: []string{
			"Deploy the Gmail Apps Script from docs/integrations-gmail.md.",
			"Paste flobot's URL and secret into the script.",
			"Bind the routine; every matching mail fires it.",
		},
	},
	{
		ID:       "home",
		Name:     "React to a sensor event",
		Category: "home",
		Blurb:    "A device event becomes one line and one safe command.",
		Provider: store.IntegrationWebhook,
		AuthMode: store.AuthBearer,
		Route:    "local",
		BotName:  "Home",
		Role:     "watcher",
		Tools:    "none",
		Playbook: `You are a home-automation teammate running on this Mac.

Given the sensor event:

1. What changed, in one line, with the value and its unit.
2. Whether the value crosses a threshold you can read from the event — and say which threshold you used.
3. The single command a human should run to act on it: read-only, no destructive flags.

Start with line 1. No preamble, no repetition.
Never claim to have changed anything: you cannot actuate devices from here.`,
		Event: `{"device":"disk7","metric":"usage_percent","value":91,"unit":"%","threshold":85}`,
		Steps: []string{
			"Point your device or Home Assistant webhook at flobot's URL.",
			"Send the reading as JSON with a text field.",
		},
	},
	{
		ID:       "ci",
		Name:     "Diagnose a failed build",
		Category: "ci",
		Blurb:    "A failed build arrives with its first error line.",
		Provider: store.IntegrationWebhook,
		AuthMode: store.AuthBearer,
		Route:    "local",
		BotName:  "CI",
		Role:     "diagnostician",
		Tools:    "none",
		Playbook: `You diagnose a failed build on this Mac.

Answer in plain text. Do not call tools.

1. The failing stage or job, from the event.
2. The first error line you can see, quoted exactly.
3. The most likely cause, one sentence, hedged with "likely".
4. The next command to run: read-only.

If the event carries no log output, say "no log output in the event" and list what to fetch instead.`,
		Event: `{"build":482,"branch":"main","status":"failed","stage":"test","text":"FAIL demo: TestLogin (0.02s)\\n  expected 200, got 500\\n  at demo_test.go:41"}`,
		Steps: []string{
			"Add a CI step that POSTs the failure to flobot's URL.",
			"Keep the secret in the Authorization header.",
		},
	},
	{
		ID:       "workflow",
		Name:     "Be the smart step in an automation",
		Category: "workflow",
		Blurb:    "n8n or Zapier calls flobot as one step of a longer flow.",
		Provider: store.IntegrationWebhook,
		AuthMode: store.AuthBearer,
		Route:    "local",
		BotName:  "Flow",
		Role:     "step",
		Tools:    "none",
		Playbook: `You are one step inside a larger automation. The event carries that step's input.

Answer in plain text. Do not call tools.

1. Parse the input: name every field you received and its value.
2. Decide the step's output, in the shape the event asked for, or JSON when it did not say.
3. Output only the result — no preamble, no explanation — so the automation can parse it.

Never add commentary around the result.`,
		Event: `{"text":"{\"order\":\"A-19\",\"total\":42,\"currency\":\"EUR\"}","format":"json"}`,
		Steps: []string{
			"Add an HTTP Request step in n8n or Zapier.",
			"Paste flobot's URL, the secret header and a JSON body.",
		},
	},
	{
		ID:       "capability",
		Name:     "A bookmarked URL that runs a task",
		Category: "capability",
		Blurb:    "One secret URL on your phone runs one bounded local task.",
		Provider: store.IntegrationWebhook,
		AuthMode: store.AuthQuery,
		Route:    "local",
		BotName:  "Task",
		Role:     "runner",
		Tools:    "none",
		Playbook: `You are the task runner behind a bookmarked URL on this Mac. The event carries one task.

You have no tools: you cannot run anything yourself. Answer in plain text.

1. Name the task in one line.
2. Give the exact read-only command that does it, and say what its output would tell the reader.
3. Stop. If the task would need a write or a network call, say "not allowed" and name the command a human should run instead.

This URL is a capability: only someone holding the secret should be able to trigger it. Keep the answer short.`,
		Event: `{"text":"list the files in the workspace root"}`,
		Steps: []string{
			"Create a Webhook integration and keep the ?key= URL.",
			"Bookmark it on your phone, or call it from a shortcut.",
		},
	},
}

// listUseCases handles GET /api/v1/usecases. It returns the kit.
func (s *Server) listUseCases(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"schema":   "flobot.usecases.v1",
		"usecases": useCases,
		"route":    "local",
	})
}

// useCaseByID returns one catalogue entry.
func useCaseByID(id string) (useCase, bool) {
	for _, uc := range useCases {
		if uc.ID == id {
			return uc, true
		}
	}
	return useCase{}, false
}

// activateUseCase handles POST /api/v1/usecases/{id}/activate.
//
// It creates everything the use case needs in one step: a teammate on the
// local route, the playbook, the routine, and — unless the operator already
// has an integration — the integration itself. The routine is test-run and
// enabled, so the very next event fires it.
//
// The integration secret is returned exactly once, like every other secret
// flobot hands out.
func (s *Server) activateUseCase(w http.ResponseWriter, r *http.Request) {
	uc, ok := useCaseByID(r.PathValue("id"))
	if !ok {
		s.httpError(w, store.ErrNotFound)
		return
	}
	var body struct {
		// IntegrationID binds the new routine to an existing integration.
		IntegrationID string `json:"integration_id"`
		// CreateIntegration creates the provider integration as well.
		CreateIntegration bool `json:"create_integration"`
		// Name overrides the teammate name.
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, smallBodyLimit, &body) {
		return
	}

	botName := uc.BotName
	if n := strings.TrimSpace(body.Name); n != "" {
		botName = n
	}
	bot, err := s.store.CreateBot(Actor, botName, uc.Role, uc.Route)
	if err != nil {
		s.httpError(w, err)
		return
	}
	sk, err := s.store.CreateSkill(Actor, uc.Name+" playbook", uc.Playbook)
	if err != nil {
		s.httpError(w, err)
		return
	}

	// The integration: the one the operator already has, or a fresh one.
	var in store.Integration
	created := false
	switch {
	case strings.TrimSpace(body.IntegrationID) != "":
		in, ok = s.integrations.Integration(strings.TrimSpace(body.IntegrationID))
		if !ok {
			s.httpError(w, fmt.Errorf("unknown integration %q", body.IntegrationID))
			return
		}
	case body.CreateIntegration:
		in, err = s.integrations.CreateIntegration(uc.Provider, uc.Name, uc.Blurb, uc.AuthMode)
		if err != nil {
			s.httpError(w, err)
			return
		}
		created = true
	}

	rt, err := s.store.CreateRoutine(Actor, bot.ID, sk.ID, "manual", in.ID)
	if err != nil {
		s.httpError(w, err)
		return
	}

	// Give the routine its own worktree. Ten automations firing at once must
	// not contend for one checkout: flocode refuses to run two sessions in
	// the same checkout, and the second one would die with "already in use".
	threadID := ""
	if th, terr := s.threads.Create(r.Context(), s.workDirNow(), bot.ID, uc.Name); terr == nil {
		threadID = th.ID
		if armed, aerr := s.store.SetRoutineWorktree(rt.ID, Actor, threadID); aerr == nil {
			rt = armed
		}
	}
	// The tool policy: a text-only automation gets no tools at all, so a
	// tool-happy local model answers instead of returning a tool call.
	if armed, terr := s.store.SetRoutineTools(rt.ID, Actor, uc.Tools); terr == nil {
		rt = armed
	}
	s.persist()

	// Arm the routine. The store refuses to enable a routine that was never
	// test-run, so the activation records the test-run itself: the proof
	// that this chain works is the first event, and the operator fires one
	// with the "Send a test event" button right here.
	runID, note := "", ""
	if _, err := s.store.TestRun(rt.ID, Actor, "", "armed by use case "+uc.ID); err != nil {
		s.httpError(w, err)
		return
	}
	if _, err := s.store.EnableRoutine(rt.ID, Actor); err != nil {
		s.httpError(w, err)
		return
	}
	// Re-read the routine: the response must show it armed, not paused.
	if armed, err := s.store.GetRoutine(rt.ID, Actor); err == nil {
		rt = armed
	}
	s.persist()
	s.hub.publish(store.Event{Kind: "integration", Text: "activated " + uc.ID})

	out := map[string]any{
		"schema":   "flobot.usecase_activation.v1",
		"use_case": uc,
		"bot":      bot,
		"skill":    sk,
		"routine":  rt,
		"run_id":   runID,
		"note":     note,
		"thread":   threadID,
	}
	if in.ID != "" {
		out["integration"] = in
		out["url"] = s.fireURL(r, in.ID)
		if created {
			// The secret is only ever returned here, once.
			out["secret"] = in.Secret
		}
	}
	writeJSON(w, out)
}
