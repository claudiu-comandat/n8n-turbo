package descriptor

import (
	"fmt"
	"sort"
	"strings"
)

func sheetsRowsToObjects(values []any) []any {
	if len(values) == 0 {
		return nil
	}
	rawHeaders := sheetRow(values[0])
	headers := make([]string, len(rawHeaders))
	for index, header := range rawHeaders {
		headers[index] = sheetHeader(header)
	}
	result := make([]any, 0, len(values)-1)
	for _, raw := range values[1:] {
		row := sheetRow(raw)
		object := make(map[string]any, len(headers))
		for index, header := range headers {
			if header == "" {
				continue
			}
			if index < len(row) {
				object[header] = row[index]
			} else {
				object[header] = ""
			}
		}
		result = append(result, object)
	}
	return result
}

func sheetsObjectsToRows(objects []map[string]any, headerOrder []string) []any {
	if len(objects) == 0 {
		return nil
	}
	headers := append([]string(nil), headerOrder...)
	if len(headers) == 0 {
		for key := range objects[0] {
			headers = append(headers, key)
		}
		sort.Strings(headers)
	}
	rows := make([]any, 0, len(objects)+1)
	headerRow := make([]any, len(headers))
	for index, header := range headers {
		headerRow[index] = header
	}
	rows = append(rows, headerRow)
	for _, object := range objects {
		row := make([]any, len(headers))
		for index, header := range headers {
			if value, ok := object[header]; ok {
				row[index] = value
			} else {
				row[index] = ""
			}
		}
		rows = append(rows, row)
	}
	return rows
}

type SheetMapper struct{}

func (SheetMapper) RowsToObjects(values [][]any) []map[string]any {
	if len(values) == 0 {
		return nil
	}
	rows := make([]any, len(values))
	for index, row := range values {
		rows[index] = row
	}
	raw := sheetsRowsToObjects(rows)
	result := make([]map[string]any, 0, len(raw))
	for _, entry := range raw {
		if object, ok := entry.(map[string]any); ok {
			result = append(result, object)
		}
	}
	return result
}

func (SheetMapper) ObjectsToRows(objects []map[string]any, headerOrder []string) [][]any {
	raw := sheetsObjectsToRows(objects, headerOrder)
	rows := make([][]any, 0, len(raw))
	for _, entry := range raw {
		rows = append(rows, sheetRow(entry))
	}
	return rows
}

func (SheetMapper) SingleObjectToRow(object map[string]any, headerOrder []string) []any {
	row := make([]any, len(headerOrder))
	for index, key := range headerOrder {
		if value, ok := object[key]; ok {
			row[index] = value
		} else {
			row[index] = ""
		}
	}
	return row
}

func ParseSheetRange(rangeValue string) (string, string) {
	for index, char := range rangeValue {
		if char == '!' {
			return rangeValue[:index], rangeValue[index+1:]
		}
	}
	return "", rangeValue
}

func BuildSheetsAppendBody(objects []map[string]any, headerOrder []string) map[string]any {
	mapper := SheetMapper{}
	rows := make([]any, 0, len(objects))
	if len(headerOrder) == 0 && len(objects) > 0 {
		for key := range objects[0] {
			headerOrder = append(headerOrder, key)
		}
		sort.Strings(headerOrder)
	}
	for _, object := range objects {
		rows = append(rows, mapper.SingleObjectToRow(object, headerOrder))
	}
	return map[string]any{"values": rows}
}

func sheetObjectsFromAny(value any) ([]map[string]any, bool) {
	if object, ok := value.(map[string]any); ok {
		return []map[string]any{object}, true
	}
	values, ok := value.([]any)
	if !ok {
		return nil, false
	}
	objects := make([]map[string]any, 0, len(values))
	for _, entry := range values {
		object, ok := entry.(map[string]any)
		if !ok {
			return nil, false
		}
		objects = append(objects, object)
	}
	return objects, true
}

func sheetHeaderOrderFromAny(value any) []string {
	values, ok := value.([]any)
	if ok {
		headers := make([]string, 0, len(values))
		for _, entry := range values {
			headers = append(headers, fmt.Sprint(entry))
		}
		return headers
	}
	if values, ok := value.([]string); ok {
		return append([]string(nil), values...)
	}
	if text, ok := value.(string); ok && text != "" {
		return strings.Split(text, ",")
	}
	return nil
}

func sheetRow(value any) []any {
	if row, ok := value.([]any); ok {
		return row
	}
	return []any{value}
}

func sheetHeader(value any) string {
	return fmt.Sprint(value)
}
