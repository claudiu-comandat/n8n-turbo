package shopify

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

var linkRegex = regexp.MustCompile(`<([^>]+)>;\s*rel="(\w+)"`)

func ParseLinkHeader(header string) map[string]string {
	links := make(map[string]string)
	matches := linkRegex.FindAllStringSubmatch(header, -1)
	for _, match := range matches {
		if len(match) == 3 {
			links[match[2]] = match[1]
		}
	}
	return links
}

func (n *Node) fetchAllPages(ctx context.Context, cred Credential, firstPath string, listKey string) ([]dataplane.Item, error) {
	items := []dataplane.Item{}
	current := firstPath
	for {
		response, err := n.do(ctx, cred, "GET", current, nil)
		if err != nil {
			return nil, fmt.Errorf("page fetch: %w", err)
		}
		var page map[string]any
		decodeErr := json.NewDecoder(response.Body).Decode(&page)
		response.Body.Close()
		if decodeErr != nil {
			return nil, decodeErr
		}
		for _, value := range listFrom(page, listKey) {
			if object, ok := value.(map[string]any); ok {
				items = append(items, dataplane.Item{JSON: object})
			}
		}
		links := ParseLinkHeader(response.Header.Get("Link"))
		next := links["next"]
		if next == "" {
			return items, nil
		}
		parsed, err := url.Parse(next)
		if err != nil {
			return items, nil
		}
		current = parsed.RequestURI()
		prefix := "/admin/api/" + cred.APIVersion
		current = strings.TrimPrefix(current, prefix)
	}
}
