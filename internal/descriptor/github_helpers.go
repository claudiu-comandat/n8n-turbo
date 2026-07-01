package descriptor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

func DecodeGitHubFileContent(item map[string]any) (string, error) {
	content, _ := item["content"].(string)
	encoding, _ := item["encoding"].(string)
	if !strings.EqualFold(encoding, "base64") {
		return content, nil
	}
	content = strings.ReplaceAll(content, "\n", "")
	content = strings.ReplaceAll(content, "\r", "")
	decoded, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		return "", fmt.Errorf("github file content decode: %w", err)
	}
	return string(decoded), nil
}

func EncodeGitHubFileContent(content string) string {
	return base64.StdEncoding.EncodeToString([]byte(content))
}

func (e Executor) withGitHubFileSHA(ctx context.Context, params map[string]any, credentials map[string]map[string]any) (map[string]any, error) {
	if _, ok := params["sha"]; ok {
		return params, nil
	}
	lookup, ok := e.descriptor.Operations["getFileContent"]
	if !ok {
		return params, fmt.Errorf("github delete file: getFileContent operation missing")
	}
	raw, _, _, err := e.executeRaw(ctx, lookup, params, credentials)
	if err != nil {
		return params, err
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return params, err
	}
	sha := valueText(decoded["sha"])
	if sha == "" {
		return params, fmt.Errorf("github delete file: file sha missing")
	}
	next := mergeParams(params, map[string]any{"sha": sha})
	return next, nil
}

func githubRequestBody(operation Operation, params map[string]any) (map[string]any, bool, error) {
	switch operation.Name {
	case "createIssue":
		body := map[string]any{}
		for _, name := range []string{"title", "body", "milestone"} {
			if value, ok := descriptorParamValue(params, Param{Name: name}); ok {
				body[name] = value
			}
		}
		if labels, ok := labelsFromGitHubCollection(params["labels"], "label"); ok {
			body["labels"] = labels
		}
		if assignees, ok := labelsFromGitHubCollection(params["assignees"], "assignee"); ok {
			body["assignees"] = assignees
		}
		return body, len(body) > 0, nil
	case "updateIssue":
		if fields, ok := params["editFields"]; ok {
			body, err := objectParam(fields)
			if err != nil {
				return nil, true, err
			}
			if labels, ok := labelsFromGitHubCollection(body["labels"], "label"); ok {
				body["labels"] = labels
			}
			if assignees, ok := labelsFromGitHubCollection(body["assignees"], "assignee"); ok {
				body["assignees"] = assignees
			}
			return body, true, nil
		}
		return nil, false, nil
	case "createRelease":
		body, err := objectParam(params["additionalFields"])
		if err != nil {
			return nil, true, err
		}
		if tag, ok := descriptorParamValue(params, Param{Name: "tag_name"}); ok {
			body["tag_name"] = tag
		}
		return body, len(body) > 0, nil
	case "createOrUpdateFile":
		body := map[string]any{}
		if message, ok := descriptorParamValue(params, Param{Name: "message"}); ok {
			body["message"] = message
		}
		if content, ok := descriptorParamValue(params, Param{Name: "content"}); ok {
			if _, official := params["fileContent"]; official {
				body["content"] = EncodeGitHubFileContent(fmt.Sprint(content))
			} else {
				body["content"] = content
			}
		}
		for _, name := range []string{"sha", "branch"} {
			if value, ok := descriptorParamValue(params, Param{Name: name}); ok {
				body[name] = value
			}
		}
		return body, len(body) > 0, nil
	case "deleteFile":
		body := map[string]any{}
		for _, name := range []string{"message", "sha", "branch"} {
			if value, ok := descriptorParamValue(params, Param{Name: name}); ok {
				body[name] = value
			}
		}
		return body, len(body) > 0, nil
	case "updateRelease":
		fields, err := objectParam(params["additionalFields"])
		if err != nil {
			return nil, true, err
		}
		return fields, true, nil
	case "createReview":
		body, err := objectParam(params["additionalFields"])
		if err != nil {
			return nil, true, err
		}
		event := strings.ToUpper(strings.ReplaceAll(valueText(params["event"]), "requestChanges", "request_changes"))
		if event != "" {
			body["event"] = event
		}
		if event == "REQUEST_CHANGES" || event == "COMMENT" {
			if text, ok := descriptorParamValue(params, Param{Name: "body"}); ok {
				body["body"] = text
			}
		}
		return body, true, nil
	case "dispatchWorkflow":
		body := map[string]any{"ref": "main"}
		if ref, ok := descriptorParamValue(params, Param{Name: "ref", Default: "main"}); ok {
			body["ref"] = ref
		}
		if inputs, ok := params["inputs"]; ok && inputs != nil {
			value, err := objectParam(inputs)
			if err != nil {
				return nil, true, err
			}
			if len(value) > 0 {
				body["inputs"] = value
			}
		}
		return body, true, nil
	default:
		return nil, false, nil
	}
}

func labelsFromGitHubCollection(value any, key string) ([]string, bool) {
	switch typed := value.(type) {
	case []any:
		values := make([]string, 0, len(typed))
		for _, entry := range typed {
			if object, ok := entry.(map[string]any); ok {
				values = append(values, valueText(object[key]))
			} else {
				values = append(values, valueText(entry))
			}
		}
		return values, true
	case []string:
		return typed, true
	default:
		return nil, false
	}
}

func objectParam(value any) (map[string]any, error) {
	switch typed := value.(type) {
	case nil:
		return map[string]any{}, nil
	case map[string]any:
		return typed, nil
	case string:
		if strings.TrimSpace(typed) == "" {
			return map[string]any{}, nil
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(typed), &decoded); err != nil {
			return nil, err
		}
		return decoded, nil
	default:
		return nil, fmt.Errorf("expected JSON object, got %T", value)
	}
}
