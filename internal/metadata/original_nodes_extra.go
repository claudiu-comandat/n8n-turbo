package metadata

import "encoding/json"

func extraOriginalNodeDescriptions() map[string]map[string]any {
	result := map[string]map[string]any{}
	if err := json.Unmarshal([]byte(originalExtraNodeDescriptionsJSON), &result); err != nil {
		return map[string]map[string]any{}
	}
	return result
}
