package nodes

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/araddon/dateparse"
	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type DateTime struct{}

type dateTimeParams struct {
	Action          string
	Value           any
	StartDate       any
	EndDate         any
	OutputFieldName string
	IncludeInput    bool
	FormatString    string
	GetPart         string
	Operation       string
	Duration        int
	Units           []string
	Unit            string
	RoundTo         string
	FromTimezone    string
	ToTimezone      string
	Timezone        string
	ISO             bool
	ISOString       bool
}

func (DateTime) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	params := newDateTimeParams(in.Node.Parameters)
	items := firstInput(in.InputData)
	if len(items) == 0 {
		items = []dataplane.Item{{JSON: map[string]any{}}}
	}
	output := make([]dataplane.Item, 0, len(items))
	for index, item := range items {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		itemParams := params
		resolvedValue := resolveValue(in, items, index, params.Value)
		if params.Action == "between" {
			itemParams.StartDate = resolveValue(in, items, index, params.StartDate)
			itemParams.EndDate = resolveValue(in, items, index, params.EndDate)
		}
		next, err := executeDateTimeItem(item, index, itemParams, resolvedValue)
		if err != nil {
			return nil, fmt.Errorf("dateTime item %d: %w", index, err)
		}
		output = append(output, next)
	}
	return dataplane.MainOutput(output), nil
}

func newDateTimeParams(raw map[string]any) dateTimeParams {
	options := map[string]any{}
	if value, ok := raw["options"].(map[string]any); ok {
		options = value
	}
	rawOperation := firstNonEmptyNode(stringParam(raw, "operation"), stringParam(raw, "action"), "getCurrentDate")
	params := dateTimeParams{
		Action:          strings.ToLower(rawOperation),
		Value:           firstNonNilDateTime(raw["value"], raw["date"], raw["dateTime"], raw["magnitude"]),
		StartDate:       raw["startDate"],
		EndDate:         raw["endDate"],
		OutputFieldName: firstNonEmptyNode(stringParam(raw, "outputFieldName", "destinationFieldName", "fieldName"), "outputDate"),
		IncludeInput:    boolParam(options, "includeInputFields", boolParam(raw, "includeInput", false)),
		FormatString:    stringParam(raw, "formatString", "customFormat", "format"),
		GetPart:         stringParam(raw, "getPart", "part"),
		Operation:       strings.ToLower(firstNonEmptyNode(stringParam(raw, "calculationOperation"), stringParam(raw, "mode"), stringParam(raw, "operation"), "add")),
		Duration:        intParam(raw, "duration", 0),
		Units:           stringList(firstNonNilDateTime(raw["units"], raw["unit"])),
		Unit:            strings.ToLower(firstNonEmptyNode(stringParam(raw, "timeUnit"), stringParam(raw, "unit"), "day")),
		RoundTo:         strings.ToLower(firstNonEmptyNode(stringParam(raw, "toNearest"), stringParam(raw, "to"), stringParam(raw, "roundTo"), "day")),
		FromTimezone:    stringParam(raw, "fromTimezone"),
		ToTimezone:      stringParam(raw, "toTimezone"),
		Timezone:        firstNonEmptyNode(stringParam(options, "timezone"), stringParam(raw, "timezone"), "UTC"),
		ISO:             boolParam(options, "iso", boolParam(raw, "iso", false)),
		ISOString:       boolParam(options, "isoString", boolParam(raw, "isoString", false)),
	}
	switch rawOperation {
	case "getCurrentDate":
		params.Action = "current"
		if boolParam(raw, "includeTime", true) {
			params.RoundTo = ""
		} else {
			params.RoundTo = "day"
		}
	case "formatDate":
		params.Action = "format"
		if stringParam(raw, "format") != "custom" {
			params.FormatString = stringParam(raw, "format")
		}
	case "addToDate":
		params.Action = "calculate"
		params.Operation = "add"
	case "subtractFromDate":
		params.Action = "calculate"
		params.Operation = "subtract"
	case "extractDate":
		params.Action = "get"
	case "getTimeBetweenDates":
		params.Action = "between"
		if params.OutputFieldName == "outputDate" {
			params.OutputFieldName = "timeDifference"
		}
	case "roundDate":
		params.Action = "round"
		params.Operation = strings.ToLower(firstNonEmptyNode(stringParam(raw, "mode"), "roundDown"))
	}
	return params
}

