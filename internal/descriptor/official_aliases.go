package descriptor

import "strings"

func withOfficialParamAliases(nodeType string, params map[string]any) map[string]any {
	if len(params) == 0 {
		return params
	}
	next := make(map[string]any, len(params)+4)
	for key, value := range params {
		next[key] = value
	}
	for official, descriptorName := range officialParamAliases[nodeType] {
		if _, exists := next[descriptorName]; exists {
			continue
		}
		if value, ok := next[official]; ok {
			next[descriptorName] = value
		}
	}
	if nodeType == "n8n-nodes-base.gmail" {
		if _, exists := next["isHtml"]; !exists {
			if emailType, ok := next["emailType"]; ok {
				next["isHtml"] = strings.EqualFold(valueText(emailType), "html")
			}
		}
		applyGmailParamAliases(next)
	}
	if nodeType == "n8n-nodes-base.github" {
		applyGitHubParamAliases(next)
	}
	if nodeType == "n8n-nodes-base.googleSheets" {
		applyGoogleSheetsParamAliases(next)
	}
	if nodeType == "n8n-nodes-base.slack" {
		applySlackParamAliases(next)
	}
	return next
}

func officialOperationAlias(nodeType, resource, operation string) string {
	if byResource, ok := officialOperationAliases[nodeType]; ok {
		if aliases, ok := byResource[resource]; ok {
			return aliases[operation]
		}
	}
	return ""
}

func valueText(value any) string {
	if object, ok := value.(map[string]any); ok {
		if raw, ok := object["value"]; ok {
			return stringValue(map[string]any{"value": raw}, "value")
		}
	}
	return stringValue(map[string]any{"value": value}, "value")
}

func applyGitHubParamAliases(params map[string]any) {
	if filters, ok := params["getUserIssuesFilters"].(map[string]any); ok {
		for _, key := range []string{"mentioned", "labels", "since", "state", "sort", "direction"} {
			if _, exists := params[key]; !exists {
				if value, ok := filters[key]; ok {
					params[key] = value
				}
			}
		}
	}
	if extra, ok := params["additionalParameters"].(map[string]any); ok {
		if _, exists := params["branch"]; !exists {
			if branch := nestedValue(extra, "branch", "branch"); branch != nil {
				params["branch"] = branch
			}
		}
		if _, exists := params["ref"]; !exists {
			if reference, ok := extra["reference"]; ok {
				params["ref"] = reference
			}
		}
	}
	if fields, ok := params["additionalFields"].(map[string]any); ok {
		if commitID, ok := fields["commitId"]; ok {
			fields["commit_id"] = commitID
		}
	}
}

func applyGmailParamAliases(params map[string]any) {
	options := map[string]any{}
	for _, key := range []string{"options", "filters"} {
		if value, ok := params[key].(map[string]any); ok {
			for optionKey, optionValue := range value {
				options[optionKey] = optionValue
			}
		}
	}
	for official, descriptorName := range map[string]string{
		"bccList":          "bcc",
		"ccList":           "cc",
		"fromAlias":        "from",
		"includeSpamTrash": "includeSpamTrash",
		"labelIds":         "labelIds",
		"q":                "q",
		"replyTo":          "replyTo",
		"sendTo":           "to",
		"threadId":         "threadId",
	} {
		if _, exists := params[descriptorName]; exists {
			continue
		}
		if value, ok := options[official]; ok {
			params[descriptorName] = value
		}
	}
	if _, exists := params["maxResults"]; !exists {
		if limit, ok := params["limit"]; ok {
			params["maxResults"] = limit
		}
	}
	operation := stringValue(params, "operation")
	if labels, ok := params["labelIds"]; ok {
		switch operation {
		case "addLabels":
			if _, exists := params["addLabelIds"]; !exists {
				params["addLabelIds"] = labels
			}
		case "removeLabels":
			if _, exists := params["removeLabelIds"]; !exists {
				params["removeLabelIds"] = labels
			}
		}
	}
}

func applyGoogleSheetsParamAliases(params map[string]any) {
	if _, exists := params["range"]; !exists {
		if sheetName, ok := params["sheetName"]; ok {
			params["range"] = valueText(sheetName) + "!A:Z"
		}
	}
	if _, exists := params["objects"]; !exists {
		if columns, ok := nestedValueAny(params["columns"], "value"); ok {
			params["objects"] = []any{columns}
		}
	}
}

