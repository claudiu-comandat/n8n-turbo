package nodes

import (
	"context"
	"fmt"
	"strings"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type Merge struct{}

type mergeFieldMatch struct {
	Field1 string
	Field2 string
}

type mergeParams struct {
	Mode                 string
	JoinMode             string
	FieldsToMatch        []mergeFieldMatch
	ChooseBranchInput    int
	ChooseBranchFallback string
	IncludeUnpaired      bool
	DisableDotNotation   bool
	MultipleMatches      string
	FuzzyCompare         bool
	ResolveClash         string
}

func (Merge) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	params := parseMergeParams(in.Node.Parameters)
	switch strings.ToLower(params.Mode) {
	case "", "append":
		return dataplane.MainOutput(mergeAppend(in.InputData)), nil
	case "passthrough", "pass through", "choosebranch", "choosebranchinput":
		return dataplane.MainOutput(mergeChooseBranch(in.InputData, params)), nil
	case "combinebyposition", "combinebyindex", "mergebyindex":
		return dataplane.MainOutput(mergeByPosition(in.InputData, params)), nil
	case "combinebyfields", "mergebykey", "combinebykey":
		items, err := mergeByFields(in.InputData, params)
		if err != nil {
			return nil, err
		}
		return dataplane.MainOutput(items), nil
	case "multiplex":
		return dataplane.MainOutput(mergeMultiplex(in.InputData, params)), nil
	default:
		return nil, fmt.Errorf("unsupported merge mode %q", params.Mode)
	}
}

func parseMergeParams(raw map[string]any) mergeParams {
	options := mergeObject(raw["options"])
	params := mergeParams{
		Mode:                 firstNonEmptyNode(stringParam(raw, "mode"), "append"),
		JoinMode:             firstNonEmptyNode(stringParam(raw, "joinMode"), stringParam(raw, "join"), "keepMatches"),
		ChooseBranchInput:    intParam(raw, "chooseBranchInput", intParam(raw, "input", 0)),
		ChooseBranchFallback: stringParam(raw, "chooseBranchFallback", "fallback"),
		IncludeUnpaired:      boolParam(options, "includeUnpaired", boolParam(raw, "includeUnpaired", false)),
		DisableDotNotation:   boolParam(options, "disableDotNotation", boolParam(raw, "disableDotNotation", false)),
		MultipleMatches:      firstNonEmptyNode(stringParam(options, "multipleMatches"), stringParam(raw, "multipleMatches"), "all"),
		FuzzyCompare:         boolParam(options, "fuzzyCompare", boolParam(raw, "fuzzyCompare", false)),
		ResolveClash:         mergeResolveClash(raw, options),
	}
	params.FieldsToMatch = parseMergeFields(firstNonNil(raw["fieldsToMatch"], raw["fields"], raw["matchingFields"]))
	return params
}

func mergeResolveClash(raw map[string]any, options map[string]any) string {
	for _, source := range []map[string]any{mergeObject(options["clashHandling"]), mergeObject(raw["clashHandling"])} {
		values := mergeObject(source["values"])
		if value := firstNonEmptyNode(stringParam(values, "resolveClash"), stringParam(source, "resolveClash")); value != "" {
			return value
		}
		if boolParam(values, "addSuffix", false) || boolParam(source, "addSuffix", false) {
			return "addSuffix"
		}
	}
	return firstNonEmptyNode(stringParam(options, "resolveClash"), stringParam(raw, "resolveClash"), "preferField2")
}

func parseMergeFields(raw any) []mergeFieldMatch {
	if object, ok := rawObject(raw); ok {
		for _, key := range []string{"values", "fields", "field"} {
			if values, ok := object[key].([]any); ok {
				return parseMergeFieldList(values)
			}
		}
		if match := parseMergeField(object); match.Field1 != "" {
			return []mergeFieldMatch{match}
		}
	}
	if values, ok := raw.([]any); ok {
		return parseMergeFieldList(values)
	}
	if text := strings.TrimSpace(fmt.Sprint(raw)); text != "" && text != "<nil>" {
		return []mergeFieldMatch{{Field1: text, Field2: text}}
	}
	return nil
}

