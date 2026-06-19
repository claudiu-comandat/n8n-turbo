package descriptor

import (
	"fmt"
	"strings"
)

func JiraTextToADF(text string) map[string]any {
	paragraphs := strings.Split(text, "\n\n")
	content := make([]map[string]any, 0, len(paragraphs))
	for _, paragraph := range paragraphs {
		paragraph = strings.TrimSpace(paragraph)
		if paragraph == "" {
			continue
		}
		content = append(content, map[string]any{
			"type":    "paragraph",
			"content": []map[string]any{{"type": "text", "text": paragraph}},
		})
	}
	if len(content) == 0 {
		content = append(content, map[string]any{"type": "paragraph", "content": []map[string]any{}})
	}
	return map[string]any{"type": "doc", "version": 1, "content": content}
}

func JiraADFText(value any) string {
	document, ok := mapFromAny(value)
	if !ok {
		return ""
	}
	parts := []string{}
	for _, block := range sliceFromAny(document["content"]) {
		blockMap, ok := mapFromAny(block)
		if !ok {
			continue
		}
		for _, inline := range sliceFromAny(blockMap["content"]) {
			inlineMap, ok := mapFromAny(inline)
			if ok {
				if text := stringFromAny(inlineMap["text"]); text != "" {
					parts = append(parts, text)
				}
			}
		}
	}
	return strings.Join(parts, " ")
}

type JiraIssueFlattener struct{}

func (JiraIssueFlattener) Flatten(issue map[string]any) map[string]any {
	result := map[string]any{"id": issue["id"], "key": issue["key"], "url": issue["self"]}
	fields, ok := mapFromAny(issue["fields"])
	if !ok {
		return result
	}
	result["summary"] = fields["summary"]
	result["description"] = JiraADFText(fields["description"])
	if status, ok := mapFromAny(fields["status"]); ok {
		result["status"] = status["name"]
		if category, ok := mapFromAny(status["statusCategory"]); ok {
			result["statusCategory"] = category["name"]
		}
	}
	if priority, ok := mapFromAny(fields["priority"]); ok {
		result["priority"] = priority["name"]
	}
	if issueType, ok := mapFromAny(fields["issuetype"]); ok {
		result["issueType"] = issueType["name"]
	}
	if assignee, ok := mapFromAny(fields["assignee"]); ok {
		result["assignee"] = assignee["displayName"]
		result["assigneeAccountId"] = assignee["accountId"]
	}
	if reporter, ok := mapFromAny(fields["reporter"]); ok {
		result["reporter"] = reporter["displayName"]
	}
	if project, ok := mapFromAny(fields["project"]); ok {
		result["project"] = project["key"]
		result["projectName"] = project["name"]
	}
	result["created"] = fields["created"]
	result["updated"] = fields["updated"]
	result["dueDate"] = fields["duedate"]
	result["labels"] = fields["labels"]
	return result
}

func JiraBuildIssueFields(projectKey string, summary string, issueType string, description string, priority string, assigneeAccountID string, labels []any) map[string]any {
	fields := map[string]any{}
	if projectKey != "" {
		fields["project"] = map[string]string{"key": projectKey}
	}
	if summary != "" {
		fields["summary"] = summary
	}
	if issueType != "" {
		fields["issuetype"] = map[string]string{"name": issueType}
	}
	if description != "" {
		fields["description"] = JiraTextToADF(description)
	}
	if priority != "" {
		fields["priority"] = map[string]string{"name": priority}
	}
	if assigneeAccountID != "" {
		fields["assignee"] = map[string]string{"accountId": assigneeAccountID}
	}
	if len(labels) > 0 {
		fields["labels"] = labels
	}
	return fields
}

func JiraBuildJQL(conditions ...string) string {
	valid := make([]string, 0, len(conditions))
	for _, condition := range conditions {
		condition = strings.TrimSpace(condition)
		if condition != "" {
			valid = append(valid, condition)
		}
	}
	if len(valid) == 0 {
		return "ORDER BY created DESC"
	}
	return strings.Join(valid, " AND ") + " ORDER BY created DESC"
}

func JiraProjectCondition(projectKey string) string {
	return fmt.Sprintf("project = %s", projectKey)
}

func JiraStatusCondition(status string) string {
	return fmt.Sprintf("status = \"%s\"", strings.ReplaceAll(status, `"`, `\"`))
}

func JiraAssigneeCurrentUser() string {
	return "assignee = currentUser()"
}

func JiraRecentlyUpdated(days int) string {
	if days < 0 {
		days = 0
	}
	return fmt.Sprintf("updated >= -%dd", days)
}