func executeDateTimeItem(item dataplane.Item, itemIndex int, params dateTimeParams, rawValue any) (dataplane.Item, error) {
	result := dataplane.Item{JSON: map[string]any{}, Binary: item.Binary, PairedItem: &dataplane.PairedItem{Item: itemIndex}}
	if params.IncludeInput {
		result = cloneItem(item)
		result.PairedItem = &dataplane.PairedItem{Item: itemIndex}
	}
	if params.Action == "current" {
		now := time.Now()
		parsed := now
		var err error
		if params.RoundTo != "" {
			parsed, err = roundDateTime(now, params)
			if err != nil {
				return dataplane.Item{}, err
			}
		}
		output, err := formatDateTimeValue(parsed, params)
		if err != nil {
			return dataplane.Item{}, err
		}
		result.JSON[params.OutputFieldName] = output
		return result, nil
	}
	value := fmt.Sprint(rawValue)
	if value == "" || value == "<nil>" {
		if existing, ok := item.JSON["value"]; ok {
			value = fmt.Sprint(existing)
		} else if existing, ok := item.JSON["date"]; ok {
			value = fmt.Sprint(existing)
		}
	}
	if params.Action == "now" {
		value = "now"
		params.Action = "format"
	}
	if params.Action == "between" {
		startDate := fmt.Sprint(resolveSimpleDateTimeValue(item, params.StartDate))
		endDate := fmt.Sprint(resolveSimpleDateTimeValue(item, params.EndDate))
		start, err := parseDateTimeValue(startDate, params.Timezone)
		if err != nil {
			return dataplane.Item{}, fmt.Errorf("startDate: %w", err)
		}
		end, err := parseDateTimeValue(endDate, params.Timezone)
		if err != nil {
			return dataplane.Item{}, fmt.Errorf("endDate: %w", err)
		}
		output, err := dateTimeBetween(start, end, params)
		if err != nil {
			return dataplane.Item{}, err
		}
		result.JSON[params.OutputFieldName] = output
		return result, nil
	}
	parsed, err := parseDateTimeValue(value, params.Timezone)
	if err != nil {
		return dataplane.Item{}, err
	}
	var output any
	switch params.Action {
	case "format":
		output, err = formatDateTimeValue(parsed, params)
	case "get":
		output, err = getDateTimePart(parsed, params)
	case "calculate":
		var calculated time.Time
		calculated, err = calculateDateTime(parsed, params)
		if err == nil {
			output, err = formatDateTimeValue(calculated, params)
		}
	case "round":
		var rounded time.Time
		rounded, err = roundDateTime(parsed, params)
		if err == nil {
			output, err = formatDateTimeValue(rounded, params)
		}
	case "convert":
		output, err = convertDateTimeZone(parsed, params)
	default:
		err = fmt.Errorf("unsupported action %s", params.Action)
	}
	if err != nil {
		return dataplane.Item{}, err
	}
	result.JSON[params.OutputFieldName] = output
	return result, nil
}

