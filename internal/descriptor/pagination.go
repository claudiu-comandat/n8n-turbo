package descriptor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

type PageFetcher func(pageParams map[string]any) ([]byte, error)
type LinkPageFetcher func(fullURL string) ([]byte, http.Header, error)

type PaginationHandler struct{}

func NewPaginationHandler() *PaginationHandler {
	return &PaginationHandler{}
}

func (p *PaginationHandler) CollectAll(ctx context.Context, fetcher PageFetcher, config *Pagination) ([]dataplane.Item, error) {
	if config == nil || config.Type == "" || config.Type == "none" {
		body, err := fetcher(nil)
		if err != nil {
			return nil, err
		}
		return p.extractItems(body, ""), nil
	}
	switch config.Type {
	case "link":
		return p.collectPage(ctx, fetcher, &Pagination{Type: "page", PageParam: firstText(config.PageParam, "page"), PerPageParam: firstText(config.PerPageParam, config.LimitParam, "per_page"), DataPath: config.DataPath, MaxItems: config.MaxItems, DefaultLimit: firstInt(config.DefaultLimit, 100)})
	case "cursor":
		return p.collectCursor(ctx, fetcher, config)
	case "offset":
		return p.collectOffset(ctx, fetcher, config)
	case "page":
		return p.collectPage(ctx, fetcher, config)
	case "nextPageToken":
		return p.collectNextPageToken(ctx, fetcher, config)
	default:
		return nil, fmt.Errorf("unknown pagination type %s", config.Type)
	}
}

func (p *PaginationHandler) CollectLinkPagination(ctx context.Context, initialURL string, fetcher LinkPageFetcher, config *Pagination) ([]dataplane.Item, error) {
	if config == nil {
		config = &Pagination{}
	}
	currentURL := initialURL
	var items []dataplane.Item
	for {
		select {
		case <-ctx.Done():
			return items, ctx.Err()
		default:
		}
		body, headers, err := fetcher(currentURL)
		if err != nil {
			return nil, err
		}
		pageItems := p.extractItems(body, config.DataPath)
		items = appendLimited(items, pageItems, config.MaxItems)
		if reachedLimit(items, config.MaxItems) || shouldStopWithConfig(pageItems, config) {
			return items, nil
		}
		nextURL := ParseLinkHeader(headers.Get("Link"))
		if nextURL == "" {
			return items, nil
		}
		currentURL = nextURL
	}
}

func (p *PaginationHandler) collectCursor(ctx context.Context, fetcher PageFetcher, config *Pagination) ([]dataplane.Item, error) {
	cursorParam := firstText(config.CursorParam, "cursor")
	limitParam := firstText(config.LimitParam, "limit")
	nextPath := firstText(config.NextPagePath, config.CursorPath, "next_cursor")
	limit := firstInt(config.DefaultLimit, 100)
	params := map[string]any{limitParam: limit}
	var items []dataplane.Item
	for {
		select {
		case <-ctx.Done():
			return items, ctx.Err()
		default:
		}
		body, err := fetcher(params)
		if err != nil {
			return nil, err
		}
		pageItems := p.extractItems(body, config.DataPath)
		items = appendLimited(items, pageItems, config.MaxItems)
		if reachedLimit(items, config.MaxItems) || shouldStopWithConfig(pageItems, config) {
			return items, nil
		}
		if hasMore, ok := boolPath(body, "has_more"); ok && !hasMore {
			return items, nil
		}
		next := stringPath(body, nextPath)
		if next == "" {
			return items, nil
		}
		params[cursorParam] = next
	}
}

func (p *PaginationHandler) collectOffset(ctx context.Context, fetcher PageFetcher, config *Pagination) ([]dataplane.Item, error) {
	offsetParam := firstText(config.OffsetParam, "offset")
	limitParam := firstText(config.LimitParam, "limit")
	limit := firstInt(config.DefaultLimit, 100)
	offset := 0
	var items []dataplane.Item
	for {
		select {
		case <-ctx.Done():
			return items, ctx.Err()
		default:
		}
		body, err := fetcher(map[string]any{offsetParam: offset, limitParam: limit})
		if err != nil {
			return nil, err
		}
		pageItems := p.extractItems(body, config.DataPath)
		items = appendLimited(items, pageItems, config.MaxItems)
		if reachedLimit(items, config.MaxItems) || shouldStopWithConfig(pageItems, config) || len(pageItems) < limit {
			return items, nil
		}
		offset += len(pageItems)
	}
}