func applySlackParamAliases(params map[string]any) {
	options := map[string]any{}
	for _, key := range []string{"options", "Options", "option", "updateFields"} {
		if value, ok := params[key].(map[string]any); ok {
			for optionKey, optionValue := range value {
				options[optionKey] = optionValue
			}
		}
	}
	for official, descriptorName := range map[string]string{
		"channelId":        "channel",
		"fileId":           "file",
		"userId":           "user",
		"userGroupId":      "usergroup",
		"returnIm":         "return_im",
		"include_count":    "include_count",
		"include_disabled": "include_disabled",
		"include_users":    "include_users",
		"channelIds":       "channels",
		"thread_ts":        "thread_ts",
		"users":            "users",
	} {
		if _, exists := params[descriptorName]; exists {
			continue
		}
		if value, ok := options[official]; ok {
			params[descriptorName] = value
		}
	}
	if stringValue(params, "resource") == "message" && stringValue(params, "operation") == "search" {
		if _, exists := params["count"]; !exists {
			if value, ok := params["limit"]; ok {
				params["count"] = value
			}
		}
	}
	if stringValue(params, "resource") == "message" && stringValue(params, "operation") == "sendAndWait" {
		if _, exists := params["channel"]; !exists && stringValue(params, "select") == "user" {
			if value, ok := params["user"]; ok {
				params["channel"] = value
			}
		}
	}
	if stringValue(params, "resource") == "user" && stringValue(params, "operation") == "updateProfile" {
		profile := map[string]any{}
		for _, key := range []string{"email", "first_name", "last_name"} {
			if value, ok := options[key]; ok {
				profile[key] = value
			}
		}
		if status, ok := options["status"].(map[string]any); ok {
			for key, value := range status {
				profile[key] = value
			}
		}
		if len(profile) > 0 {
			params["profile"] = profile
		}
	}
	if users, ok := params["users"].([]any); ok {
		values := make([]string, 0, len(users))
		for _, user := range users {
			values = append(values, valueText(user))
		}
		params["users"] = strings.Join(values, ",")
	}
}

func nestedValue(object map[string]any, keys ...string) any {
	var current any = object
	for _, key := range keys {
		asMap, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = asMap[key]
	}
	return current
}