func parseDateTimeValue(value string, timezone string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("empty date value")
	}
	loc := time.UTC
	if timezone != "" {
		if loaded, err := time.LoadLocation(timezone); err == nil {
			loc = loaded
		}
	}
	if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
		if parsed > 1e12 {
			return time.Unix(parsed/1000, (parsed%1000)*int64(time.Millisecond)).UTC(), nil
		}
		return time.Unix(parsed, 0).UTC(), nil
	}
	if parsed, err := strconv.ParseFloat(value, 64); err == nil && parsed > 0 {
		seconds := int64(parsed)
		return time.Unix(seconds, int64((parsed-float64(seconds))*1e9)).UTC(), nil
	}
	switch strings.ToLower(value) {
	case "now":
		return time.Now().In(loc), nil
	case "today":
		now := time.Now().In(loc)
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc), nil
	case "yesterday":
		now := time.Now().In(loc).AddDate(0, 0, -1)
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc), nil
	case "tomorrow":
		now := time.Now().In(loc).AddDate(0, 0, 1)
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc), nil
	}
	if parsed, err := dateparse.ParseIn(value, loc); err == nil {
		return parsed, nil
	}
	formats := []string{time.RFC3339Nano, time.RFC3339, time.RFC1123Z, time.RFC1123, time.RFC850, time.RFC822Z, time.RFC822, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02", "01/02/2006", "02.01.2006", "January 2, 2006", "Jan 2, 2006", "02 January 2006", "2 Jan 2006", "January 2006", "2006-01"}
	for _, format := range formats {
		if parsed, err := time.ParseInLocation(format, value, loc); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("could not parse date %q", value)
}

func formatDateTimeValue(value time.Time, params dateTimeParams) (string, error) {
	if params.ISO {
		return value.UTC().Format(time.RFC3339), nil
	}
	timezone := params.Timezone
	if timezone != "" {
		location, err := time.LoadLocation(timezone)
		if err != nil {
			return "", err
		}
		value = value.In(location)
	}
	format := params.FormatString
	if format == "" {
		return value.Format(time.RFC3339), nil
	}
	return value.Format(luxonDateTimeFormatToGo(format)), nil
}

func getDateTimePart(value time.Time, params dateTimeParams) (any, error) {
	if params.Timezone != "" {
		if location, err := time.LoadLocation(params.Timezone); err == nil {
			value = value.In(location)
		}
	}
	switch strings.ToLower(params.GetPart) {
	case "year":
		return value.Year(), nil
	case "month":
		return int(value.Month()), nil
	case "day":
		return value.Day(), nil
	case "hour":
		return value.Hour(), nil
	case "minute":
		return value.Minute(), nil
	case "second":
		return value.Second(), nil
	case "millisecond":
		return value.Nanosecond() / int(time.Millisecond), nil
	case "weekday":
		return int(value.Weekday()), nil
	case "weekdayname":
		return value.Weekday().String(), nil
	case "dayofyear":
		return value.YearDay(), nil
	case "week":
		_, week := value.ISOWeek()
		return week, nil
	case "isoweekyear":
		year, _ := value.ISOWeek()
		return year, nil
	case "quarter":
		return (int(value.Month())-1)/3 + 1, nil
	case "timestamp":
		return value.Unix(), nil
	case "timestampms":
		return value.UnixMilli(), nil
	default:
		return nil, fmt.Errorf("unsupported date part %s", params.GetPart)
	}
}

func calculateDateTime(value time.Time, params dateTimeParams) (time.Time, error) {
	duration := params.Duration
	if params.Operation == "subtract" {
		duration = -duration
	}
	switch params.Unit {
	case "year", "years":
		return value.AddDate(duration, 0, 0), nil
	case "month", "months":
		return value.AddDate(0, duration, 0), nil
	case "week", "weeks":
		return value.AddDate(0, 0, duration*7), nil
	case "day", "days":
		return value.AddDate(0, 0, duration), nil
	case "hour", "hours":
		return value.Add(time.Duration(duration) * time.Hour), nil
	case "minute", "minutes":
		return value.Add(time.Duration(duration) * time.Minute), nil
	case "second", "seconds":
		return value.Add(time.Duration(duration) * time.Second), nil
	case "millisecond", "milliseconds":
		return value.Add(time.Duration(duration) * time.Millisecond), nil
	default:
		return value, fmt.Errorf("unsupported unit %s", params.Unit)
	}
}

func roundDateTime(value time.Time, params dateTimeParams) (time.Time, error) {
	location := time.UTC
	if params.Timezone != "" {
		if loaded, err := time.LoadLocation(params.Timezone); err == nil {
			location = loaded
		}
	}
	value = value.In(location)
	if params.Operation == "roundup" {
		next, err := addDateTimeUnit(value, params.RoundTo, 1)
		if err != nil {
			return value, err
		}
		value = next
	}
	switch params.RoundTo {
	case "year":
		return time.Date(value.Year(), 1, 1, 0, 0, 0, 0, location), nil
	case "month":
		return time.Date(value.Year(), value.Month(), 1, 0, 0, 0, 0, location), nil
	case "week":
		weekday := int(value.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		monday := value.AddDate(0, 0, -(weekday - 1))
		return time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, location), nil
	case "day":
		return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, location), nil
	case "hour":
		return time.Date(value.Year(), value.Month(), value.Day(), value.Hour(), 0, 0, 0, location), nil
	case "minute":
		return time.Date(value.Year(), value.Month(), value.Day(), value.Hour(), value.Minute(), 0, 0, location), nil
	case "second":
		return time.Date(value.Year(), value.Month(), value.Day(), value.Hour(), value.Minute(), value.Second(), 0, location), nil
	default:
		return value, fmt.Errorf("unsupported round unit %s", params.RoundTo)
	}
}

