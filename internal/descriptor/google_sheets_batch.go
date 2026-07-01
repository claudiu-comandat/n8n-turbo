package descriptor

import (
	"fmt"
	"strconv"
	"strings"
)

func googleSheetsBatchBody(operation Operation, params map[string]any) (map[string]any, bool, error) {
	switch operation.Name {
	case "createSheet":
		title := strings.TrimSpace(valueText(firstPresent(params, "title")))
		if title == "" {
			title = "n8n-sheet"
		}
		return map[string]any{"requests": []any{map[string]any{
			"addSheet": map[string]any{"properties": map[string]any{"title": title}},
		}}}, true, nil
	case "removeSheet":
		sheetID, err := sheetIDParam(params)
		if err != nil {
			return nil, true, err
		}
		return map[string]any{"requests": []any{map[string]any{
			"deleteSheet": map[string]any{"sheetId": sheetID},
		}}}, true, nil
	case "deleteDimension":
		sheetID, err := sheetIDParam(params)
		if err != nil {
			return nil, true, err
		}
		start, err := dimensionStartIndex(params)
		if err != nil {
			return nil, true, err
		}
		count := intValue(params, "numberToDelete", 1)
		if count <= 0 {
			count = 1
		}
		dimension := "ROWS"
		if strings.EqualFold(valueText(firstPresent(params, "toDelete")), "columns") {
			dimension = "COLUMNS"
		}
		return map[string]any{"requests": []any{map[string]any{
			"deleteDimension": map[string]any{"range": map[string]any{
				"sheetId":    sheetID,
				"dimension":  dimension,
				"startIndex": start,
				"endIndex":   start + count,
			}},
		}}}, true, nil
	default:
		return nil, false, nil
	}
}

func sheetIDParam(params map[string]any) (int, error) {
	raw := valueText(firstPresent(params, "sheetName", "sheetId"))
	id, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("sheetName must resolve to a numeric sheet id")
	}
	return id, nil
}

func dimensionStartIndex(params map[string]any) (int, error) {
	raw := strings.TrimSpace(valueText(firstPresent(params, "startIndex")))
	if raw == "" {
		return 1, nil
	}
	if strings.EqualFold(valueText(firstPresent(params, "toDelete")), "columns") {
		column, err := columnIndex(raw)
		if err != nil {
			return 0, err
		}
		return column, nil
	}
	row, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("startIndex must be a row number")
	}
	if row <= 0 {
		return 0, fmt.Errorf("startIndex must be greater than zero")
	}
	return row - 1, nil
}

func columnIndex(raw string) (int, error) {
	index := 0
	for _, r := range strings.ToUpper(strings.TrimSpace(raw)) {
		if r < 'A' || r > 'Z' {
			return 0, fmt.Errorf("startIndex must be a column letter")
		}
		index = index*26 + int(r-'A'+1)
	}
	if index == 0 {
		return 0, fmt.Errorf("startIndex must be a column letter")
	}
	return index - 1, nil
}
