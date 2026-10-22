package amesh

import (
	"fmt"
	"strings"
)

// PinName is the flocode pin name for a mesh model. It is a valid TOML
// bare key segment (letters, digits, dash, dot) so the generated config
// parses with flocode's tiny TOML subset parser.
func PinName(modelID string) string {
	id := strings.TrimSpace(modelID)
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '.', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "mesh-model"
	}
	return b.String()
}

// ConfigTOML renders a flocode config that pins exactly one mesh model.
// The generated file carries no secrets: mesh endpoints are private-network
// OpenAI-compatible URLs and need no api_key_env. The profile is left unset
// so flocode falls through to generic-openai sniffing.
func ConfigTOML(m Model) string {
	var b strings.Builder
	b.WriteString("# flobot generated: amesh mesh model pin. No secrets.\n")
	b.WriteString("# route mesh/" + m.ID + "\n")
	fmt.Fprintf(&b, "default_model = %q\n\n", PinName(m.ID))
	fmt.Fprintf(&b, "[models.%s]\n", PinName(m.ID))
	b.WriteString("provider = \"openai-compat\"\n")
	fmt.Fprintf(&b, "model = %q\n", m.ID)
	fmt.Fprintf(&b, "base_url = %q\n", m.BaseURL)
	return b.String()
}