func addDateTimeUnit(value time.Time, unit string, amount int) (time.Time, error) {
	switch unit {
	case "year", "years":
		return value.AddDate(amount, 0, 0), nil
	case "month", "months":
		return value.AddDate(0, amount, 0), nil
	case "week", "weeks":
		return value.AddDate(0, 0, amount*7), nil
	case "day", "days":
		return value.AddDate(0, 0, amount), nil
	case "hour", "hours":
		return value.Add(time.Duration(amount) * time.Hour), nil
	case "minute", "minutes":
		return value.Add(time.Duration(amount) * time.Minute), nil
	case "second", "seconds":
		return value.Add(time.Duration(amount) * time.Second), nil
	default:
		return value, fmt.Errorf("unsupported unit %s", unit)
	}
}

func convertDateTimeZone(value time.Time, params dateTimeParams) (string, error) {
	if params.FromTimezone != "" {
		location, err := time.LoadLocation(params.FromTimezone)
		if err != nil {
			return "", err
		}
		value = time.Date(value.Year(), value.Month(), value.Day(), value.Hour(), value.Minute(), value.Second(), value.Nanosecond(), location)
	}
	target := firstNonEmptyNode(params.ToTimezone, params.Timezone, "UTC")
	location, err := time.LoadLocation(target)
	if err != nil {
		return "", err
	}
	params.Timezone = target
	return formatDateTimeValue(value.In(location), params)
}

func resolveSimpleDateTimeValue(item dataplane.Item, value any) any {
	if value != nil {
		return value
	}
	if item.JSON != nil {
		if raw, ok := item.JSON["date"]; ok {
			return raw
		}
	}
	return ""
}

func dateTimeBetween(start time.Time, end time.Time, params dateTimeParams) (any, error) {
	units := normalizedDateTimeUnits(params.Units)
	parts := dateTimeDurationParts(start, end, units)
	if params.ISOString {
		return dateTimeDurationISO(parts), nil
	}
	result := map[string]any{}
	for _, unit := range units {
		result[dateTimeDurationKey(unit)] = parts[unit]
	}
	return result, nil
}

func normalizedDateTimeUnits(units []string) []string {
	if len(units) == 0 {
		return []string{"day"}
	}
	seen := map[string]bool{}
	result := make([]string, 0, len(units))
	for _, unit := range units {
		unit = strings.ToLower(strings.TrimSpace(unit))
		unit = strings.TrimSuffix(unit, "s")
		switch unit {
		case "year", "month", "week", "day", "hour", "minute", "second", "millisecond":
			if !seen[unit] {
				result = append(result, unit)
				seen[unit] = true
			}
		}
	}
	if len(result) == 0 {
		return []string{"day"}
	}
	return result
}

