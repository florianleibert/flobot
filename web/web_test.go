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
