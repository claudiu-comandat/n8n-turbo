package descriptor

func airtableRequestBody(operation Operation, params map[string]any) (map[string]any, bool, error) {
	if operation.Name != "upsertRecord" {
		return nil, false, nil
	}
	fields, _ := nestedValueAny(params["columns"], "value")
	if fields == nil {
		fields = params["fields"]
	}
	body := map[string]any{
		"records": []any{map[string]any{"fields": fields}},
	}
	if typecast, ok := descriptorParamValue(params, Param{Name: "typecast"}); ok {
		body["typecast"] = typecast
	}
	if matching, ok := nestedValueAny(params["columns"], "matchingColumns"); ok {
		body["performUpsert"] = map[string]any{"fieldsToMergeOn": matching}
	}
	return body, true, nil
}