func dateTimeDurationParts(start time.Time, end time.Time, units []string) map[string]float64 {
	sign := 1.0
	if end.Before(start) {
		start, end = end, start
		sign = -1
	}
	current := start
	parts := map[string]float64{}
	for _, unit := range units {
		switch unit {
		case "year":
			count := 0
			for !current.AddDate(1, 0, 0).After(end) {
				current = current.AddDate(1, 0, 0)
				count++
			}
			parts[unit] = sign * float64(count)
		case "month":
			count := 0
			for !current.AddDate(0, 1, 0).After(end) {
				current = current.AddDate(0, 1, 0)
				count++
			}
			parts[unit] = sign * float64(count)
		case "week":
			count := int(end.Sub(current) / (7 * 24 * time.Hour))
			current = current.AddDate(0, 0, count*7)
			parts[unit] = sign * float64(count)
		case "day":
			count := int(end.Sub(current) / (24 * time.Hour))
			current = current.AddDate(0, 0, count)
			parts[unit] = sign * float64(count)
		case "hour":
			count := int(end.Sub(current) / time.Hour)
			current = current.Add(time.Duration(count) * time.Hour)
			parts[unit] = sign * float64(count)
		case "minute":
			count := int(end.Sub(current) / time.Minute)
			current = current.Add(time.Duration(count) * time.Minute)
			parts[unit] = sign * float64(count)
		case "second":
			count := int(end.Sub(current) / time.Second)
			current = current.Add(time.Duration(count) * time.Second)
			parts[unit] = sign * float64(count)
		case "millisecond":
			count := float64(end.Sub(current)) / float64(time.Millisecond)
			parts[unit] = sign * count
		}
	}
	return parts
}

func dateTimeDurationKey(unit string) string {
	switch unit {
	case "millisecond":
		return "milliseconds"
	default:
		return unit + "s"
	}
}

func dateTimeDurationISO(parts map[string]float64) string {
	years := int(parts["year"])
	months := int(parts["month"])
	weeks := int(parts["week"])
	days := int(parts["day"])
	hours := int(parts["hour"])
	minutes := int(parts["minute"])
	seconds := parts["second"] + parts["millisecond"]/1000
	var builder strings.Builder
	builder.WriteString("P")
	if years != 0 {
		builder.WriteString(fmt.Sprintf("%dY", years))
	}
	if months != 0 {
		builder.WriteString(fmt.Sprintf("%dM", months))
	}
	if weeks != 0 {
		builder.WriteString(fmt.Sprintf("%dW", weeks))
	}
	if days != 0 {
		builder.WriteString(fmt.Sprintf("%dD", days))
	}
	if hours != 0 || minutes != 0 || seconds != 0 {
		builder.WriteString("T")
		if hours != 0 {
			builder.WriteString(fmt.Sprintf("%dH", hours))
		}
		if minutes != 0 {
			builder.WriteString(fmt.Sprintf("%dM", minutes))
		}
		if seconds != 0 {
			if seconds == float64(int(seconds)) {
				builder.WriteString(fmt.Sprintf("%dS", int(seconds)))
			} else {
				builder.WriteString(strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.3f", seconds), "0"), ".") + "S")
			}
		}
	}
	if builder.Len() == 1 {
		return "PT0S"
	}
	return builder.String()
}

func luxonDateTimeFormatToGo(format string) string {
	replacements := []struct {
		From string
		To   string
	}{
		{"yyyy", "2006"}, {"yy", "06"}, {"MMMM", "January"}, {"MMM", "Jan"}, {"MM", "01"}, {"M", "1"}, {"dd", "02"}, {"d", "2"}, {"cccc", "Monday"}, {"ccc", "Mon"}, {"HH", "15"}, {"H", "15"}, {"hh", "03"}, {"h", "3"}, {"mm", "04"}, {"m", "4"}, {"ss", "05"}, {"s", "5"}, {"SSS", "000"}, {"SS", "00"}, {"S", "0"}, {"a", "PM"}, {"ZZZ", "-07:00"}, {"ZZ", "-0700"}, {"Z", "-07:00"}, {"z", "MST"},
	}
	result := format
	for _, replacement := range replacements {
		result = strings.ReplaceAll(result, replacement.From, replacement.To)
	}
	return result
}

func firstNonNilDateTime(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return ""
}
