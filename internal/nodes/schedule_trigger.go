package nodes

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

func (ScheduleTrigger) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	if len(in.InputData) > 0 && len(in.InputData[0]) > 0 {
		return dataplane.MainOutput(in.InputData[0]), nil
	}
	location := scheduleLocation(in.Node.Parameters)
	now := time.Now().In(location)
	return dataplane.MainOutput([]dataplane.Item{ScheduledTriggerItem(in.Node, now)}), nil
}

func ScheduledTriggerItem(node dataplane.Node, at time.Time) dataplane.Item {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	local := at
	_, offset := local.Zone()
	offsetSign := "+"
	if offset < 0 {
		offsetSign = "-"
		offset = -offset
	}
	offsetHours := offset / 3600
	offsetMinutes := (offset % 3600) / 60
	return dataplane.Item{JSON: map[string]any{
		"timestamp":     local.Format("2006-01-02T15:04:05.000-07:00"),
		"Readable date": fmt.Sprintf("%s %s %04d, %s", local.Month().String(), ordinalScheduleDay(local.Day()), local.Year(), strings.ToLower(local.Format("3:04:05 PM"))),
		"Readable time": strings.ToLower(local.Format("3:04:05 PM")),
		"Day of week":   local.Weekday().String(),
		"Year":          local.Format("2006"),
		"Month":         local.Month().String(),
		"Day of month":  local.Format("02"),
		"Hour":          local.Format("15"),
		"Minute":        local.Format("04"),
		"Second":        local.Format("05"),
		"Timezone":      fmt.Sprintf("%s (UTC%s%02d:%02d)", local.Location().String(), offsetSign, offsetHours, offsetMinutes),
	}}
}

func ordinalScheduleDay(day int) string {
	if day%100 >= 11 && day%100 <= 13 {
		return fmt.Sprintf("%dth", day)
	}
	switch day % 10 {
	case 1:
		return fmt.Sprintf("%dst", day)
	case 2:
		return fmt.Sprintf("%dnd", day)
	case 3:
		return fmt.Sprintf("%drd", day)
	default:
		return fmt.Sprintf("%dth", day)
	}
}

