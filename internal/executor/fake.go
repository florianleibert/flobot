package executor

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// WriteFakeBinary installs a tiny flocode stand-in that prints two NDJSON
// lines (content, done). Extra fields on the first line are dropped by parse.
func WriteFakeBinary(dir string) (string, error) {
	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, "flocode.bat")
		script := "@echo off\r\n" +
			"if defined FLOCODE_FAKE_ARGV (echo %* > %FLOCODE_FAKE_ARGV%)\r\n" +
			"echo {\"kind\":\"content\",\"turn_id\":1,\"text\":\"hello from fake\",\"args\":{\"command\":\"secret-tool-arg\"},\"extra\":true}\r\n" +
			"echo {\"kind\":\"done\",\"turn_id\":1}\r\n"
		return path, os.WriteFile(path, []byte(script), 0o755)
	}
	path := filepath.Join(dir, "flocode")
	script := `#!/bin/sh
if [ -n "$FLOCODE_FAKE_ARGV" ]; then
  printf '%s\n' "$0" "$@" > "$FLOCODE_FAKE_ARGV"
fi
if [ -n "$FLOCODE_FAKE_ENV" ]; then
  {
    if [ -n "$FLOCODE_LOG_LEVEL" ]; then printf 'FLOCODE_LOG_LEVEL=set\n'; fi
    if [ -n "$MODEL_API_KEY" ]; then printf 'MODEL_API_KEY=set\n'; fi
  } > "$FLOCODE_FAKE_ENV"
fi
if [ -n "$FLOCODE_FAKE_SLEEP" ]; then
  sleep "$FLOCODE_FAKE_SLEEP"
fi
printf '%s\n' '{"kind":"content","turn_id":1,"text":"hello from fake","args":{"command":"secret-tool-arg"},"extra":true}'
printf '%s\n' '{"kind":"done","turn_id":1}'
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		return "", fmt.Errorf("fake flocode: %w", err)
	}
	return path, nil
}
