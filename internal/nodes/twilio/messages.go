package twilio

import (
	"context"
	"fmt"
	"net/url"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func (n *Node) handleMessage(ctx context.Context, cred Credential, operation string, params map[string]any) ([]dataplane.Item, error) {
	switch operation {
	case "send", "create":
		return single(n.sendSMS(ctx, cred, params))
	case "get":
		messageSid := stringParam(params, "messageSid", "sid")
		if messageSid == "" {
			return nil, fmt.Errorf("messageSid is required")
		}
		return single(n.doGet(ctx, cred, fmt.Sprintf("%s/Messages/%s.json", n.accountURL(cred), messageSid), nil))
	case "getAll", "list":
		return itemsFromMaps(n.listSMS(ctx, cred, params))
	case "delete":
		messageSid := stringParam(params, "messageSid", "sid")
		if messageSid == "" {
			return nil, fmt.Errorf("messageSid is required")
		}
		result, err := n.doDelete(ctx, cred, fmt.Sprintf("%s/Messages/%s.json", n.accountURL(cred), messageSid))
		if err == nil {
			result["sid"] = messageSid
		}
		return single(result, err)
	default:
		return nil, fmt.Errorf("unknown sms operation %s", operation)
	}
}

func (n *Node) sendSMS(ctx context.Context, cred Credential, params map[string]any) (map[string]any, error) {
	from := stringParam(params, "from")
	to := stringParam(params, "to")
	body := stringParam(params, "message", "body")
	if to == "" {
		return nil, fmt.Errorf("to is required")
	}
	if body == "" {
		return nil, fmt.Errorf("message body is required")
	}
	form := url.Values{}
	form.Set("To", to)
	form.Set("Body", body)
	if messagingService := stringParam(params, "messagingServiceSid"); messagingService != "" {
		form.Set("MessagingServiceSid", messagingService)
	} else {
		if from == "" {
			return nil, fmt.Errorf("from is required")
		}
		form.Set("From", from)
	}
	formAdd(form, "MediaUrl", stringParam(params, "mediaUrl"))
	formAdd(form, "StatusCallback", stringParam(params, "statusCallback"))
	formAdd(form, "ScheduleType", stringParam(params, "scheduleType"))
	formAdd(form, "SendAt", stringParam(params, "sendAt"))
	return n.doFormPost(ctx, cred, n.accountURL(cred)+"/Messages.json", form)
}

func (n *Node) listSMS(ctx context.Context, cred Credential, params map[string]any) ([]map[string]any, error) {
	query := url.Values{}
	formAdd(query, "To", stringParam(params, "to"))
	formAdd(query, "From", stringParam(params, "from"))
	formAdd(query, "DateSent>", stringParam(params, "dateSentAfter"))
	if limit := intParam(params, "limit"); limit > 0 {
		query.Set("PageSize", fmt.Sprint(limit))
	}
	result, err := n.doGet(ctx, cred, n.accountURL(cred)+"/Messages.json", query)
	if err != nil {
		return nil, err
	}
	items := listFrom(result, "messages")
	if boolParam(params, "returnAll") {
		return n.fetchAllPages(ctx, cred, result, items, "messages")
	}
	return items, nil
}
