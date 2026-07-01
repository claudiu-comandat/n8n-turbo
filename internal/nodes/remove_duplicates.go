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
	"sync"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type RemoveDuplicates struct{}

type removeDuplicatesParams struct {
	Operation          string
	Compare            string
	FieldsToCompare    []string
	DisabledFields     []string
	KeepMode           string
	Logic              string
	DedupeValue        any
	IncrementalValue   any
	DateValue          any
	Scope              string
	HistorySize        int
	CaseSensitive      bool
	RemoveBlankValues  bool
	FuzzyMatching      bool
	FuzzyThreshold     float64
	SortBeforeDedup    bool
	DisableDotNotation bool
	RemoveOtherFields  bool
	NodeVersion        float64
}

func (RemoveDuplicates) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	items := cloneItems(firstInput(in.InputData))
	params := newRemoveDuplicatesParams(in.Node.Parameters, in.Node.TypeVersion)
	if len(items) == 0 {
		return dataplane.MainOutput([]dataplane.Item{}), nil
	}
	switch params.Operation {
	case "clearDeduplicationHistory":
		removeDuplicateClearHistory(in, params)
		return dataplane.MainOutput(items), nil
	case "removeItemsSeenInPreviousExecutions":
		return removeDuplicateSeenInPreviousExecutions(in, items, params)
	}
	fields, err := removeDuplicateFields(items, params)
	if err != nil {
		return nil, err
	}
	if err := validateRemoveDuplicateFields(items, fields, params); err != nil {
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
	var result []dataplane.Item
	switch params.KeepMode {
	case "first":
		result = removeDuplicateKeepFirst(items, fields, params)
	case "last":
		result = removeDuplicateKeepLast(items, fields, params)
	case "all-if-different", "unique", "unique-only":
		result = removeDuplicateKeepUniqueOnly(items, fields, params)
	default:
		return nil, fmt.Errorf("removeDuplicates: unsupported keepMode %s", params.KeepMode)
	}
	if params.RemoveOtherFields {
		result = removeDuplicatePickFields(result, fields, params)
	}
	return dataplane.MainOutput(result), nil
}

func newRemoveDuplicatesParams(raw map[string]any, nodeVersion float64) removeDuplicatesParams {
	options := map[string]any{}
	if value, ok := raw["options"].(map[string]any); ok {
		options = value
	}
	fields := stringList(firstNonNilRemoveDuplicates(raw["fieldsToCompare"], raw["fields"], raw["fieldName"], raw["field"]))
	disabled := stringList(firstNonNilRemoveDuplicates(raw["disabledFields"], raw["fieldsToExclude"]))
	return removeDuplicatesParams{
		Operation:          firstNonEmptyNode(stringParam(raw, "operation"), "removeDuplicateInputItems"),
		Compare:            firstNonEmptyNode(stringParam(raw, "compare"), chooseRemoveDuplicateCompare(fields)),
		FieldsToCompare:    fields,
		DisabledFields:     disabled,
		KeepMode:           strings.ToLower(firstNonEmptyNode(stringParam(raw, "keepMode"), stringParam(raw, "mode"), "first")),
		Logic:              firstNonEmptyNode(stringParam(raw, "logic"), "removeItemsWithAlreadySeenKeyValues"),
		DedupeValue:        firstNonNilRemoveDuplicates(raw["dedupeValue"], raw["value"]),
		IncrementalValue:   firstNonNilRemoveDuplicates(raw["incrementalDedupeValue"], raw["dedupeValue"], raw["value"]),
		DateValue:          firstNonNilRemoveDuplicates(raw["dateDedupeValue"], raw["dedupeValue"], raw["value"]),
		Scope:              firstNonEmptyNode(stringParam(raw, "scope"), stringParam(options, "scope"), "node"),
		HistorySize:        int(floatParamRemoveDuplicates(firstNonNilRemoveDuplicates(options["historySize"], raw["historySize"]), 10000)),
		CaseSensitive:      boolParam(options, "caseSensitive", boolParam(raw, "caseSensitive", true)),
		RemoveBlankValues:  boolParam(options, "removeBlankValues", boolParam(raw, "removeBlankValues", false)),
		FuzzyMatching:      boolParam(options, "fuzzyMatching", boolParam(raw, "fuzzyMatching", false)),
		FuzzyThreshold:     floatParamRemoveDuplicates(firstNonNilRemoveDuplicates(options["fuzzyThreshold"], raw["fuzzyThreshold"]), 0.8),
		SortBeforeDedup:    boolParam(options, "sortBeforeDedup", boolParam(raw, "sortBeforeDedup", false)),
		DisableDotNotation: boolParam(options, "disableDotNotation", boolParam(raw, "disableDotNotation", false)),
		RemoveOtherFields:  boolParam(options, "removeOtherFields", boolParam(raw, "removeOtherFields", false)),
		NodeVersion:        nodeVersion,
	}
}

