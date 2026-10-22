// Package taskclient talks to a running `flocode serve` task API
// (POST /api/v1/tasks, GET status/stream/result, POST cancel) and
// translates its SSE events into flobot run events. It is selected by
// FLOBOT_BACKEND=taskclient; the child-process executor remains the
// default and the fallback.
package taskclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is a task API client. Base is e.g. http://127.0.0.1:7777.
type Client struct {
	base string
	hc   *http.Client
}

// New returns a client for base.
func New(base string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{base: strings.TrimRight(base, "/"), hc: hc}
}

// SubmitResult is the 202 response from POST /api/v1/tasks.
type SubmitResult struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
}

// Status is the GET /api/v1/tasks/{id} response.
type Status struct {
	TaskID        string `json:"task_id"`
	Status        string `json:"status"`
	StartedAt     any    `json:"started_at"`
	EndedAt       any    `json:"ended_at"`
	Error         string `json:"error"`
	DroppedEvents int    `json:"dropped_events"`
}

// Event is one SSE event from the stream.
type Event struct {
	Type string          `json:"-"`
	Data json.RawMessage `json:"-"`
}

// Result is the task result envelope (subset flobot consumes).
type Result struct {
	TaskID              string          `json:"task_id"`
	Status              string          `json:"status"`
	Summary             string          `json:"summary"`
	Patch               PatchInfo       `json:"patch"`
	Tests               json.RawMessage `json:"tests"`
	Provenance          json.RawMessage `json:"provenance,omitempty"`
	Confidence          float64         `json:"confidence"`
	RequiresHumanReview bool            `json:"requires_human_review"`
	Error               string          `json:"error,omitempty"`
}

// PatchInfo mirrors flocode's patch envelope.
type PatchInfo struct {
	Format       string   `json:"format"`
	SHA256       string   `json:"sha256"`
	ChangedFiles []string `json:"changed_files"`
	Diff         string   `json:"diff"`
	Applied      bool     `json:"applied"`
}

// Submit posts a task body (a task.Task JSON object).
func (c *Client) Submit(ctx context.Context, taskBody any) (SubmitResult, error) {
	raw, err := json.Marshal(taskBody)
	if err != nil {
		return SubmitResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/v1/tasks", bytes.NewReader(raw))
	if err != nil {
		return SubmitResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return SubmitResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		return SubmitResult{}, fmt.Errorf("taskclient: submit: %s", resp.Status)
	}
	var out SubmitResult
	return out, json.NewDecoder(resp.Body).Decode(&out)
}

// Status fetches one task's status.
func (c *Client) Status(ctx context.Context, id string) (Status, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/v1/tasks/"+id, nil)
	if err != nil {
		return Status{}, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return Status{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Status{}, fmt.Errorf("taskclient: status: %s", resp.Status)
	}
	var st Status
	return st, json.NewDecoder(resp.Body).Decode(&st)
}

// Cancel cancels a running task.
func (c *Client) Cancel(ctx context.Context, id string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/v1/tasks/"+id+"/cancel", nil)
	if err != nil {
		return err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("taskclient: cancel: %s", resp.Status)
	}
	return nil
}

// Result fetches the terminal result envelope.
func (c *Client) Result(ctx context.Context, id string) (*Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/v1/tasks/"+id+"/result", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("taskclient: result: %s", resp.Status)
	}
	var res Result
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	return &res, nil
}

// Stream subscribes to the SSE stream. There is no replay: a reconnect
// starts a fresh subscription; the caller must poll status and render
// from the result instead of assuming a resumed stream. The channel
// closes when the stream ends (terminal event, server close, or ctx).
func (c *Client) Stream(ctx context.Context, id string) (<-chan Event, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/v1/tasks/"+id+"/stream", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	// Stream responses have no timeout; the ctx bounds them.
	streamHC := &http.Client{}
	resp, err := streamHC.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("taskclient: stream: %s", resp.Status)
	}
	ch := make(chan Event, 64)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		ev := Event{}
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "event:"):
				ev.Type = strings.TrimSpace(line[6:])
			case strings.HasPrefix(line, "data:"):
				ev.Data = json.RawMessage(strings.TrimSpace(line[5:]))
			case line == "":
				if ev.Type != "" || len(ev.Data) > 0 {
					ch <- ev
					ev = Event{}
				}
			}
			if ctx.Err() != nil {
				return
			}
		}
	}()
	return ch, nil
}

// Terminal reports whether a task status is terminal.
func Terminal(status string) bool {
	switch status {
	case "completed", "cancelled", "failed":
		return true
	}
	return false
}

var _ = io.EOF
