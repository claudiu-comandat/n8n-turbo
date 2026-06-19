package descriptor

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

type AirtableFieldType string

const (
	AirtableFieldSingleLineText  AirtableFieldType = "singleLineText"
	AirtableFieldMultilineText   AirtableFieldType = "multilineText"
	AirtableFieldEmail           AirtableFieldType = "email"
	AirtableFieldURL             AirtableFieldType = "url"
	AirtableFieldNumber          AirtableFieldType = "number"
	AirtableFieldCurrency        AirtableFieldType = "currency"
	AirtableFieldPercent         AirtableFieldType = "percent"
	AirtableFieldDuration        AirtableFieldType = "duration"
	AirtableFieldAutoNumber      AirtableFieldType = "autoNumber"
	AirtableFieldCheckbox        AirtableFieldType = "checkbox"
	AirtableFieldDate            AirtableFieldType = "date"
	AirtableFieldDateTime        AirtableFieldType = "dateTime"
	AirtableFieldSingleSelect    AirtableFieldType = "singleSelect"
	AirtableFieldMultipleSelects AirtableFieldType = "multipleSelects"
	AirtableFieldLinkToRecord    AirtableFieldType = "multipleRecordLinks"
	AirtableFieldAttachment      AirtableFieldType = "multipleAttachments"
	AirtableFieldLookup          AirtableFieldType = "multipleLookupValues"
	AirtableFieldRollup          AirtableFieldType = "rollup"
	AirtableFieldFormula         AirtableFieldType = "formula"
	AirtableFieldCreatedTime     AirtableFieldType = "createdTime"
	AirtableFieldLastModified    AirtableFieldType = "lastModifiedTime"
	AirtableFieldCreatedBy       AirtableFieldType = "createdBy"
	AirtableFieldLastModifiedBy  AirtableFieldType = "lastModifiedBy"
	AirtableFieldUser            AirtableFieldType = "collaborator"
	AirtableFieldActionButton    AirtableFieldType = "button"
	AirtableFieldRating          AirtableFieldType = "rating"
	AirtableFieldPhoneNumber     AirtableFieldType = "phoneNumber"
	AirtableFieldBarcode         AirtableFieldType = "barcode"
)

type AirtableAttachment struct {
	ID         string         `json:"id,omitempty"`
	URL        string         `json:"url,omitempty"`
	Filename   string         `json:"filename,omitempty"`
	Size       int            `json:"size,omitempty"`
	Type       string         `json:"type,omitempty"`
	Width      int            `json:"width,omitempty"`
	Height     int            `json:"height,omitempty"`
	Thumbnails map[string]any `json:"thumbnails,omitempty"`
}

type AirtableRateLimiter struct {
	mu       sync.Mutex
	lastCall time.Time
	interval time.Duration
}

func NewAirtableRateLimiter() *AirtableRateLimiter {
	return &AirtableRateLimiter{interval: 200 * time.Millisecond}
}

func (r *AirtableRateLimiter) Wait(ctx context.Context) error {
	r.mu.Lock()
	wait := r.interval - time.Since(r.lastCall)
	if wait <= 0 {
		r.lastCall = time.Now()
		r.mu.Unlock()
		return nil
	}
	r.mu.Unlock()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		r.mu.Lock()
		r.lastCall = time.Now()
		r.mu.Unlock()
		return nil
	}
}

func ExtractAirtableAttachments(fieldValue any) []AirtableAttachment {
	values := sliceFromAny(fieldValue)
	result := make([]AirtableAttachment, 0, len(values))
	for _, value := range values {
		item, ok := mapFromAny(value)
		if !ok {
			continue
		}
		result = append(result, AirtableAttachment{
			ID:         stringFromAny(item["id"]),
			URL:        stringFromAny(item["url"]),
			Filename:   stringFromAny(item["filename"]),
			Size:       intFromAny(item["size"]),
			Type:       stringFromAny(item["type"]),
			Width:      intFromAny(item["width"]),
			Height:     intFromAny(item["height"]),
			Thumbnails: mapValue(item["thumbnails"]),
		})
	}
	return result
}

func FormatDateForAirtable(value time.Time, includeTime bool) string {
	if includeTime {
		return value.UTC().Format(time.RFC3339)
	}
	return value.Format("2006-01-02")
}

func BuildAirtableCreateRecord(fields map[string]any) map[string]any {
	return map[string]any{"fields": fields}
}

func BuildAirtableUpdateRecord(recordID string, fields map[string]any) map[string]any {
	return map[string]any{"id": recordID, "fields": fields}
}

func AirtableFilterFormula(conditions ...string) string {
	if len(conditions) == 0 {
		return ""
	}
	if len(conditions) == 1 {
		return conditions[0]
	}
	return "AND(" + strings.Join(conditions, ", ") + ")"
}

func AirtableFieldEquals(fieldName string, value string) string {
	return fmt.Sprintf("{%s}='%s'", fieldName, strings.ReplaceAll(value, "'", "\\'"))
}

func AirtableFlattenRecord(record map[string]any) map[string]any {
	flat := map[string]any{"id": record["id"], "createdTime": record["createdTime"]}
	if fields, ok := mapFromAny(record["fields"]); ok {
		for key, value := range fields {
			flat[key] = value
		}
	}
	return flat
}

func AirtableFlattenRecords(records []any) []map[string]any {
	result := make([]map[string]any, 0, len(records))
	for _, record := range records {
		if object, ok := mapFromAny(record); ok {
			result = append(result, AirtableFlattenRecord(object))
		}
	}
	return result
}

func AirtableRecordBatches(records []any, batchSize int) [][]any {
	if batchSize <= 0 {
		batchSize = 10
	}
	var batches [][]any
	for start := 0; start < len(records); start += batchSize {
		end := start + batchSize
		if end > len(records) {
			end = len(records)
		}
		batches = append(batches, records[start:end])
	}
	return batches
}

func mapValue(value any) map[string]any {
	if object, ok := mapFromAny(value); ok {
		return object
	}
	return nil
}

var defaultAirtableRateLimiter = NewAirtableRateLimiter()
