// Package backup snapshots the database and the notes into one archive and
// puts it somewhere else: a directory, an S3 bucket. Everything here is a
// one-shot; the schedule is the system's (a systemd user timer, a crontab
// line), never a daemon of pecunia's own.
package backup

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Every is how often a backup runs: N times a day or N times a week. The
// owner says "2/day"; the timer gets the hours worked out from it.
type Every struct {
	N   int
	Per string // "day" or "week"
}

// firstHour is when the first run of the day lands. 03:00 rather than midnight:
// the machine is idle, and a nightly reboot or a laptop closed at 23:00 is
// less likely to swallow it.
const firstHour = 3

var weekdays = []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}

// cronDays is the crontab number for each of weekdays: cron counts from Sunday.
var cronDays = []string{"1", "2", "3", "4", "5", "6", "0"}

// ParseEvery reads "N/day", "N/week", "daily" or "weekly".
func ParseEvery(s string) (Every, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "":
		return Every{}, errors.New("empty schedule — want N/day or N/week")
	case "daily":
		return Every{1, "day"}, nil
	case "weekly":
		return Every{1, "week"}, nil
	}
	n, per, ok := strings.Cut(s, "/")
	if !ok {
		return Every{}, fmt.Errorf("schedule %q — want N/day or N/week", s)
	}
	count, err := strconv.Atoi(strings.TrimSpace(n))
	if err != nil {
		return Every{}, fmt.Errorf("schedule %q — %q is not a number", s, strings.TrimSpace(n))
	}
	e := Every{count, strings.TrimSpace(per)}
	switch e.Per {
	case "day":
		if e.N > 24 {
			return Every{}, fmt.Errorf("schedule %q — at most 24 a day", s)
		}
	case "week":
		if e.N > 7 {
			return Every{}, fmt.Errorf("schedule %q — at most 7 a week", s)
		}
	default:
		return Every{}, fmt.Errorf("schedule %q — per day or week, not %q", s, e.Per)
	}
	if e.N < 1 {
		return Every{}, fmt.Errorf("schedule %q — at least 1", s)
	}
	return e, nil
}

func (e Every) String() string { return fmt.Sprintf("%d/%s", e.N, e.Per) }

// hours are the run hours of a day, N of them spread evenly from firstHour.
func (e Every) hours() []int {
	if e.Per != "day" {
		return []int{firstHour}
	}
	hs := make([]int, e.N)
	for i := range hs {
		hs[i] = (firstHour + i*24/e.N) % 24
	}
	return hs
}

// days are the indexes into weekdays a weekly schedule runs on, N of them
// spread evenly from Monday. A daily schedule runs every day.
func (e Every) days() []int {
	if e.Per != "week" {
		return nil
	}
	ds := make([]int, e.N)
	for i := range ds {
		ds[i] = i * 7 / e.N
	}
	return ds
}

// OnCalendar is the schedule as systemd.time(7) writes it.
func (e Every) OnCalendar() string {
	var hs []string
	for _, h := range e.hours() {
		hs = append(hs, fmt.Sprintf("%02d", h))
	}
	cal := fmt.Sprintf("*-*-* %s:00:00", strings.Join(hs, ","))
	if ds := e.days(); ds != nil {
		var names []string
		for _, d := range ds {
			names = append(names, weekdays[d])
		}
		cal = strings.Join(names, ",") + " " + cal
	}
	return cal
}

// Cron is the same schedule as the five fields of a crontab line.
func (e Every) Cron() string {
	var hs []string
	for _, h := range e.hours() {
		hs = append(hs, strconv.Itoa(h))
	}
	dow := "*"
	if ds := e.days(); ds != nil {
		var nums []string
		for _, d := range ds {
			nums = append(nums, cronDays[d])
		}
		dow = strings.Join(nums, ",")
	}
	return fmt.Sprintf("0 %s * * %s", strings.Join(hs, ","), dow)
}