func parseMergeFieldList(values []any) []mergeFieldMatch {
	fields := make([]mergeFieldMatch, 0, len(values))
	for _, value := range values {
		object, ok := rawObject(value)
		if !ok {
			continue
		}
		field := parseMergeField(object)
		if field.Field1 != "" {
			fields = append(fields, field)
		}
	}
	return fields
}

func parseMergeField(object map[string]any) mergeFieldMatch {
	field1 := firstNonEmptyNode(stringParam(object, "field1"), stringParam(object, "fieldName1"), stringParam(object, "fieldNameInput1"), stringParam(object, "field"))
	field2 := firstNonEmptyNode(stringParam(object, "field2"), stringParam(object, "fieldName2"), stringParam(object, "fieldNameInput2"), field1)
	return mergeFieldMatch{Field1: field1, Field2: field2}
}

func mergeAppend(inputs dataplane.Output) []dataplane.Item {
	items := []dataplane.Item{}
	for _, input := range inputs {
		items = append(items, input...)
	}
	return items
}

func mergeChooseBranch(inputs dataplane.Output, params mergeParams) []dataplane.Item {
	index := params.ChooseBranchInput
	if index < 0 || index >= len(inputs) {
		index = 0
	}
	chosen := mergeInput(inputs, index)
	if len(chosen) > 0 || params.ChooseBranchFallback == "" {
		return chosen
	}
	switch strings.ToLower(params.ChooseBranchFallback) {
	case "preferbranch2", "input2", "1":
		if len(mergeInput(inputs, 1)) > 0 {
			return mergeInput(inputs, 1)
		}
		return mergeInput(inputs, 0)
	case "preferbranch1", "input1", "0":
		if len(mergeInput(inputs, 0)) > 0 {
			return mergeInput(inputs, 0)
		}
		return mergeInput(inputs, 1)
	default:
		return chosen
	}
}

func mergeByPosition(inputs dataplane.Output, params mergeParams) []dataplane.Item {
	left := mergeInput(inputs, 0)
	right := mergeInput(inputs, 1)
	maxLen := len(left)
	if len(right) > maxLen {
		maxLen = len(right)
	}
	result := make([]dataplane.Item, 0, maxLen)
	for index := 0; index < maxLen; index++ {
		hasLeft := index < len(left)
		hasRight := index < len(right)
		switch {
		case hasLeft && hasRight:
			result = append(result, mergeItems(left[index], right[index], params))
		case params.IncludeUnpaired && hasLeft:
			result = append(result, left[index])
		case params.IncludeUnpaired && hasRight:
			result = append(result, right[index])
		}
	}
	return result
}

func mergeByFields(inputs dataplane.Output, params mergeParams) ([]dataplane.Item, error) {
	left := mergeInput(inputs, 0)
	right := mergeInput(inputs, 1)
	if len(params.FieldsToMatch) == 0 {
		return nil, fmt.Errorf("merge by fields requires fieldsToMatch")
	}
	index := mergeBuildIndex(right, params, true)
	matchedRight := map[int]bool{}
	result := []dataplane.Item{}
	for _, leftItem := range left {
		key := mergeMatchKey(leftItem.JSON, params, false)
		matches := index[key]
		if params.FuzzyCompare && len(matches) == 0 {
			matches = index[strings.ToLower(strings.TrimSpace(key))]
		}
		hasMatch := len(matches) > 0
		switch strings.ToLower(params.JoinMode) {
		case "", "keepmatches":
			if hasMatch {
				for _, rightIndex := range matches {
					matchedRight[rightIndex] = true
					result = append(result, mergeItems(leftItem, right[rightIndex], params))
					if strings.EqualFold(params.MultipleMatches, "first") {
						break
					}
				}
			}
		case "keepnonmatches":
			if !hasMatch {
				result = append(result, leftItem)
			}
		case "enrichinput1":
			if hasMatch {
				for _, rightIndex := range matches {
					matchedRight[rightIndex] = true
					result = append(result, mergeItems(leftItem, right[rightIndex], params))
					if strings.EqualFold(params.MultipleMatches, "first") {
						break
					}
				}
			} else {
				result = append(result, leftItem)
			}
		case "enrichinput2":
			if hasMatch {
				for _, rightIndex := range matches {
					matchedRight[rightIndex] = true
					result = append(result, mergeItems(right[rightIndex], leftItem, params))
				}
			}
		default:
			return nil, fmt.Errorf("unsupported merge join mode %q", params.JoinMode)
		}
	}
	if strings.EqualFold(params.JoinMode, "enrichInput2") {
		for index, item := range right {
			if !matchedRight[index] {
				result = append(result, item)
			}
		}
	}
	return result, nil
}

