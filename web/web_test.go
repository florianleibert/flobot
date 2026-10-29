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