func BuildScheduleCronExpression(parameters map[string]any) (string, error) {
	rule := scheduleRuleMap(parameters)
	if expr := strings.TrimSpace(stringParam(rule, "cronExpression", "expression")); expr != "" {
		fields := strings.Fields(expr)
		if len(fields) == 5 {
			return "0 " + expr, nil
		}
		if len(fields) == 6 || strings.HasPrefix(expr, "@") {
			return expr, nil
		}
		return "", fmt.Errorf("invalid cron expression %q", expr)
	}
	if interval, ok := rule["interval"]; ok {
		if expr, err := intervalCronExpression(interval); expr != "" || err != nil {
			return expr, err
		}
	}
	if mode := strings.TrimSpace(strings.ToLower(stringParam(rule, "mode"))); mode == "everyx" || mode == "interval" {
		if expr, err := intervalCronExpression(rule); expr != "" || err != nil {
			return expr, err
		}
	}
	hour, err := boundedScheduleInt(rule, 0, 23, 0, "triggerAtHour", "hour")
	if err != nil {
		return "", err
	}
	minute, err := boundedScheduleInt(rule, 0, 59, 0, "triggerAtMinute", "minute")
	if err != nil {
		return "", err
	}
	day := "*"
	weekday := "*"
	if value, ok := firstScheduleValue(rule, "triggerAtDay", "day", "weekday"); ok {
		day, weekday, err = scheduleDayFields(value)
		if err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("0 %d %d %s * %s", minute, hour, day, weekday), nil
}

func ScheduleTimezone(parameters map[string]any) string {
	rule := scheduleRuleMap(parameters)
	return strings.TrimSpace(stringParam(rule, "timezone", "timeZone"))
}

func scheduleLocation(parameters map[string]any) *time.Location {
	timezone := ScheduleTimezone(parameters)
	if timezone == "" {
		return time.UTC
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return time.UTC
	}
	return location
}

func scheduleRuleMap(parameters map[string]any) map[string]any {
	if parameters == nil {
		return map[string]any{}
	}
	if raw, ok := parameters["rule"]; ok {
		if rule, ok := rawObject(raw); ok {
			return rule
		}
	}
	if raw, ok := parameters["rule"].(map[string]any); ok {
		return raw
	}
	return parameters
}

func intervalCronExpression(value any) (string, error) {
	switch typed := value.(type) {
	case []any:
		if len(typed) == 0 {
			return "", nil
		}
		return intervalEntryCronExpression(anyScheduleMap(typed[0]))
	case map[string]any:
		if items, ok := typed["item"]; ok {
			return intervalCronExpression(items)
		}
		if values, ok := typed["values"]; ok {
			return intervalCronExpression(values)
		}
		return intervalEntryCronExpression(typed)
	default:
		if object, ok := rawObject(value); ok {
			return intervalCronExpression(object)
		}
	}
	return "", nil
}

func intervalEntryCronExpression(entry map[string]any) (string, error) {
	if len(entry) == 0 {
		return "", nil
	}
	field := strings.ToLower(strings.TrimSpace(stringParam(entry, "field", "unit")))
	if field == "" {
		field = "seconds"
	}
	amount := intervalAmount(entry, field)
	if amount <= 0 {
		return "", fmt.Errorf("schedule interval for %s must be positive", field)
	}
	switch field {
	case "second", "seconds":
		return fmt.Sprintf("@every %ds", amount), nil
	case "minute", "minutes":
		return fmt.Sprintf("0 */%d * * * *", amount), nil
	case "hour", "hours":
		return fmt.Sprintf("0 0 */%d * * *", amount), nil
	case "day", "days":
		return fmt.Sprintf("0 0 0 */%d * *", amount), nil
	case "week", "weeks":
		return fmt.Sprintf("0 0 0 */%d * *", amount*7), nil
	case "month", "months":
		return fmt.Sprintf("0 0 0 1 */%d *", amount), nil
	default:
		return "", fmt.Errorf("unknown schedule interval field %q", field)
	}
}

func intervalAmount(entry map[string]any, field string) int {
	keys := []string{"value", "amount", "every", "interval"}
	switch field {
	case "second", "seconds":
		keys = append([]string{"secondsInterval"}, keys...)
	case "minute", "minutes":
		keys = append([]string{"minutesInterval"}, keys...)
	case "hour", "hours":
		keys = append([]string{"hoursInterval", "hourInterval"}, keys...)
	case "day", "days":
		keys = append([]string{"daysInterval"}, keys...)
	case "week", "weeks":
		keys = append([]string{"weeksInterval"}, keys...)
	case "month", "months":
		keys = append([]string{"monthsInterval"}, keys...)
	}
	for _, key := range keys {
		value := scheduleInt(entry[key])
		if value > 0 {
			return value
		}
	}
	return 0
}

func boundedScheduleInt(values map[string]any, min int, max int, fallback int, keys ...string) (int, error) {
	raw, ok := firstScheduleValue(values, keys...)
	if !ok {
		return fallback, nil
	}
	value := scheduleInt(raw)
	if value < min || value > max {
		return 0, fmt.Errorf("schedule value %d outside %d-%d", value, min, max)
	}
	return value, nil
}

func firstScheduleValue(values map[string]any, keys ...string) (any, bool) {
	for _, key := range keys {
		value, ok := values[key]
		if ok {
			return value, true
		}
	}
	return nil, false
}

func scheduleDayFields(value any) (string, string, error) {
	switch typed := value.(type) {
	case []any:
		parts := make([]string, 0, len(typed))
		for _, entry := range typed {
			parts = append(parts, strings.TrimSpace(fmt.Sprint(entry)))
		}
		return "*", strings.Join(parts, ","), nil
	case string:
		text := strings.TrimSpace(typed)
		if text == "" || text == "*" || text == "everyDay" {
			return "*", "*", nil
		}
		return "*", text, nil
	default:
		day := scheduleInt(typed)
		if day >= 0 && day <= 6 {
			return "*", strconv.Itoa(day), nil
		}
		if day >= 1 && day <= 31 {
			return strconv.Itoa(day), "*", nil
		}
	}
	return "", "", fmt.Errorf("invalid schedule day %v", value)
}

func scheduleInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(typed))
		return parsed
	case interface{ Int64() (int64, error) }:
		parsed, _ := typed.Int64()
		return int(parsed)
	case interface{ Float64() (float64, error) }:
		parsed, _ := typed.Float64()
		return int(parsed)
	default:
		return 0
	}
}

func anyScheduleMap(value any) map[string]any {
	if object, ok := rawObject(value); ok {
		return object
	}
	return map[string]any{}
}