type removeDuplicateHistoryState struct {
	Entries    map[string]bool
	Order      []string
	HasNumber  bool
	LatestNum  float64
	HasDate    bool
	LatestDate time.Time
}

var removeDuplicateHistory = struct {
	sync.Mutex
	byKey map[string]*removeDuplicateHistoryState
}{byKey: map[string]*removeDuplicateHistoryState{}}

func removeDuplicateSeenInPreviousExecutions(in engine.ExecuteInput, items []dataplane.Item, params removeDuplicatesParams) (dataplane.Output, error) {
	key, err := removeDuplicateHistoryKey(in, params.Scope)
	if err != nil {
		return nil, err
	}
	switch params.Logic {
	case "removeItemsWithAlreadySeenKeyValues", "":
		return removeDuplicateSeenEntries(in, items, params, key)
	case "removeItemsUpToStoredIncrementalKey":
		return removeDuplicateSeenIncremental(in, items, params, key)
	case "removeItemsUpToStoredDate":
		return removeDuplicateSeenDate(in, items, params, key)
	default:
		return dataplane.Output{items}, nil
	}
}

func removeDuplicateSeenEntries(in engine.ExecuteInput, items []dataplane.Item, params removeDuplicatesParams, historyKey string) (dataplane.Output, error) {
	groups := map[string][]dataplane.Item{}
	order := []string{}
	for index, item := range items {
		key := fmt.Sprint(resolveValue(in, items, index, params.DedupeValue))
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], item)
	}
	removeDuplicateHistory.Lock()
	defer removeDuplicateHistory.Unlock()
	state := removeDuplicateState(historyKey)
	if params.HistorySize > 0 && len(state.Order)+len(items) > params.HistorySize {
		return nil, fmt.Errorf("removeDuplicates: processed data would exceed historySize")
	}
	var fresh, seen []dataplane.Item
	for _, key := range order {
		if state.Entries[key] {
			seen = append(seen, groups[key]...)
			continue
		}
		fresh = append(fresh, groups[key]...)
		state.Entries[key] = true
		state.Order = append(state.Order, key)
	}
	removeDuplicateTrimHistory(state, params.HistorySize)
	return dataplane.Output{fresh, seen}, nil
}

func removeDuplicateSeenIncremental(in engine.ExecuteInput, items []dataplane.Item, params removeDuplicatesParams, historyKey string) (dataplane.Output, error) {
	removeDuplicateHistory.Lock()
	defer removeDuplicateHistory.Unlock()
	state := removeDuplicateState(historyKey)
	var fresh, seen []dataplane.Item
	for index, item := range items {
		value, err := strconv.ParseFloat(fmt.Sprint(resolveValue(in, items, index, params.IncrementalValue)), 64)
		if err != nil || math.IsNaN(value) {
			return nil, fmt.Errorf("removeDuplicates: '%v' is not a number", resolveValue(in, items, index, params.IncrementalValue))
		}
		if state.HasNumber && value <= state.LatestNum {
			seen = append(seen, item)
			continue
		}
		fresh = append(fresh, item)
		state.HasNumber = true
		state.LatestNum = value
	}
	return dataplane.Output{fresh, seen}, nil
}

