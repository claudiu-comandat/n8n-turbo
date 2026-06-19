package twilio

import (
	"context"
	"fmt"
	"net/url"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func (n *Node) handleVerify(ctx context.Context, cred Credential, operation string, params map[string]any) ([]dataplane.Item, error) {
	serviceSid := stringParam(params, "serviceSid")
	if serviceSid == "" {
		return nil, fmt.Errorf("serviceSid is required")
	}
	switch operation {
	case "send":
		return single(n.sendVerification(ctx, cred, serviceSid, params))
	case "check":
		return single(n.checkVerification(ctx, cred, serviceSid, params))
	default:
		return nil, fmt.Errorf("unknown verify operation %s", operation)
	}
}

func (n *Node) sendVerification(ctx context.Context, cred Credential, serviceSid string, params map[string]any) (map[string]any, error) {
	to := stringParam(params, "to")
	if to == "" {
		return nil, fmt.Errorf("to is required")
	}
	channel := stringParam(params, "channel")
	if channel == "" {
		channel = "sms"
	}
	form := url.Values{}
	form.Set("To", to)
	form.Set("Channel", channel)
	formAdd(form, "From", stringParam(params, "from"))
	return n.doFormPost(ctx, cred, n.verifyURL(cred, serviceSid)+"/Verifications", form)
}

func (n *Node) checkVerification(ctx context.Context, cred Credential, serviceSid string, params map[string]any) (map[string]any, error) {
	to := stringParam(params, "to")
	code := stringParam(params, "code")
	if to == "" || code == "" {
		return nil, fmt.Errorf("to and code are required")
	}
	form := url.Values{}
	form.Set("To", to)
	form.Set("Code", code)
	result, err := n.doFormPost(ctx, cred, n.verifyURL(cred, serviceSid)+"/VerificationCheck", form)
	if err != nil {
		return nil, err
	}
	result["verified"] = result["status"] == "approved"
	return result, nil
}
