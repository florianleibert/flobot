package schedule

import (
	"fmt"
	"strings"
	"time"
)

// CronSpec is a standard 5-field cron expression with @hourly/@daily/…
// aliases, used for triggers of the form "cron <expr>" (e.g. "cron 0 9 * * 1-5"
// fires at 09:00 on weekdays). Fields: minute hour day-of-month month
// day-of-week. Each field supports "*", "*/n", "a-b", "a-b/n", and comma
// lists. Interval triggers ("every 5m") stay the common case; cron is for
// firing on the clock (mornings, Mondays, month ends).
type CronSpec struct {
	Raw     string
	minute  []bool
	hour    []bool
	dom     []bool
	month   []bool
	dow     []bool
	domStar bool
	dowStar bool
}

var cronAliases = map[string]string{
	"@hourly":  "0 * * * *",
	"@daily":   "0 0 * * *",
	"@weekly":  "0 0 * * 0",
	"@monthly": "0 0 1 * *",
	"@yearly":  "0 0 1 1 *",
}

// ParseCron validates and compiles a 5-field expression (or alias). The
// caller has already stripped the leading "cron " keyword.
func ParseCron(expr string) (*CronSpec, error) {
	raw := strings.TrimSpace(expr)
	if a, ok := cronAliases[strings.ToLower(raw)]; ok {
		raw = a
	}
	fields := strings.Fields(raw)
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron %q: want 5 fields (minute hour dom month dow)", expr)
	}
	spec := &CronSpec{Raw: strings.TrimSpace(expr)}
	bounds := [][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 6}}
	out := make([][]bool, 5)
	for i, f := range fields {
		set, star, err := parseField(f, bounds[i][0], bounds[i][1])
		if err != nil {
			return nil, fmt.Errorf("cron %q field %d (%q): %v", expr, i+1, f, err)
		}
		out[i] = set
		if i == 2 {
			spec.domStar = star
		}
		if i == 4 {
			spec.dowStar = star
		}
	}
	spec.minute, spec.hour, spec.dom, spec.month, spec.dow = out[0], out[1], out[2], out[3], out[4]
	return spec, nil
}

// parseField expands one comma-separated field into a membership set over
// [lo,hi], and reports whether the field as a whole is unrestricted ("*" or
// "*/n"), which the day-of-month/day-of-week OR-rule needs.
func parseField(f string, lo, hi int) ([]bool, bool, error) {
	set := make([]bool, hi+1)
	star := f == "*" || strings.HasPrefix(f, "*/")
	for _, part := range strings.Split(f, ",") {
		step := 1
		if i := strings.Index(part, "/"); i >= 0 {
			if _, err := fmt.Sscanf(part[i+1:], "%d", &step); err != nil || step < 1 {
				return nil, false, fmt.Errorf("bad step %q", part)
			}
			part = part[:i]
		}
		a, b := lo, hi
		if part != "*" {
			if i := strings.Index(part, "-"); i > 0 {
				if _, err := fmt.Sscanf(part[:i], "%d", &a); err != nil {
					return nil, false, fmt.Errorf("bad range %q", part)
				}
				if _, err := fmt.Sscanf(part[i+1:], "%d", &b); err != nil {
					return nil, false, fmt.Errorf("bad range %q", part)
				}
			} else {
				if _, err := fmt.Sscanf(part, "%d", &a); err != nil {
					return nil, false, fmt.Errorf("bad value %q", part)
				}
				b = a
			}
		}
		if a < lo || b > hi || a > b {
			return nil, false, fmt.Errorf("value out of range %d-%d in %q", lo, hi, part)
		}
		for v := a; v <= b; v += step {
			set[v] = true
		}
	}
	return set, star, nil
}

// Next returns the first activation strictly after t (minute resolution,
// local time). It gives up after ~5 years of searching, so an impossible
// spec (Feb 30) returns ok=false instead of spinning forever.
func (c *CronSpec) Next(t time.Time) (time.Time, bool) {
	t = t.Truncate(time.Minute).Add(time.Minute)
	limit := t.Add(5 * 365 * 24 * time.Hour)
	for t.Before(limit) {
		if !c.month[int(t.Month())] || !c.dayMatch(t) {
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, t.Location())
			continue
		}
		if !c.hour[t.Hour()] {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, t.Location())
			continue
		}
		if !c.minute[t.Minute()] {
			t = t.Add(time.Minute)
			continue
		}
		return t, true
	}
	return time.Time{}, false
}

// dayMatch implements classic Vixie-cron OR semantics: when BOTH day-of-month
// and day-of-week are restricted, a day matches if either matches; when one
// is "*", both must match (so "dom" alone or "dow" alone still works).
func (c *CronSpec) dayMatch(t time.Time) bool {
	mDom := c.dom[t.Day()]
	mDow := c.dow[int(t.Weekday())]
	if c.domStar || c.dowStar {
		return mDom && mDow
	}
	return mDom || mDow
}
