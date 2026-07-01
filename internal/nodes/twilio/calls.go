package twilio

import (
	"context"
	"fmt"
	"net/url"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func (n *Node) handleCall(ctx context.Context, cred Credential, operation string, params map[string]any) ([]dataplane.Item, error) {
	switch operation {
	case "create", "make":
		return single(n.createCall(ctx, cred, params))
	case "get":
		callSid := stringParam(params, "callSid", "sid")
		if callSid == "" {
			return nil, fmt.Errorf("callSid is required")
		}
		return single(n.doGet(ctx, cred, fmt.Sprintf("%s/Calls/%s.json", n.accountURL(cred), callSid), nil))
	case "getAll", "list":
		return itemsFromMaps(n.listCalls(ctx, cred, params))
	case "update":
		return single(n.updateCall(ctx, cred, params))
	case "delete":
		callSid := stringParam(params, "callSid", "sid")
		if callSid == "" {
			return nil, fmt.Errorf("callSid is required")
		}
		return single(n.doDelete(ctx, cred, fmt.Sprintf("%s/Calls/%s.json", n.accountURL(cred), callSid)))
	default:
		return nil, fmt.Errorf("unknown call operation %s", operation)
	}
}

func (n *Node) createCall(ctx context.Context, cred Credential, params map[string]any) (map[string]any, error) {
	from := stringParam(params, "from")
	to := stringParam(params, "to")
	callURL := stringParam(params, "url")
	twiml := stringParam(params, "twiml")
	message := stringParam(params, "message")
	if from == "" || to == "" {
		return nil, fmt.Errorf("from and to are required")
	}
	if message != "" {
		if boolParam(params, "twiml") {
			twiml = message
		} else {
			twiml = "<Response><Say>" + xmlEscape(message) + "</Say></Response>"
		}
	}
	if callURL == "" && twiml == "" {
		return nil, fmt.Errorf("message, url, or twiml is required")
	}
	form := url.Values{}
	form.Set("From", from)
	form.Set("To", to)
	formAdd(form, "Url", callURL)
	formAdd(form, "Twiml", twiml)
	formAdd(form, "Method", stringParam(params, "method"))
	formAdd(form, "StatusCallback", stringParam(params, "statusCallback"))
	formAdd(form, "StatusCallback", nestedStringParam(params, "options", "statusCallback"))
	formAdd(form, "MachineDetection", stringParam(params, "machineDetection"))
	formAddInt(form, "Timeout", intParam(params, "timeout"))
	if boolParam(params, "record") {
		form.Set("Record", "true")
	}
	return n.doFormPost(ctx, cred, n.accountURL(cred)+"/Calls.json", form)
}

func (n *Node) updateCall(ctx context.Context, cred Credential, params map[string]any) (map[string]any, error) {
	callSid := stringParam(params, "callSid", "sid")
	if callSid == "" {
		return nil, fmt.Errorf("callSid is required")
	}
	form := url.Values{}
	formAdd(form, "Status", stringParam(params, "status"))
	formAdd(form, "Url", stringParam(params, "url"))
	formAdd(form, "Method", stringParam(params, "method"))
	return n.doFormPost(ctx, cred, fmt.Sprintf("%s/Calls/%s.json", n.accountURL(cred), callSid), form)
}

func (n *Node) listCalls(ctx context.Context, cred Credential, params map[string]any) ([]map[string]any, error) {
	query := url.Values{}
	formAdd(query, "To", stringParam(params, "to"))
	formAdd(query, "From", stringParam(params, "from"))
	formAdd(query, "Status", stringParam(params, "status"))
	if limit := intParam(params, "limit"); limit > 0 {
		query.Set("PageSize", fmt.Sprint(limit))
	}
	result, err := n.doGet(ctx, cred, n.accountURL(cred)+"/Calls.json", query)
	if err != nil {
		return nil, err
	}
	items := listFrom(result, "calls")
	if boolParam(params, "returnAll") {
		return n.fetchAllPages(ctx, cred, result, items, "calls")
	}
	return items, nil
}
