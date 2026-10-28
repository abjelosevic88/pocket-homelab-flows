package flow

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-flows/internal/store"
)

// CheckSchedule validates a schedule trigger and fills in defaults.
func CheckSchedule(t *store.Trigger) error {
	if t.Time == "" {
		t.Time = "08:00"
	}
	if _, _, err := clock(t.Time); err != nil {
		return err
	}
	switch t.Repeat {
	case "minutes":
		if t.Every == 0 {
			t.Every = 15
		}
		if t.Every < 5 || t.Every > 720 {
			return errors.New("every 5 to 720 minutes")
		}
	case "hourly":
		if t.Every == 0 {
			t.Every = 1
		}
		if t.Every < 1 || t.Every > 24 {
			return errors.New("every 1 to 24 hours")
		}
	case "daily":
	case "weekly":
		d := ""
		for _, c := range "1234567" {
			if strings.ContainsRune(t.Days, c) {
				d += string(c)
			}
		}
		if d == "" {
			return errors.New("pick at least one day")
		}
		t.Days = d
	case "monthly":
		if t.DayOfMonth == 0 {
			t.DayOfMonth = 1
		}
		if t.DayOfMonth < 1 || t.DayOfMonth > 31 {
			return errors.New("day of the month is 1 to 31")
		}
	case "once":
		if _, err := time.ParseInLocation("2006-01-02T15:04", t.At, time.Local); err != nil {
			return errors.New("pick a date and time")
		}
	default:
		return errors.New("repeat is minutes, hourly, daily, weekly, monthly or once")
	}
	return nil
}

func clock(s string) (int, int, error) {
	h, m, ok := strings.Cut(s, ":")
	hh, e1 := strconv.Atoi(h)
	mm, e2 := strconv.Atoi(m)
	if !ok || e1 != nil || e2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, 0, errors.New("time is HH:MM")
	}
	return hh, mm, nil
}

// Next is the first time strictly after `after` that the schedule runs, in
// loc. ok=false means never again (a "once" that has passed).
func Next(t store.Trigger, after time.Time, loc *time.Location) (time.Time, bool) {
	if loc == nil {
		loc = time.Local
	}
	a := after.In(loc)
	hh, mm, err := clock(t.Time)
	if err != nil {
		hh, mm = 8, 0
	}
	at := func(d time.Time) time.Time { return time.Date(d.Year(), d.Month(), d.Day(), hh, mm, 0, 0, loc) }
	switch t.Repeat {
	case "minutes":
		every := time.Duration(max(t.Every, 5)) * time.Minute
		// aligned to the clock: every 15 minutes = :00, :15, :30, :45
		day := time.Date(a.Year(), a.Month(), a.Day(), 0, 0, 0, 0, loc)
		n := a.Sub(day)/every + 1
		next := day.Add(n * every)
		if next.Day() != a.Day() {
			next = day.AddDate(0, 0, 1)
		}
		return next, true
	case "hourly":
		every := max(t.Every, 1)
		day := time.Date(a.Year(), a.Month(), a.Day(), 0, mm, 0, 0, loc)
		for h := 0; h < 48; h += every {
			c := day.Add(time.Duration(h) * time.Hour)
			if c.After(a) {
				return c, true
			}
		}
		return day.AddDate(0, 0, 1), true
	case "daily":
		c := at(a)
		if !c.After(a) {
			c = at(a.AddDate(0, 0, 1))
		}
		return c, true
	case "weekly":
		for i := 0; i < 8; i++ {
			d := a.AddDate(0, 0, i)
			wd := int(d.Weekday())
			if wd == 0 {
				wd = 7
			}
			if strings.ContainsRune(t.Days, rune('0'+wd)) {
				if c := at(d); c.After(a) {
					return c, true
				}
			}
		}
		return time.Time{}, false
	case "monthly":
		for i := 0; i < 3; i++ {
			first := time.Date(a.Year(), a.Month()+time.Month(i), 1, 0, 0, 0, 0, loc)
			last := first.AddDate(0, 1, -1).Day()
			c := at(first.AddDate(0, 0, min(t.DayOfMonth, last)-1))
			if c.After(a) {
				return c, true
			}
		}
		return time.Time{}, false
	case "once":
		c, err := time.ParseInLocation("2006-01-02T15:04", t.At, loc)
		if err != nil || !c.After(a) {
			return time.Time{}, false
		}
		return c, true
	}
	return time.Time{}, false
}

var weekdayNames = []string{"", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"}

// DescribeSchedule says a schedule in words: "Every weekday at 08:00".
func DescribeSchedule(t store.Trigger) string {
	switch t.Repeat {
	case "minutes":
		return fmt.Sprintf("Every %d minutes", t.Every)
	case "hourly":
		if t.Every <= 1 {
			return "Every hour"
		}
		return fmt.Sprintf("Every %d hours", t.Every)
	case "daily":
		return "Every day at " + t.Time
	case "weekly":
		switch t.Days {
		case "1234567":
			return "Every day at " + t.Time
		case "12345":
			return "Every weekday at " + t.Time
		case "67":
			return "Weekends at " + t.Time
		}
		var names []string
		for _, c := range t.Days {
			names = append(names, weekdayNames[c-'0'][:3])
		}
		return "Every " + strings.Join(names, ", ") + " at " + t.Time
	case "monthly":
		return fmt.Sprintf("Monthly on day %d at %s", t.DayOfMonth, t.Time)
	case "once":
		return "Once, " + strings.Replace(t.At, "T", " at ", 1)
	}
	return "On a schedule"
}
