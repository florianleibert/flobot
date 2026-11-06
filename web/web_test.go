// Package web tests the static UI assets. The UI is plain HTML/CSS/JS
// served by the loopback server, so the tests assert on the embedded
// sources: every NDJSON kind must be rendered, the stream endpoint must
// be used, and no secret material may ever reach the page.
package web

import (
	"regexp"
	"strings"
	"testing"
)

// allKinds are the NDJSON kinds the UI must render (T-webui acceptance).
var allKinds = []string{
	"content", "thinking", "tool_start", "tool_end",
	"perm", "plan", "err", "done", "verify",
}

func TestIndexReferencesAppAndStyles(t *testing.T) {
	if !strings.Contains(IndexHTML, `src="/app.js"`) {
		t.Errorf("index.html does not load /app.js")
	}
	if !strings.Contains(IndexHTML, `href="/styles.css"`) {
		t.Errorf("index.html does not load /styles.css")
	}
}

func TestLocalDevIndexSwapsBundleForModule(t *testing.T) {
	dev := IndexHTMLForLocalDev()
	if strings.Contains(dev, BundleTag) {
		t.Errorf("dev index still carries the production bundle tag")
	}
	if !strings.Contains(dev, `<script type="module" src="/src/main.js"></script>`) {
		t.Errorf("dev index does not load the vite module entry")
	}
}

func TestAppConnectsToStream(t *testing.T) {
	if !strings.Contains(AppJS, "/api/stream") {
		t.Errorf("app.js does not connect to /api/stream")
	}
}

func TestAppRendersAllKinds(t *testing.T) {
	for _, k := range allKinds {
		if !strings.Contains(AppJS, k) {
			t.Errorf("app.js does not handle kind %q", k)
		}
	}
}

func TestAppSanitizesSecrets(t *testing.T) {
	// The sanitizer must mask the documented key file and any
	// MODEL_API_KEY material before it can be rendered.
	for _, needle := range []string{"MODEL_API_KEY", "meta.ai"} {
		if !strings.Contains(AppJS, needle) {
			t.Errorf("app.js sanitizer does not reference %q", needle)
		}
	}
}

func TestAppNeverEmbedsSecretMaterial(t *testing.T) {
	// No API key, token, or credential value may be baked into the UI.
	for _, f := range []string{IndexHTML, AppJS, StylesCSS} {
		if regexp.MustCompile(`(?i)(api[_-]?key|token|secret|password)\s*[:=]\s*["'][A-Za-z0-9_\-]{8,}["']`).MatchString(f) {
			t.Errorf("asset appears to embed a secret-looking literal")
		}
	}
}

func TestStylesStyleAllKinds(t *testing.T) {
	for _, k := range allKinds {
		if !strings.Contains(StylesCSS, k) {
			t.Errorf("styles.css has no rule for kind %q", k)
		}
	}
}

func TestIndexHasHubsPanel(t *testing.T) {
	if !strings.Contains(IndexHTML, `data-panel="hubs"`) {
		t.Errorf("index.html has no Hubs nav button")
	}
	if !strings.Contains(IndexHTML, `id="panel-hubs"`) {
		t.Errorf("index.html has no Hubs panel section")
	}
	if !strings.Contains(IndexHTML, `id="hubList"`) || !strings.Contains(IndexHTML, `id="hubModels"`) {
		t.Errorf("index.html Hubs panel is missing hub/model list hosts")
	}
}

func TestAppWiresHubDiscovery(t *testing.T) {
	for _, needle := range []string{"/api/v1/hubs", "/api/v1/models/", "loadHubs", "startModel", "data-start"} {
		if !strings.Contains(AppJS, needle) {
			t.Errorf("app.js does not reference %q", needle)
		}
	}
}

func TestStylesStyleHubPanel(t *testing.T) {
	for _, sel := range []string{".hub-card", ".hub-model", ".hub-list", ".hub-models"} {
		if !strings.Contains(StylesCSS, sel) {
			t.Errorf("styles.css has no rule for %q", sel)
		}
	}
}

func TestIndexHasIntegrationsPanel(t *testing.T) {
	if !strings.Contains(IndexHTML, `data-panel="integrations"`) {
		t.Errorf("index.html has no Integrations nav button")
	}
	if !strings.Contains(IndexHTML, `id="panel-integrations"`) {
		t.Errorf("index.html has no Integrations panel section")
	}
	for _, host := range []string{`id="integrations"`, `id="intGallery"`, `id="intEvents"`, `id="intSheet"`, `id="intCount"`} {
		if !strings.Contains(IndexHTML, host) {
			t.Errorf("index.html Integrations panel is missing %s", host)
		}
	}
}

func TestAppWiresIntegrations(t *testing.T) {
	for _, needle := range []string{
		"/api/v1/integrations", "/api/v1/integrations/providers",
		"refreshInt", "renderIntegrations", "renderIntGallery", "renderIntEvents",
		"createInt", "data-int-bind", "data-int-test", "data-int-rotate",
		"data-int-connect", "data-int-copyurl",
	} {
		if !strings.Contains(AppJS, needle) {
			t.Errorf("app.js does not reference %q", needle)
		}
	}
}

