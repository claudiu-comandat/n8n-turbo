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
	OutputFieldName string
	IncludeInput    bool
	FormatString    string
	GetPart         string
	Operation       string
	Duration        int
	Unit            string
	RoundTo         string
	FromTimezone    string
	ToTimezone      string
	Timezone        string
	ISO             bool
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
		resolvedValue := resolveValue(in, items, index, params.Value)
		next, err := executeDateTimeItem(item, params, resolvedValue)
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
	action := strings.ToLower(firstNonEmptyNode(stringParam(raw, "action"), stringParam(raw, "operation"), "format"))
	return dateTimeParams{
		Action:          action,
		Value:           firstNonNilDateTime(raw["value"], raw["date"], raw["dateTime"]),
		OutputFieldName: firstNonEmptyNode(stringParam(raw, "outputFieldName", "destinationFieldName", "fieldName"), "outputDate"),
		IncludeInput:    boolParam(raw, "includeInput", true),
		FormatString:    stringParam(raw, "formatString", "format"),
		GetPart:         stringParam(raw, "getPart", "part"),
		Operation:       strings.ToLower(firstNonEmptyNode(stringParam(raw, "calculationOperation"), stringParam(raw, "operation"), "add")),
		Duration:        intParam(raw, "duration", 0),
		Unit:            strings.ToLower(firstNonEmptyNode(stringParam(raw, "unit"), "day")),
		RoundTo:         strings.ToLower(firstNonEmptyNode(stringParam(raw, "roundTo"), "day")),
		FromTimezone:    stringParam(raw, "fromTimezone"),
		ToTimezone:      stringParam(raw, "toTimezone"),
		Timezone:        firstNonEmptyNode(stringParam(options, "timezone"), stringParam(raw, "timezone"), "UTC"),
		ISO:             boolParam(options, "iso", boolParam(raw, "iso", false)),
	}
}

func executeDateTimeItem(item dataplane.Item, params dateTimeParams, rawValue any) (dataplane.Item, error) {
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
	parsed, err := parseDateTimeValue(value, params.Timezone)
	if err != nil {
		return dataplane.Item{}, err
	}
	result := dataplane.Item{JSON: map[string]any{}, Binary: item.Binary, PairedItem: item.PairedItem}
	if params.IncludeInput {
		result = cloneItem(item)
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
