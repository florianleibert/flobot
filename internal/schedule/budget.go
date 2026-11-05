// budget.go holds the three things that keep an unattended routine from
// becoming an unattended problem: a timeout split that can say "wait at most
// this long to start, run at most this long per attempt, give up after this
// long in total"; a per-routine rate limit; and a recursion guard so a fire
// cannot spawn a fire forever.
//
// One timeout number cannot express those three bounds (round 22 §3), and an
// unbounded chain of fires is how an automation bills you for a loop.
package schedule

import (
	"fmt"
	"sync"
	"time"
)

// Budget is the timeout split for one fire.
//
// The three values answer three different questions:
//
//   - QueueWait (schedule-to-start): how long may a due occurrence sit before
//     it starts? Expiring here is NOT retryable — retrying puts the work back
//     into the same backed-up queue.
//   - Attempt (start-to-close): how long may one attempt run?
//   - Total (schedule-to-close): how long may the whole fire take, retries
//     included? This bounds duration without changing the attempt count.
type Budget struct {
	QueueWait time.Duration
	Attempt   time.Duration
	Total     time.Duration
}

const (
	// DefaultQueueWait bounds how long a due routine may wait to start.
	DefaultQueueWait = 30 * time.Second
	// DefaultAttempt bounds one attempt. It matches executor.DefaultTimeout,
	// so a routine's attempt is no longer than an interactive run's.
	DefaultAttempt = 10 * time.Minute
	// DefaultTotal bounds the whole fire, retries included.
	DefaultTotal = 30 * time.Minute
)

// DefaultBudget is the budget a routine gets when it declares none.
func DefaultBudget() Budget {
	return Budget{QueueWait: DefaultQueueWait, Attempt: DefaultAttempt, Total: DefaultTotal}
}

// normalized fills zero fields with their defaults so a partially-specified
// budget cannot mean "unbounded".
func (b Budget) normalized() Budget {
	if b.QueueWait <= 0 {
		b.QueueWait = DefaultQueueWait
	}
	if b.Attempt <= 0 {
		b.Attempt = DefaultAttempt
	}
	if b.Total <= 0 {
		b.Total = DefaultTotal
	}
	return b
}

// ErrQueueWaitExpired means the occurrence waited too long to start. It is
// deliberately not retryable: re-enqueuing lands in the same queue.
var ErrQueueWaitExpired = fmt.Errorf("fire waited longer than its queue-wait budget")

// ErrBudgetSpent means the fire's total budget is gone; no further attempt.
var ErrBudgetSpent = fmt.Errorf("fire spent its total budget")

// AttemptTimeout returns how long the next attempt may run, given how long the
// fire waited to start and how much of its total budget is already spent.
//
// It returns an error rather than a zero duration so an expired budget is a
// decision the caller records, never a silent instant timeout.
func (b Budget) AttemptTimeout(waited, spent time.Duration) (time.Duration, error) {
	b = b.normalized()
	if waited > b.QueueWait {
		return 0, ErrQueueWaitExpired
	}
	remaining := b.Total - spent
	if remaining <= 0 {
		return 0, ErrBudgetSpent
	}
	if remaining < b.Attempt {
		return remaining, nil
	}
	return b.Attempt, nil
}

// Limiter is a per-key sliding-window rate limit. A routine that fires on every
// inbound webhook needs one: a provider retry storm or a misconfigured sender
// must not turn into a hundred agent runs.
type Limiter struct {
	max    int
	window time.Duration

	mu   sync.Mutex
	hits map[string][]time.Time
}

// NewLimiter returns a limiter allowing max hits per key per window. A max of 0
// or less means 12 per hour; a window of 0 or less means one hour.
func NewLimiter(max int, window time.Duration) *Limiter {
	if max <= 0 {
		max = 12
	}
	if window <= 0 {
		window = time.Hour
	}
	return &Limiter{max: max, window: window, hits: make(map[string][]time.Time)}
}

// Allow records a hit for key and reports whether it is inside the limit.
// A refused hit is not recorded, so a burst cannot push the window forward.
func (l *Limiter) Allow(key string, now time.Time) bool {
	if l == nil || key == "" {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Add(-l.window)
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	return true
}

// MaxDepth is how deep a chain of fires may go. A fire caused by a run that was
// itself caused by a fire is depth 2; the third is refused.
const MaxDepth = 3

// ErrTooDeep means a fire would continue a chain that is already at MaxDepth.
var ErrTooDeep = fmt.Errorf("fire chain is too deep (recursion guard)")

// Recursion refuses a fire that would continue an existing chain of fires.
//
// The failure it prevents is concrete: a routine whose run triggers the same
// routine fires forever, and each level spends tokens.
type Recursion struct {
	mu    sync.Mutex
	depth map[string]int
}

// NewRecursion returns an empty recursion guard.
func NewRecursion() *Recursion { return &Recursion{depth: make(map[string]int)} }

// Enter records one level for key and reports whether it may proceed. A caller
// that gets nil MUST call Leave when its fire finishes.
func (r *Recursion) Enter(key string) error {
	if r == nil || key == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.depth[key] >= MaxDepth {
		return ErrTooDeep
	}
	r.depth[key]++
	return nil
}

// Leave releases one level recorded by Enter.
func (r *Recursion) Leave(key string) {
	if r == nil || key == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.depth[key] <= 1 {
		delete(r.depth, key)
		return
	}
	r.depth[key]--
}

// Depth reports the current depth for key (tests and diagnostics).
func (r *Recursion) Depth(key string) int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.depth[key]
}

// EventFireID is the occurrence identity of an event-triggered fire: the
// routine plus the provider's delivery id.
//
// A webhook provider retries a delivery it did not see acknowledged, and it
// sends the same delivery id each time. That id — not the arrival instant — is
// what makes two deliveries one occurrence. An empty delivery id means the
// provider gave us nothing to dedupe on, so there is no identity and the fire
// runs (a missing id must not silently swallow real events).
func EventFireID(routineID, deliveryID string) string {
	if routineID == "" || deliveryID == "" {
		return ""
	}
	return routineID + "#" + deliveryID
}