func removeDuplicateSeenDate(in engine.ExecuteInput, items []dataplane.Item, params removeDuplicatesParams, historyKey string) (dataplane.Output, error) {
	removeDuplicateHistory.Lock()
	defer removeDuplicateHistory.Unlock()
	state := removeDuplicateState(historyKey)
	var fresh, seen []dataplane.Item
	for index, item := range items {
		raw := fmt.Sprint(resolveValue(in, items, index, params.DateValue))
		value, err := parseDateTimeValue(raw, "")
		if err != nil {
			return nil, fmt.Errorf("removeDuplicates: '%s' is not a valid date", raw)
		}
		if state.HasDate && !value.After(state.LatestDate) {
			seen = append(seen, item)
			continue
		}
		fresh = append(fresh, item)
		state.HasDate = true
		state.LatestDate = value
	}
	return dataplane.Output{fresh, seen}, nil
}

func removeDuplicateClearHistory(in engine.ExecuteInput, params removeDuplicatesParams) {
	key, err := removeDuplicateHistoryKey(in, params.Scope)
	if err != nil {
		return
	}
	removeDuplicateHistory.Lock()
	defer removeDuplicateHistory.Unlock()
	delete(removeDuplicateHistory.byKey, key)
}

func removeDuplicateState(key string) *removeDuplicateHistoryState {
	state := removeDuplicateHistory.byKey[key]
	if state == nil {
		state = &removeDuplicateHistoryState{Entries: map[string]bool{}}
		removeDuplicateHistory.byKey[key] = state
	}
	return state
}

func removeDuplicateTrimHistory(state *removeDuplicateHistoryState, limit int) {
	if limit <= 0 {
		return
	}
	for len(state.Order) > limit {
		delete(state.Entries, state.Order[0])
		state.Order = state.Order[1:]
	}
}

func removeDuplicateHistoryKey(in engine.ExecuteInput, scope string) (string, error) {
	workflowID := firstNonEmptyNode(in.WorkflowID, in.WorkflowName, "workflow")
	switch scope {
	case "", "node":
		return "node:" + workflowID + ":" + in.Node.Name, nil
	case "workflow":
		return "workflow:" + workflowID, nil
	default:
		return "", fmt.Errorf("removeDuplicates: unsupported scope %s", scope)
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
	case "selectedfields", "selected-fields", "selected", "fields":
		if len(params.FieldsToCompare) == 0 {
			return nil, fmt.Errorf("removeDuplicates: fieldsToCompare is required")
		}
		return params.FieldsToCompare, nil
	case "allfieldsexcept", "all-fields-except":
		if len(params.DisabledFields) == 0 {
			return nil, fmt.Errorf("removeDuplicates: fieldsToExclude is required")
		}
		disabled := map[string]bool{}
		for _, field := range params.DisabledFields {
			disabled[field] = true
		}
		fields := removeDuplicateAllFields(items, params.DisableDotNotation)
		if !params.DisableDotNotation && len(items) > 0 {
			fields = removeDuplicateItemKeys(items[0].JSON, false)
		}
		result := make([]string, 0, len(fields))
		for _, field := range fields {
			if !disabled[field] {
				result = append(result, field)
			}
		}
		return result, nil
	default:
		return removeDuplicateAllFields(items, params.DisableDotNotation), nil
	}
}

