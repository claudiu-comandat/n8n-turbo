package nodes

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type RemoveDuplicates struct{}

type removeDuplicatesParams struct {
	Compare           string
	FieldsToCompare   []string
	DisabledFields    []string
	KeepMode          string
	CaseSensitive     bool
	RemoveBlankValues bool
	FuzzyMatching     bool
	FuzzyThreshold    float64
	SortBeforeDedup   bool
}

func (RemoveDuplicates) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	items := cloneItems(firstInput(in.InputData))
	params := newRemoveDuplicatesParams(in.Node.Parameters)
	if len(items) == 0 {
		return dataplane.MainOutput([]dataplane.Item{}), nil
	}
	fields, err := removeDuplicateFields(items, params)
	if err != nil {
		return nil, err
	}
	if params.SortBeforeDedup {
		sort.SliceStable(items, func(i int, j int) bool {
			return removeDuplicateKey(items[i].JSON, fields, params) < removeDuplicateKey(items[j].JSON, fields, params)
		})
	}
	if params.FuzzyMatching {
		return dataplane.MainOutput(removeDuplicateFuzzy(items, fields, params)), nil
	}
	switch params.KeepMode {
	case "first":
		return dataplane.MainOutput(removeDuplicateKeepFirst(items, fields, params)), nil
	case "last":
		return dataplane.MainOutput(removeDuplicateKeepLast(items, fields, params)), nil
	case "all-if-different", "unique", "unique-only":
		return dataplane.MainOutput(removeDuplicateKeepUniqueOnly(items, fields, params)), nil
	default:
		return nil, fmt.Errorf("removeDuplicates: unsupported keepMode %s", params.KeepMode)
	}
}

func newRemoveDuplicatesParams(raw map[string]any) removeDuplicatesParams {
	options := map[string]any{}
	if value, ok := raw["options"].(map[string]any); ok {
		options = value
	}
	fields := stringList(firstNonNilRemoveDuplicates(raw["fieldsToCompare"], raw["fields"], raw["fieldName"], raw["field"]))
	disabled := stringList(firstNonNilRemoveDuplicates(raw["disabledFields"], raw["fieldsToExclude"]))
	return removeDuplicatesParams{
		Compare:           firstNonEmptyNode(stringParam(raw, "compare"), chooseRemoveDuplicateCompare(fields)),
		FieldsToCompare:   fields,
		DisabledFields:    disabled,
		KeepMode:          strings.ToLower(firstNonEmptyNode(stringParam(raw, "keepMode"), stringParam(raw, "mode"), "first")),
		CaseSensitive:     boolParam(options, "caseSensitive", boolParam(raw, "caseSensitive", true)),
		RemoveBlankValues: boolParam(options, "removeBlankValues", boolParam(raw, "removeBlankValues", false)),
		FuzzyMatching:     boolParam(options, "fuzzyMatching", boolParam(raw, "fuzzyMatching", false)),
		FuzzyThreshold:    floatParamRemoveDuplicates(firstNonNilRemoveDuplicates(options["fuzzyThreshold"], raw["fuzzyThreshold"]), 0.8),
		SortBeforeDedup:   boolParam(options, "sortBeforeDedup", boolParam(raw, "sortBeforeDedup", false)),
	}
}

func chooseRemoveDuplicateCompare(fields []string) string {
	if len(fields) > 0 {
		return "selected-fields"
	}
	return "all-fields"
}

func removeDuplicateFields(items []dataplane.Item, params removeDuplicatesParams) ([]string, error) {
	switch strings.ToLower(params.Compare) {
	case "selected-fields", "selected", "fields":
		if len(params.FieldsToCompare) == 0 {
			return nil, fmt.Errorf("removeDuplicates: fieldsToCompare is required")
		}
		return params.FieldsToCompare, nil
	default:
		seen := map[string]bool{}
		disabled := map[string]bool{}
		for _, field := range params.DisabledFields {
			disabled[field] = true
		}
		for _, item := range items {
			for key := range item.JSON {
				if !disabled[key] {
					seen[key] = true
				}
			}
		}
		fields := make([]string, 0, len(seen))
		for key := range seen {
			fields = append(fields, key)
		}
		sort.Strings(fields)
		return fields, nil
	}
}

func removeDuplicateKeepFirst(items []dataplane.Item, fields []string, params removeDuplicatesParams) []dataplane.Item {
	seen := map[string]bool{}
	result := make([]dataplane.Item, 0, len(items))
	for _, item := range items {
		key := removeDuplicateKey(item.JSON, fields, params)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, item)
	}
	return result
}

func removeDuplicateKeepLast(items []dataplane.Item, fields []string, params removeDuplicatesParams) []dataplane.Item {
	seen := map[string]bool{}
	reversed := make([]dataplane.Item, 0, len(items))
	for index := len(items) - 1; index >= 0; index-- {
		key := removeDuplicateKey(items[index].JSON, fields, params)
		if seen[key] {
			continue
		}
		seen[key] = true
		reversed = append(reversed, items[index])
	}
	result := make([]dataplane.Item, len(reversed))
	for index, item := range reversed {
		result[len(reversed)-1-index] = item
	}
	return result
}

