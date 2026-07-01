package descriptor

func gmailRequestBody(operation Operation, params map[string]any) (map[string]any, bool, error) {
	switch operation.Name {
	case "createDraft":
		raw, err := buildGmailRawMessageWithOptions(params, true)
		if err != nil {
			return nil, true, err
		}
		message := map[string]any{"raw": raw}
		if threadID := stringValue(params, "threadId"); threadID != "" {
			message["threadId"] = threadID
		}
		return map[string]any{"message": message}, true, nil
	case "replyMessage", "replyThread":
		raw, err := buildGmailRawMessageWithOptions(params, true)
		if err != nil {
			return nil, true, err
		}
		body := map[string]any{"raw": raw}
		if threadID := stringValue(params, "threadId"); threadID != "" {
			body["threadId"] = threadID
		}
		return body, true, nil
	default:
		return nil, false, nil
	}
}