func nestedValueAny(value any, keys ...string) (any, bool) {
	current := value
	for _, key := range keys {
		asMap, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = asMap[key]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

var officialParamAliases = map[string]map[string]string{
	"n8n-nodes-base.airtable": {
		"base":  "baseId",
		"table": "tableIdOrName",
	},
	"n8n-nodes-base.github": {
		"repository":        "repo",
		"issueNumber":       "issue_number",
		"lockReason":        "lock_reason",
		"filePath":          "path",
		"commitMessage":     "message",
		"fileContent":       "content",
		"releaseTag":        "tag_name",
		"pullRequestNumber": "pull_request_number",
		"reviewId":          "review_id",
		"workflowId":        "workflow_id",
	},
	"n8n-nodes-base.gmail": {
		"sendTo":  "to",
		"message": "body",
	},
	"n8n-nodes-base.googleSheets": {
		"documentId": "spreadsheetId",
	},
	"n8n-nodes-base.notion": {
		"blockId":    "blockId",
		"databaseId": "databaseId",
		"pageId":     "pageId",
	},
	"n8n-nodes-base.slack": {
		"channelId":   "channel",
		"fileId":      "file",
		"message":     "text",
		"userId":      "user",
		"userGroupId": "usergroup",
	},
}

var officialOperationAliases = map[string]map[string]map[string]string{
	"n8n-nodes-base.airtable": {
		"base": {
			"getMany":   "listBases",
			"getSchema": "getBaseSchema",
		},
		"record": {
			"create":       "createRecord",
			"deleteRecord": "deleteRecord",
			"get":          "getRecord",
			"search":       "listRecords",
			"update":       "updateRecord",
			"upsert":       "upsertRecord",
		},
	},
	"n8n-nodes-base.github": {
		"file": {
			"create": "createOrUpdateFile",
			"delete": "deleteFile",
			"edit":   "createOrUpdateFile",
			"get":    "getFileContent",
			"list":   "listFiles",
		},
		"issue": {
			"create":        "createIssue",
			"createComment": "createIssueComment",
			"edit":          "updateIssue",
			"get":           "getIssue",
			"lock":          "lockIssue",
		},
		"release": {
			"create": "createRelease",
			"delete": "deleteRelease",
			"get":    "getRelease",
			"getAll": "listReleases",
			"update": "updateRelease",
		},
		"repository": {
			"get":              "getRepository",
			"getIssues":        "listIssues",
			"getLicense":       "getLicense",
			"getProfile":       "getProfile",
			"getPullRequests":  "listPullRequests",
			"listPopularPaths": "listPopularPaths",
			"listReferrers":    "listReferrers",
		},
		"organization": {
			"getRepositories": "listOrgRepositories",
		},
		"review": {
			"create": "createReview",
			"get":    "getReview",
			"getAll": "listReviews",
			"update": "updateReview",
		},
		"user": {
			"getRepositories": "listUserRepositories",
			"getUserIssues":   "listUserIssues",
			"invite":          "inviteUser",
		},
		"workflow": {
			"disable":         "disableWorkflow",
			"dispatch":        "dispatchWorkflow",
			"dispatchAndWait": "dispatchWorkflow",
			"enable":          "enableWorkflow",
			"get":             "getWorkflow",
			"getUsage":        "getWorkflowUsage",
			"list":            "listWorkflows",
		},
	},
	"n8n-nodes-base.gmail": {
		"draft": {
			"create": "createDraft",
			"delete": "deleteDraft",
			"get":    "getDraft",
			"getAll": "listDrafts",
		},
		"label": {
			"create": "createLabel",
			"delete": "deleteLabel",
			"get":    "getLabel",
			"getAll": "listLabels",
		},
		"message": {
			"addLabels":    "addLabelsToMessage",
			"delete":       "deleteMessage",
			"get":          "getMessage",
			"getAll":       "listMessages",
			"markAsRead":   "markAsRead",
			"markAsUnread": "markAsUnread",
			"removeLabels": "removeLabelsFromMessage",
			"reply":        "replyMessage",
			"send":         "sendMessage",
			"sendAndWait":  "sendMessage",
		},
		"thread": {
			"addLabels":    "addLabelsToThread",
			"delete":       "deleteThread",
			"get":          "getThread",
			"getAll":       "listThreads",
			"removeLabels": "removeLabelsFromThread",
			"reply":        "replyThread",
			"trash":        "trashThread",
			"untrash":      "untrashThread",
		},
	},
	"n8n-nodes-base.googleSheets": {
		"sheet": {
			"append":         "appendValues",
			"appendOrUpdate": "appendOrUpdate",
			"clear":          "clear",
			"create":         "createSheet",
			"delete":         "deleteDimension",
			"read":           "read",
			"remove":         "removeSheet",
			"update":         "update",
		},
		"spreadsheet": {
			"create":            "createSpreadsheet",
			"deleteSpreadsheet": "deleteSpreadsheet",
		},
	},
	"n8n-nodes-base.hubspot": {
		"company": {
			"create":                    "createCompany",
			"delete":                    "deleteCompany",
			"get":                       "getCompany",
			"getAll":                    "listCompanies",
			"getRecentlyCreatedUpdated": "listCompanies",
			"searchByDomain":            "searchCompanies",
			"update":                    "updateCompany",
		},
		"contact": {
			"create":                    "createContact",
			"delete":                    "deleteContact",
			"get":                       "getContact",
			"getAll":                    "listContacts",
			"getRecentlyCreatedUpdated": "listContacts",
			"search":                    "searchContacts",
			"update":                    "updateContact",
			"upsert":                    "updateContact",
		},
		"contactList": {
			"add":    "addContactToList",
			"remove": "removeContactFromList",
		},
		"deal": {
			"create":                    "createDeal",
			"delete":                    "deleteDeal",
			"get":                       "getDeal",
			"getAll":                    "listDeals",
			"getRecentlyCreatedUpdated": "listDeals",
			"search":                    "searchDeals",
			"update":                    "updateDeal",
		},
		"engagement": {
			"create": "createEngagement",
			"delete": "deleteEngagement",
			"get":    "getEngagement",
			"getAll": "listEngagements",
		},
		"ticket": {
			"create": "createTicket",
			"delete": "deleteTicket",
			"get":    "getTicket",
			"getAll": "listTickets",
			"update": "updateTicket",
		},
	},
	"n8n-nodes-base.jira": {
		"issue": {
			"changelog":   "getIssueChangelog",
			"create":      "createIssue",
			"delete":      "deleteIssue",
			"get":         "getIssue",
			"getAll":      "searchIssues",
			"notify":      "notifyIssue",
			"transitions": "getTransitions",
			"update":      "updateIssue",
		},
		"issueAttachment": {
			"add":    "addAttachment",
			"get":    "getAttachment",
			"getAll": "listAttachments",
			"remove": "deleteAttachment",
		},
		"issueComment": {
			"add":    "addComment",
			"get":    "getComment",
			"getAll": "listComments",
			"remove": "deleteComment",
			"update": "updateComment",
		},
		"user": {
			"create": "createUser",
			"delete": "deleteUser",
			"get":    "getUser",
		},
	},
	"n8n-nodes-base.notion": {
		"block": {
			"append": "appendBlockChildren",
			"getAll": "getBlockChildren",
		},
		"database": {
			"get":    "getDatabase",
			"getAll": "queryDatabase",
			"search": "search",
		},
		"databasePage": {
			"create": "createPage",
			"get":    "getPage",
			"getAll": "queryDatabase",
			"update": "updatePage",
		},
		"page": {
			"archive": "archivePage",
			"create":  "createPage",
			"get":     "getPage",
			"search":  "search",
		},
		"user": {
			"get":    "getUser",
			"getAll": "listUsers",
		},
	},
	"n8n-nodes-base.openAi": {
		"chat": {
			"complete": "chatCompletion",
		},
		"image": {
			"create": "imageGeneration",
		},
		"text": {
			"complete": "textCompletion",
			"edit":     "editText",
			"moderate": "moderate",
		},
	},
	"n8n-nodes-base.slack": {
		"channel": {
			"archive":    "archiveChannel",
			"close":      "closeChannel",
			"create":     "createChannel",
			"get":        "getChannel",
			"getAll":     "listChannels",
			"history":    "getChannelHistory",
			"invite":     "inviteToChannel",
			"join":       "joinChannel",
			"kick":       "kickFromChannel",
			"leave":      "leaveChannel",
			"member":     "listChannelMembers",
			"open":       "openChannel",
			"rename":     "renameChannel",
			"replies":    "getChannelReplies",
			"setPurpose": "setChannelPurpose",
			"setTopic":   "setChannelTopic",
			"unarchive":  "unarchiveChannel",
		},
		"file": {
			"get":    "getFile",
			"getAll": "listFiles",
			"upload": "uploadFile",
		},
		"message": {
			"delete":       "deleteMessage",
			"getPermalink": "getPermalink",
			"post":         "postMessage",
			"search":       "searchMessages",
			"sendAndWait":  "sendMessage",
			"update":       "updateMessage",
		},
		"reaction": {
			"add":    "addReaction",
			"get":    "getReaction",
			"remove": "removeReaction",
		},
		"star": {
			"add":    "addStar",
			"delete": "removeStar",
			"getAll": "listStars",
		},
		"user": {
			"getAll":        "listUsers",
			"getPresence":   "getUserPresence",
			"getProfile":    "getUserProfile",
			"info":          "getUser",
			"updateProfile": "updateUserProfile",
		},
		"userGroup": {
			"create":      "createUserGroup",
			"disable":     "disableUserGroup",
			"enable":      "enableUserGroup",
			"getAll":      "listUserGroups",
			"getUsers":    "listUserGroupUsers",
			"update":      "updateUserGroup",
			"updateUsers": "updateUserGroupUsers",
		},
	},
	"n8n-nodes-base.stripe": {
		"balance": {
			"get": "getBalance",
		},
		"customerCard": {
			"add":    "addCustomerCard",
			"get":    "getCustomerCard",
			"remove": "removeCustomerCard",
		},
		"charge": {
			"create": "createCharge",
			"get":    "getCharge",
			"getAll": "listCharges",
			"update": "updateCharge",
		},
		"coupon": {
			"create": "createCoupon",
			"getAll": "listCoupons",
		},
		"customer": {
			"create": "createCustomer",
			"delete": "deleteCustomer",
			"get":    "getCustomer",
			"getAll": "listCustomers",
			"update": "updateCustomer",
		},
		"meterEvent": {
			"create": "createMeterEvent",
		},
		"source": {
			"create": "createSource",
			"delete": "deleteSource",
			"get":    "getSource",
		},
		"token": {
			"create": "createToken",
		},
	},
}
