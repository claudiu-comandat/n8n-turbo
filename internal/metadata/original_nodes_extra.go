package metadata

import "encoding/json"

func extraOriginalNodeDescriptions() map[string]map[string]any {
	result := map[string]map[string]any{}
	if err := json.Unmarshal([]byte(originalExtraNodeDescriptionsJSON), &result); err != nil {
		result = map[string]map[string]any{}
	}
	// Nodes reimplemented natively in Go that are not in the generated n8n catalog:
	// a polling Gmail trigger and the Cloudinary community node. Supplying their
	// descriptions here lets the editor UI render them like any built-in node.
	for name, raw := range goNativeNodeDescriptions() {
		result[name] = raw
	}
	return result
}

func goNativeNodeDescriptions() map[string]map[string]any {
	return map[string]map[string]any{
		"n8n-nodes-base.gmailTrigger": {
			"displayName": "Gmail Trigger",
			"name":        "n8n-nodes-base.gmailTrigger",
			"icon":        "fa:envelope",
			"iconColor":   "#d93025",
			"group":       []any{"trigger"},
			"version":     []any{1, 1.1, 1.2},
			"description": "Fetches emails from Gmail on a schedule (polling trigger)",
			"defaults":    map[string]any{"name": "Gmail Trigger"},
			"inputs":      []any{},
			"outputs":     []any{"main"},
			"credentials": []any{map[string]any{"name": "gmailOAuth2", "required": true}},
			"polling":     true,
			"properties": []any{
				map[string]any{
					"displayName": "Poll Times",
					"name":        "pollTimes",
					"type":        "fixedCollection",
					"typeOptions": map[string]any{"multipleValues": true, "multipleValueButtonText": "Add Poll Time"},
					"default":     map[string]any{"item": []any{map[string]any{"mode": "everyMinute"}}},
					"description": "Time at which polling should occur",
					"options": []any{map[string]any{
						"name":        "item",
						"displayName": "Item",
						"values": []any{map[string]any{
							"displayName": "Mode",
							"name":        "mode",
							"type":        "options",
							"default":     "everyMinute",
							"options": []any{
								map[string]any{"name": "Every Minute", "value": "everyMinute"},
								map[string]any{"name": "Every Hour", "value": "everyHour"},
								map[string]any{"name": "Every Day", "value": "everyDay"},
								map[string]any{"name": "Every X", "value": "everyX"},
								map[string]any{"name": "Custom (Cron)", "value": "custom"},
							},
						}},
					}},
				},
				map[string]any{"displayName": "Simple", "name": "simple", "type": "boolean", "default": true, "description": "Whether to return a simplified version of the email"},
				map[string]any{
					"displayName": "Filters",
					"name":        "filters",
					"type":        "collection",
					"placeholder": "Add Filter",
					"default":     map[string]any{},
					"options": []any{
						map[string]any{"displayName": "Search", "name": "q", "type": "string", "default": "", "description": "Gmail search query, e.g. subject:\"Order confirmed\""},
					},
				},
			},
		},
		"n8n-nodes-cloudinary.cloudinary": {
			"displayName": "Cloudinary",
			"name":        "n8n-nodes-cloudinary.cloudinary",
			"icon":        "fa:cloud-upload-alt",
			"iconColor":   "#3448c5",
			"group":       []any{"transform"},
			"version":     1,
			"description": "Upload media to Cloudinary",
			"defaults":    map[string]any{"name": "Cloudinary"},
			"inputs":      []any{"main"},
			"outputs":     []any{"main"},
			"credentials": []any{map[string]any{"name": "cloudinaryApi", "required": true}},
			"properties": []any{
				map[string]any{"displayName": "Resource", "name": "resource", "type": "options", "noDataExpression": true, "default": "file", "options": []any{map[string]any{"name": "File", "value": "file"}}},
				map[string]any{"displayName": "Operation", "name": "operation", "type": "options", "noDataExpression": true, "default": "uploadFile", "options": []any{map[string]any{"name": "Upload", "value": "uploadFile", "action": "Upload a file"}}},
				map[string]any{"displayName": "Binary Property", "name": "binaryPropertyName", "type": "string", "default": "data", "description": "Name of the binary property holding the file to upload (ignored if a file URL is provided)"},
				map[string]any{
					"displayName": "Additional Fields",
					"name":        "additionalFieldsFile",
					"type":        "collection",
					"placeholder": "Add Field",
					"default":     map[string]any{},
					"options": []any{
						map[string]any{"displayName": "Public ID", "name": "public_id", "type": "string", "default": "", "description": "Identifier for the uploaded asset"},
						map[string]any{"displayName": "Folder", "name": "folder", "type": "string", "default": ""},
						map[string]any{"displayName": "Overwrite", "name": "overwrite", "type": "boolean", "default": true},
					},
				},
			},
		},
	}
}