func (p *PaginationHandler) collectPage(ctx context.Context, fetcher PageFetcher, config *Pagination) ([]dataplane.Item, error) {
	pageParam := firstText(config.PageParam, "page")
	perPageParam := firstText(config.PerPageParam, config.LimitParam, "per_page")
	limit := firstInt(config.DefaultLimit, 100)
	page := 1
	var items []dataplane.Item
	for {
		select {
		case <-ctx.Done():
			return items, ctx.Err()
		default:
		}
		body, err := fetcher(map[string]any{pageParam: page, perPageParam: limit})
		if err != nil {
			return nil, err
		}
		pageItems := p.extractItems(body, config.DataPath)
		items = appendLimited(items, pageItems, config.MaxItems)
		if reachedLimit(items, config.MaxItems) || shouldStopWithConfig(pageItems, config) || len(pageItems) < limit {
			return items, nil
		}
		page++
		if page > 1000 {
			return items, nil
		}
	}
}

func (p *PaginationHandler) collectNextPageToken(ctx context.Context, fetcher PageFetcher, config *Pagination) ([]dataplane.Item, error) {
	tokenParam := firstText(config.CursorParam, "pageToken")
	nextPath := firstText(config.NextPagePath, "nextPageToken")
	limitParam := firstText(config.LimitParam, "maxResults")
	limit := firstInt(config.DefaultLimit, 100)
	params := map[string]any{limitParam: limit}
	var items []dataplane.Item
	for {
		select {
		case <-ctx.Done():
			return items, ctx.Err()
		default:
		}
		body, err := fetcher(params)
		if err != nil {
			return nil, err
		}
		pageItems := p.extractItems(body, config.DataPath)
		items = appendLimited(items, pageItems, config.MaxItems)
		if reachedLimit(items, config.MaxItems) || shouldStopWithConfig(pageItems, config) {
			return items, nil
		}
		next := stringPath(body, nextPath)
		if next == "" {
			return items, nil
		}
		params[tokenParam] = next
	}
}

func (p *PaginationHandler) extractItems(body []byte, dataPath string) []dataplane.Item {
	var decoded any
	if json.Unmarshal(body, &decoded) != nil {
		return []dataplane.Item{{JSON: map[string]any{"data": string(body)}}}
	}
	if dataPath != "" {
		decoded = extractPath(decoded, dataPath)
	}
	return toItems(decoded)
}

func stringPath(body []byte, path string) string {
	var decoded any
	if json.Unmarshal(body, &decoded) != nil {
		return ""
	}
	value := extractPath(decoded, path)
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func boolPath(body []byte, path string) (bool, bool) {
	var decoded any
	if json.Unmarshal(body, &decoded) != nil {
		return false, false
	}
	value := extractPath(decoded, path)
	typed, ok := value.(bool)
	return typed, ok
}

func appendLimited(items []dataplane.Item, pageItems []dataplane.Item, maxItems int) []dataplane.Item {
	items = append(items, pageItems...)
	if maxItems > 0 && len(items) > maxItems {
		return items[:maxItems]
	}
	return items
}

func reachedLimit(items []dataplane.Item, maxItems int) bool {
	return maxItems > 0 && len(items) >= maxItems
}

func shouldStop(pageItems []dataplane.Item) bool {
	return len(pageItems) == 0
}

func shouldStopWithConfig(pageItems []dataplane.Item, _ *Pagination) bool {
	return shouldStop(pageItems)
}

func ParseLinkHeader(header string) string {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if !strings.Contains(part, `rel="next"`) && !strings.Contains(part, `rel=next`) {
			continue
		}
		start := strings.Index(part, "<")
		end := strings.Index(part, ">")
		if start >= 0 && end > start {
			return part[start+1 : end]
		}
	}
	return ""
}

func AppendPaginationParams(rawURL string, params map[string]any) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	for key, value := range params {
		query.Set(key, fmt.Sprint(value))
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func MergePaginationParams(base map[string]any, override map[string]any) map[string]any {
	result := make(map[string]any, len(base)+len(override))
	for key, value := range base {
		result[key] = value
	}
	for key, value := range override {
		result[key] = value
	}
	return result
}

func ParseIntParam(params map[string]any, key string, defaultValue int) int {
	value, ok := params[key]
	if !ok {
		return defaultValue
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		if parsed, err := strconv.Atoi(typed); err == nil {
			return parsed
		}
	}
	return defaultValue
}

func firstText(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}