func validateRemoveDuplicateFields(items []dataplane.Item, fields []string, params removeDuplicatesParams) error {
	for _, field := range fields {
		if strings.TrimSpace(field) == "" {
			return fmt.Errorf("removeDuplicates: name of field to compare is blank")
		}
		var seenType string
		for index, item := range items {
			value, exists := removeDuplicateFieldValueExists(item.JSON, field, params.DisableDotNotation)
			if !exists {
				if params.DisableDotNotation && strings.Contains(field, ".") {
					return fmt.Errorf("removeDuplicates: %q field is missing from some input items; if this is nested, enable dot notation", field)
				}
				return fmt.Errorf("removeDuplicates: %q field is missing from some input items", field)
			}
			if value == nil && params.NodeVersion > 1 {
				continue
			}
			currentType := removeDuplicateValueType(value)
			if seenType != "" && currentType != "" && seenType != currentType {
				return fmt.Errorf("removeDuplicates: %q isn't always the same type; item %d is %s but previous items were %s", field, index, currentType, seenType)
			}
			if currentType != "" {
				seenType = currentType
			}
		}
	}
	return nil
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
		value := removeDuplicateFieldValue(item, field, params.DisableDotNotation)
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
		value := removeDuplicateFieldValue(item, field, params.DisableDotNotation)
		if params.RemoveBlankValues && isBlankRemoveDuplicateValue(value) {
			continue
		}
		parts = append(parts, normalizeRemoveDuplicateValue(value, false))
	}
	return strings.Join(parts, " ")
}

func removeDuplicateAllFields(items []dataplane.Item, disableDotNotation bool) []string {
	seen := map[string]bool{}
	for _, item := range items {
		for _, key := range removeDuplicateItemKeys(item.JSON, disableDotNotation) {
			seen[key] = true
		}
	}
	fields := make([]string, 0, len(seen))
	for key := range seen {
		fields = append(fields, key)
	}
	sort.Strings(fields)
	return fields
}

func removeDuplicateItemKeys(data map[string]any, disableDotNotation bool) []string {
	if disableDotNotation {
		fields := make([]string, 0, len(data))
		for key := range data {
			fields = append(fields, key)
		}
		return fields
	}
	fields := []string{}
	var walk func(prefix string, value any)
	walk = func(prefix string, value any) {
		object, ok := value.(map[string]any)
		if !ok || len(object) == 0 {
			fields = append(fields, prefix)
			return
		}
		for key, child := range object {
			next := key
			if prefix != "" {
				next = prefix + "." + key
			}
			walk(next, child)
		}
	}
	for key, value := range data {
		walk(key, value)
	}
	return fields
}

func removeDuplicateFieldValue(data map[string]any, field string, disableDotNotation bool) any {
	value, _ := removeDuplicateFieldValueExists(data, field, disableDotNotation)
	return value
}

func removeDuplicateFieldValueExists(data map[string]any, field string, disableDotNotation bool) (any, bool) {
	if !disableDotNotation && strings.Contains(field, ".") {
		return nestedRemoveDuplicateValue(data, field)
	}
	value, ok := data[field]
	return value, ok
}

func nestedRemoveDuplicateValue(data map[string]any, path string) (any, bool) {
	current := any(data)
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		value, exists := object[part]
		if !exists {
			return nil, false
		}
		current = value
	}
	return current, true
}

func removeDuplicateValueType(value any) string {
	switch value.(type) {
	case nil:
		return ""
	case bool:
		return "boolean"
	case int, int64, float32, float64, json.Number:
		return "number"
	case string:
		return "string"
	default:
		return "object"
	}
}

func removeDuplicatePickFields(items []dataplane.Item, fields []string, params removeDuplicatesParams) []dataplane.Item {
	result := make([]dataplane.Item, 0, len(items))
	for index, item := range items {
		next := dataplane.Item{JSON: map[string]any{}, Binary: item.Binary, PairedItem: &dataplane.PairedItem{Item: index}, Error: item.Error}
		for _, field := range fields {
			value := removeDuplicateFieldValue(item.JSON, field, params.DisableDotNotation)
			if value == nil {
				continue
			}
			if !params.DisableDotNotation && strings.Contains(field, ".") {
				setNestedSetValue(next.JSON, field, deepCopySetValue(value))
			} else {
				next.JSON[field] = deepCopySetValue(value)
			}
		}
		result = append(result, next)
	}
	return result
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
