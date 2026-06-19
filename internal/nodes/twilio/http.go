package twilio

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type Error struct {
	Code     int    `json:"code"`
	Message  string `json:"message"`
	MoreInfo string `json:"more_info"`
	Status   int    `json:"status"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("twilio error %d: %s", e.Code, e.Message)
}

func (n *Node) doFormPost(ctx context.Context, cred Credential, apiURL string, form url.Values) (map[string]any, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", cred.BasicAuth())
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	return n.do(request)
}

func (n *Node) doGet(ctx context.Context, cred Credential, apiURL string, query url.Values) (map[string]any, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, appendQuery(apiURL, query), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", cred.BasicAuth())
	request.Header.Set("Accept", "application/json")
	return n.do(request)
}

func (n *Node) doDelete(ctx context.Context, cred Credential, apiURL string) (map[string]any, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, apiURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", cred.BasicAuth())
	request.Header.Set("Accept", "application/json")
	return n.do(request)
}

func (n *Node) do(request *http.Request) (map[string]any, error) {
	response, err := n.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return map[string]any{"success": true}, nil
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 16*1024*1024))
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 400 {
		apiErr := &Error{Status: response.StatusCode}
		_ = json.Unmarshal(data, apiErr)
		if apiErr.Message == "" {
			apiErr.Message = string(data)
		}
		return nil, apiErr
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (n *Node) fetchAllPages(ctx context.Context, cred Credential, first map[string]any, initial []map[string]any, key string) ([]map[string]any, error) {
	results := append([]map[string]any{}, initial...)
	current := first
	for {
		next, _ := current["next_page_uri"].(string)
		if next == "" {
			break
		}
		pageURL := rootFromBase(n.rootURL()) + next
		page, err := n.doGet(ctx, cred, pageURL, nil)
		if err != nil {
			return nil, err
		}
		results = append(results, listFrom(page, key)...)
		current = page
	}
	return results, nil
}