func mergeMultiplex(inputs dataplane.Output, params mergeParams) []dataplane.Item {
	left := mergeInput(inputs, 0)
	right := mergeInput(inputs, 1)
	result := make([]dataplane.Item, 0, len(left)*len(right))
	for _, leftItem := range left {
		for _, rightItem := range right {
			result = append(result, mergeItems(leftItem, rightItem, params))
		}
	}
	return result
}

func mergeBuildIndex(items []dataplane.Item, params mergeParams, useField2 bool) map[string][]int {
	index := map[string][]int{}
	for itemIndex, item := range items {
		key := mergeMatchKeyWithSide(item.JSON, params, useField2)
		index[key] = append(index[key], itemIndex)
		if params.FuzzyCompare {
			fuzzy := strings.ToLower(strings.TrimSpace(key))
			index[fuzzy] = append(index[fuzzy], itemIndex)
		}
	}
	return index
}

func mergeMatchKey(data map[string]any, params mergeParams, useField2 bool) string {
	return mergeMatchKeyWithSide(data, params, useField2)
}

func mergeMatchKeyWithSide(data map[string]any, params mergeParams, useField2 bool) string {
	parts := make([]string, 0, len(params.FieldsToMatch))
	for _, field := range params.FieldsToMatch {
		name := field.Field1
		if useField2 {
			name = firstNonEmptyNode(field.Field2, field.Field1)
		}
		value := data[name]
		if !params.DisableDotNotation && strings.Contains(name, ".") {
			value = nestedMergeValue(data, name)
		}
		part := fmt.Sprint(value)
		if params.FuzzyCompare {
			part = strings.ToLower(strings.TrimSpace(part))
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "||")
}

func mergeItems(left dataplane.Item, right dataplane.Item, params mergeParams) dataplane.Item {
	return dataplane.Item{
		JSON:       mergeJSONMaps(left.JSON, right.JSON, params.ResolveClash),
		Binary:     mergeBinaryMaps(left.Binary, right.Binary),
		PairedItem: left.PairedItem,
		Error:      firstItemError(left.Error, right.Error),
	}
}

func mergeJSONMaps(left map[string]any, right map[string]any, resolveClash string) map[string]any {
	result := deepCopySetMap(left)
	for key, value := range right {
		if existing, ok := result[key]; ok {
			switch strings.ToLower(resolveClash) {
			case "preferfield1":
				result[key] = existing
			case "addsuffix":
				delete(result, key)
				result[key+"_1"] = existing
				result[key+"_2"] = deepCopySetValue(value)
			case "keepboth":
				result[key] = []any{existing, deepCopySetValue(value)}
			default:
				result[key] = deepCopySetValue(value)
			}
		} else {
			result[key] = deepCopySetValue(value)
		}
	}
	return result
}

func mergeBinaryMaps(left map[string]dataplane.Binary, right map[string]dataplane.Binary) map[string]dataplane.Binary {
	if left == nil && right == nil {
		return nil
	}
	result := map[string]dataplane.Binary{}
	for key, value := range left {
		result[key] = value
	}
	for key, value := range right {
		if _, ok := result[key]; ok {
			result[key+"_2"] = value
		} else {
			result[key] = value
		}
	}
	return result
}

func mergeInput(inputs dataplane.Output, index int) []dataplane.Item {
	if index < 0 || index >= len(inputs) {
		return nil
	}
	return inputs[index]
}

func mergeObject(value any) map[string]any {
	object, ok := rawObject(value)
	if !ok {
		return map[string]any{}
	}
	return object
}

func nestedMergeValue(data map[string]any, path string) any {
	var current any = data
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = object[part]
	}
	return current
}

func firstItemError(left *dataplane.NodeError, right *dataplane.NodeError) *dataplane.NodeError {
	if left != nil {
		return left
	}
	return right
}