func TestStylesStyleIntegrations(t *testing.T) {
	for _, sel := range []string{
		".int-gallery", ".int-tile", ".int-card", ".int-grid", ".int-events",
		".int-ev", ".int-sheet", ".int-snippet", ".int-steps",
	} {
		if !strings.Contains(StylesCSS, sel) {
			t.Errorf("styles.css has no rule for %q", sel)
		}
	}
}

func TestIndexHasUseCaseGallery(t *testing.T) {
	if !strings.Contains(IndexHTML, `id="ucGallery"`) {
		t.Errorf("index.html has no use-case gallery")
	}
}

func TestAppWiresUseCases(t *testing.T) {
	for _, needle := range []string{
		"/api/v1/usecases", "renderIntUseCases", "openUcSheet", "ucActivate",
		"data-uc", "data-uc-test",
	} {
		if !strings.Contains(AppJS, needle) {
			t.Errorf("app.js does not reference %q", needle)
		}
	}
}

func TestStylesStyleUseCases(t *testing.T) {
	for _, sel := range []string{".uc-tile", ".int-tile-tags"} {
		if !strings.Contains(StylesCSS, sel) {
			t.Errorf("styles.css has no rule for %q", sel)
		}
	}
}
// TestAppRoutesRunEndThroughDesktopBridge pins the notification wiring.
//
// The main process owns coalescing, the dock badge and the tray count, and it
// only hears about a finished run through the preload bridge. A renderer that
// calls the browser Notification API directly bypasses all three, which is
// exactly the defect this test exists to prevent: the coalescing code was
// reachable, correct and never called.
func TestAppRoutesRunEndThroughDesktopBridge(t *testing.T) {
	for _, needle := range []string{"flobotDesktop", "notifyRunEnd"} {
		if !strings.Contains(AppJS, needle) {
			t.Errorf("app.js does not reference %q; run-end never reaches the main process", needle)
		}
	}
	// The bridge must be preferred over the browser API, not merely present.
	bridge := strings.Index(AppJS, "notifyRunEnd")
	browser := strings.Index(AppJS, "Notification.permission")
	if bridge < 0 || browser < 0 {
		t.Fatalf("missing bridge (%d) or browser fallback (%d)", bridge, browser)
	}
	if bridge > browser {
		t.Errorf("app.js consults the browser Notification API before the desktop bridge")
	}
}

// TestAppResumesStreamsFromSeq pins the resume contract on both stream readers.
//
// Every reader must send ?since= and must take the offset from the event's seq.
// A reader that resumes from a count of frames it has rendered indexes the
// frame sequence with a number from the event sequence: the hub coalesces
// deltas, so after an answer streams the replay starts too early and re-renders
// events the user already saw.
func TestAppResumesStreamsFromSeq(t *testing.T) {
	if n := strings.Count(AppJS, "since="); n < 2 {
		t.Errorf("app.js builds a ?since= resume URL %d time(s), want at least 2 (main feed and Code rail)", n)
	}
	if !strings.Contains(AppJS, "ev.seq") {
		t.Errorf("app.js never reads ev.seq; the resume offset is not in the event sequence")
	}
	// A frame-counting offset is the defect: it must not be what is sent.
	if regexp.MustCompile(`since="\s*\+\s*streamSeen\s*\+\s*"`).MatchString(AppJS) &&
		!strings.Contains(AppJS, "ev.seq > streamSeen") {
		t.Errorf("app.js counts frames for its resume offset instead of tracking seq")
	}
}

// TestAppFiltersHeartbeatsOnEveryReader pins that keepalives never render.
//
// The server sends heartbeats so an idle connection is not mistaken for a dead
// one. A reader that renders them puts a heartbeat row in the log every few
// seconds; a reader that treats one as data can also mistake it for progress.
func TestAppFiltersHeartbeatsOnEveryReader(t *testing.T) {
	if n := strings.Count(AppJS, `"heartbeat"`); n < 2 {
		t.Errorf("app.js filters heartbeats in %d place(s), want at least 2 (main feed and Code rail)", n)
	}
}

// TestAppHandlesStreamDropped pins that a truncation is not read as completion.
//
// When a client falls too far behind the server gives up and says so. A reader
// that ignores the frame shows a half answer as if the run had finished.
func TestAppHandlesStreamDropped(t *testing.T) {
	if !strings.Contains(AppJS, "stream_dropped") {
		t.Errorf("app.js does not handle stream_dropped; a truncation would render as completion")
	}
}

// TestBundleIsBuiltFromSource guards against editing the generated bundle.
//
// web/app.js is produced by esbuild from flobot-ui-redesign/src. Fixes applied
// straight to the bundle are silently reverted by the next sync — which is how
// the stream resume work was lost once already. The bundle carries a banner
// naming its source so this stays discoverable.
func TestBundleIsBuiltFromSource(t *testing.T) {
	if !strings.Contains(AppJS, "flobot-ui-redesign") {
		t.Errorf("app.js has no banner naming its source project; edits to it are lost on the next sync")
	}
}
