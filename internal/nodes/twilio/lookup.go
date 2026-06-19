package twilio

import (
	"context"
	"fmt"
	"net/url"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func (n *Node) handleLookup(ctx context.Context, cred Credential, params map[string]any) ([]dataplane.Item, error) {
	phone := stringParam(params, "phoneNumber", "to")
	if phone == "" {
		return nil, fmt.Errorf("phoneNumber is required")
	}
	query := url.Values{}
	if boolParam(params, "fetchCallerName") {
		query.Add("Type", "caller-name")
	}
	if value, ok := params["fetchLineType"]; !ok || value == true {
		query.Add("Type", "line-type-intelligence")
	}
	formAdd(query, "CountryCode", stringParam(params, "countryCode"))
	return single(n.doGet(ctx, cred, n.lookupURL(cred)+"/"+url.PathEscape(phone), query))
}