func removeDuplicateKeepUniqueOnly(items []dataplane.Item, fields []string, params removeDuplicatesParams) []dataplane.Item {
	counts := map[string]int{}
	order := []string{}
	byKey := map[string]dataplane.Item{}
	for _, item := range items {
		key := removeDuplicateKey(item.JSON, fields, params)
		if counts[key] == 0 {
			order = append(order, key)
			byKey[key] = item
		}
		counts[key]++
	}
	result := []dataplane.Item{}
	for _, key := range order {
		if counts[key] == 1 {
			result = append(result, byKey[key])
		}
	}
	return result
}

func removeDuplicateFuzzy(items []dataplane.Item, fields []string, params removeDuplicatesParams) []dataplane.Item {
	used := make([]bool, len(items))
	result := []dataplane.Item{}
	threshold := params.FuzzyThreshold
	if threshold <= 0 || threshold > 1 {
		threshold = 0.8
	}
	for index := range items {
		if used[index] {
			continue
		}
		result = append(result, items[index])
		used[index] = true
		current := removeDuplicateFuzzyString(items[index].JSON, fields, params)
		for other := index + 1; other < len(items); other++ {
			if used[other] {
				continue
			}
			candidate := removeDuplicateFuzzyString(items[other].JSON, fields, params)
			if jaroWinklerRemoveDuplicates(current, candidate) >= threshold {
				used[other] = true
			}
		}
	}
	return result
}

func removeDuplicateKey(item map[string]any, fields []string, params removeDuplicatesParams) string {
	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		value := item[field]
		if params.RemoveBlankValues && isBlankRemoveDuplicateValue(value) {
			continue
		}
		parts = append(parts, field+"="+normalizeRemoveDuplicateValue(value, params.CaseSensitive))
	}
	key := strings.Join(parts, "|")
	if len(key) > 200 {
		hash := md5.Sum([]byte(key))
		return fmt.Sprintf("%x", hash)
	}
	return key
}

func removeDuplicateFuzzyString(item map[string]any, fields []string, params removeDuplicatesParams) string {
	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		value := item[field]
		if params.RemoveBlankValues && isBlankRemoveDuplicateValue(value) {
			continue
		}
		parts = append(parts, normalizeRemoveDuplicateValue(value, false))
	}
	return strings.Join(parts, " ")
}

func normalizeRemoveDuplicateValue(value any, caseSensitive bool) string {
	if value == nil {
		return "<nil>"
	}
	var result string
	switch typed := value.(type) {
	case string:
		result = typed
	case bool:
		if typed {
			result = "true"
		} else {
			result = "false"
		}
	case int:
		result = fmt.Sprintf("%d", typed)
	case int64:
		result = fmt.Sprintf("%d", typed)
	case float64:
		if typed == float64(int64(typed)) {
			result = fmt.Sprintf("%d", int64(typed))
		} else {
			result = fmt.Sprintf("%g", typed)
		}
	case map[string]any, []any:
		encoded, err := json.Marshal(typed)
		if err == nil {
			result = string(encoded)
		} else {
			result = fmt.Sprint(typed)
		}
	default:
		result = fmt.Sprint(value)
	}
	if !caseSensitive {
		result = strings.ToLower(result)
	}
	return result
}

func isBlankRemoveDuplicateValue(value any) bool {
	if value == nil {
		return true
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed) == ""
	case []any:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	default:
		return false
	}
}

func jaroWinklerRemoveDuplicates(a string, b string) float64 {
	a = strings.ToLower(strings.TrimSpace(a))
	b = strings.ToLower(strings.TrimSpace(b))
	if a == b {
		return 1
	}
	if a == "" || b == "" {
		return 0
	}
	jaroScore := jaroRemoveDuplicates(a, b)
	prefix := 0
	for index := 0; index < len(a) && index < len(b) && index < 4; index++ {
		if a[index] != b[index] {
			break
		}
		prefix++
	}
	return jaroScore + float64(prefix)*0.1*(1-jaroScore)
}

func jaroRemoveDuplicates(a string, b string) float64 {
	if a == b {
		return 1
	}
	aLen := len(a)
	bLen := len(b)
	if aLen == 0 || bLen == 0 {
		return 0
	}
	matchDistance := int(math.Max(float64(aLen), float64(bLen))/2) - 1
	if matchDistance < 0 {
		matchDistance = 0
	}
	aMatches := make([]bool, aLen)
	bMatches := make([]bool, bLen)
	matches := 0
	transpositions := 0
	for i := 0; i < aLen; i++ {
		start := max(0, i-matchDistance)
		end := min(i+matchDistance+1, bLen)
		for j := start; j < end; j++ {
			if bMatches[j] || a[i] != b[j] {
				continue
			}
			aMatches[i] = true
			bMatches[j] = true
			matches++
			break
		}
	}
	if matches == 0 {
		return 0
	}
	k := 0
	for i := 0; i < aLen; i++ {
		if !aMatches[i] {
			continue
		}
		for !bMatches[k] {
			k++
		}
		if a[i] != b[k] {
			transpositions++
		}
		k++
	}
	return (float64(matches)/float64(aLen) + float64(matches)/float64(bLen) + float64(matches-transpositions/2)/float64(matches)) / 3
}

func firstNonNilRemoveDuplicates(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func floatParamRemoveDuplicates(value any, fallback float64) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case string:
		parsed, err := strconv.ParseFloat(typed, 64)
		if err == nil {
			return parsed
		}
	}
	return fallback
}
