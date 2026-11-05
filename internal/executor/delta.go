package executor

import "strings"

// KindDelta is a token-level fragment of one turn's answer.
//
// The child used to emit only message-granular `content` events, so a long
// answer looked like a hang: nothing arrived for tens of seconds and then the
// whole message appeared at once. A `delta` is the same text split into
// fragments, emitted as it is produced.
//
// A delta is never a complete answer on its own and never replaces `content`:
// the final `content` event still carries the whole message, so a consumer that
// ignores deltas stays correct, and a consumer that renders deltas gets a live
// transcript.
const KindDelta = "delta"

// CoalesceDeltas merges consecutive deltas of the same turn into one event.
//
// One frame per fragment would be thousands of frames per answer, so the
// fragments are batched before they reach a subscriber. Consecutive matters:
// two turns interleaved must not be concatenated into one string, or the
// transcript would show one turn's text inside another's.
//
// Non-delta events pass through unchanged and break a run of deltas.
func CoalesceDeltas(in []Event) []Event {
	if len(in) == 0 {
		return nil
	}
	out := make([]Event, 0, len(in))
	for _, ev := range in {
		if n := len(out); n > 0 && ev.Kind == KindDelta && out[n-1].Kind == KindDelta && out[n-1].TurnID == ev.TurnID {
			out[n-1].Text += ev.Text
			continue
		}
		out = append(out, ev)
	}
	return out
}

// TurnText reconstructs one turn's answer from its events.
//
// A delta stream is authoritative while it lasts: if a turn produced deltas,
// their concatenation is the answer, because a consumer that rendered them
// already showed that text. A turn with no deltas falls back to its `content`
// events, which is what every run before this contract produced.
func TurnText(events []Event, turnID uint64) string {
	var deltas, content strings.Builder
	for _, ev := range events {
		if ev.TurnID != turnID {
			continue
		}
		switch ev.Kind {
		case KindDelta:
			deltas.WriteString(ev.Text)
		case "content":
			content.WriteString(ev.Text)
		}
	}
	if deltas.Len() > 0 {
		return deltas.String()
	}
	return content.String()
}
