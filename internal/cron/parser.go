package cron

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Schedule struct {
	Second   uint64
	Minute   uint64
	Hour     uint64
	Dom      uint64
	Month    uint64
	Dow      uint64
	Location *time.Location
	Interval time.Duration
}

func Parse(expr string, loc *time.Location) (*Schedule, error) {
	if loc == nil {
		loc = time.UTC
	}
	expr = strings.TrimSpace(strings.ToLower(expr))
	switch expr {
	case "@yearly", "@annually":
		return Parse("0 0 1 1 *", loc)
	case "@monthly":
		return Parse("0 0 1 * *", loc)
	case "@weekly":
		return Parse("0 0 * * 0", loc)
	case "@daily", "@midnight":
		return Parse("0 0 * * *", loc)
	case "@hourly":
		return Parse("0 * * * *", loc)
	}
	if strings.HasPrefix(expr, "@every ") {
		interval, err := time.ParseDuration(strings.TrimSpace(strings.TrimPrefix(expr, "@every ")))
		if err != nil {
			return nil, fmt.Errorf("parse @every duration: %w", err)
		}
		if interval <= 0 {
			return nil, fmt.Errorf("@every duration must be positive")
		}
		return &Schedule{Location: loc, Interval: interval}, nil
	}
	fields := strings.Fields(expr)
	switch len(fields) {
	case 5:
		fields = append([]string{"0"}, fields...)
	case 6:
	default:
		return nil, fmt.Errorf("invalid cron expression %q", expr)
	}
	var err error
	schedule := &Schedule{Location: loc}
	if schedule.Second, err = parseField(fields[0], 0, 59); err != nil {
		return nil, fmt.Errorf("second: %w", err)
	}
	if schedule.Minute, err = parseField(fields[1], 0, 59); err != nil {
		return nil, fmt.Errorf("minute: %w", err)
	}
	if schedule.Hour, err = parseField(fields[2], 0, 23); err != nil {
		return nil, fmt.Errorf("hour: %w", err)
	}
	if schedule.Dom, err = parseField(fields[3], 1, 31); err != nil {
		return nil, fmt.Errorf("day of month: %w", err)
	}
	if schedule.Month, err = parseField(normalizeNames(fields[4]), 1, 12); err != nil {
		return nil, fmt.Errorf("month: %w", err)
	}
	if schedule.Dow, err = parseField(normalizeNames(fields[5]), 0, 7); err != nil {
		return nil, fmt.Errorf("day of week: %w", err)
	}
	if schedule.Dow&(1<<7) != 0 {
		schedule.Dow |= 1
		schedule.Dow &^= 1 << 7
	}
	return schedule, nil
}

func (s *Schedule) IsInterval() bool {
	return s.Interval > 0
}

func (s *Schedule) IntervalDuration() time.Duration {
	return s.Interval
}

func (s *Schedule) Next(after time.Time) time.Time {
	if s == nil {
		return time.Time{}
	}
	if s.Interval > 0 {
		return after.Add(s.Interval)
	}
	loc := s.Location
	if loc == nil {
		loc = time.UTC
	}
	candidate := after.In(loc).Add(time.Second).Truncate(time.Second)
	limit := candidate.AddDate(4, 0, 0)
	for candidate.Before(limit) {
		if !bitSet(s.Month, int(candidate.Month())) {
			candidate = nextMonth(candidate)
			continue
		}
		if !bitSet(s.Dom, candidate.Day()) || !bitSet(s.Dow, int(candidate.Weekday())) {
			candidate = nextDay(candidate)
			continue
		}
		if !bitSet(s.Hour, candidate.Hour()) {
			candidate = nextHour(candidate)
			continue
		}
		if !bitSet(s.Minute, candidate.Minute()) {
			candidate = candidate.Add(time.Minute).Truncate(time.Minute)
			continue
		}
		if !bitSet(s.Second, candidate.Second()) {
			candidate = candidate.Add(time.Second)
			continue
		}
		return candidate
	}
	return time.Time{}
}

func parseField(field string, min int, max int) (uint64, error) {
	var mask uint64
	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		step := 1
		hasStep := false
		if before, after, ok := strings.Cut(part, "/"); ok {
			parsed, err := strconv.Atoi(after)
			if err != nil || parsed <= 0 {
				return 0, fmt.Errorf("invalid step %q", after)
			}
			step = parsed
			hasStep = true
			part = before
		}
		start, end, err := parseRange(part, min, max)
		if err != nil {
			return 0, err
		}
		// N/step runs N..max, like */step.
		if hasStep && part != "*" && !strings.Contains(part, "-") {
			end = max
		}
		for value := start; value <= end; value += step {
			mask |= 1 << uint(value)
		}
	}
	if mask == 0 {
		return 0, fmt.Errorf("empty field %q", field)
	}
	return mask, nil
}

func parseRange(part string, min int, max int) (int, int, error) {
	if part == "*" || part == "" {
		return min, max, nil
	}
	if startText, endText, ok := strings.Cut(part, "-"); ok {
		start, err := strconv.Atoi(startText)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid range start %q", startText)
		}
		end, err := strconv.Atoi(endText)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid range end %q", endText)
		}
		if start < min || end > max || start > end {
			return 0, 0, fmt.Errorf("range %d-%d outside %d-%d", start, end, min, max)
		}
		return start, end, nil
	}
	value, err := strconv.Atoi(part)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid value %q", part)
	}
	if value < min || value > max {
		return 0, 0, fmt.Errorf("value %d outside %d-%d", value, min, max)
	}
	return value, value, nil
}

func normalizeNames(value string) string {
	replacements := map[string]string{
		"jan": "1", "feb": "2", "mar": "3", "apr": "4", "may": "5", "jun": "6", "jul": "7", "aug": "8", "sep": "9", "oct": "10", "nov": "11", "dec": "12",
		"sun": "0", "mon": "1", "tue": "2", "wed": "3", "thu": "4", "fri": "5", "sat": "6",
	}
	for key, replacement := range replacements {
		value = strings.ReplaceAll(value, key, replacement)
	}
	return value
}

func bitSet(mask uint64, value int) bool {
	return mask&(1<<uint(value)) != 0
}

func nextMonth(value time.Time) time.Time {
	year := value.Year()
	month := value.Month() + 1
	if month > time.December {
		year++
		month = time.January
	}
	return time.Date(year, month, 1, 0, 0, 0, 0, value.Location())
}

func nextDay(value time.Time) time.Time {
	next := value.AddDate(0, 0, 1)
	return time.Date(next.Year(), next.Month(), next.Day(), 0, 0, 0, 0, value.Location())
}

func nextHour(value time.Time) time.Time {
	next := value.Add(time.Hour)
	return time.Date(next.Year(), next.Month(), next.Day(), next.Hour(), 0, 0, 0, value.Location())
}
