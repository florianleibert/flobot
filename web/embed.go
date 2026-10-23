package web

import (
	_ "embed"
	"strings"
)

//go:embed index.html
var IndexHTML string

//go:embed app.js
var AppJS string

//go:embed styles.css
var StylesCSS string

// BundleTag marks where the production bundle script lives in index.html.
// The Go server serves the page with this tag intact; local vite dev swaps
// it for the module entry via IndexHTMLForLocalDev.
const BundleTag = `<!-- bundle:start --><script src="/app.js"></script><!-- bundle:end -->`

// IndexHTMLForLocalDev swaps the embedded production bundle tag for the
// vite module entry so the same embedded index.html also works under
// `vite dev`, where /src/main.js loads the sources live.
func IndexHTMLForLocalDev() string {
	devTag := "<script type=\"module\" src=\"/src/main.js\"></script>"
	return strings.Replace(IndexHTML, BundleTag, devTag, 1)
}
