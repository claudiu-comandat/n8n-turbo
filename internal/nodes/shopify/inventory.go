package shopify

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func (n *Node) handleInventory(ctx context.Context, cred Credential, operation string, params map[string]any) ([]dataplane.Item, error) {
	switch operation {
	case OpGet:
		return singleValue(n.getInventoryLevel(ctx, cred, params))
	case OpGetAll, "list":
		return n.listInventoryLevels(ctx, cred, params)
	case OpAdjust:
		return singleValue(n.adjustInventory(ctx, cred, params))
	case OpSet:
		return singleValue(n.setInventory(ctx, cred, params))
	default:
		return nil, fmt.Errorf("unknown inventory operation %s", operation)
	}
}

func (n *Node) getInventoryLevel(ctx context.Context, cred Credential, params map[string]any) (map[string]any, error) {
	inventoryItemID := int64Param(params, "inventoryItemId")
	locationID := int64Param(params, "locationId")
	if inventoryItemID == 0 || locationID == 0 {
		return nil, fmt.Errorf("inventoryItemId and locationId are required")
	}
	query := url.Values{"inventory_item_ids": {fmt.Sprint(inventoryItemID)}, "location_ids": {fmt.Sprint(locationID)}}
	result, err := n.doJSON(ctx, cred, http.MethodGet, "/inventory_levels.json?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	levels := listFrom(result, "inventory_levels")
	if len(levels) == 0 {
		return nil, fmt.Errorf("inventory level not found")
	}
	if object, ok := levels[0].(map[string]any); ok {
		return object, nil
	}
	return nil, fmt.Errorf("invalid inventory level")
}

func (n *Node) listInventoryLevels(ctx context.Context, cred Credential, params map[string]any) ([]dataplane.Item, error) {
	query := url.Values{}
	if value := int64Param(params, "inventoryItemId"); value != 0 {
		query.Set("inventory_item_ids", fmt.Sprint(value))
	}
	if value := int64Param(params, "locationId"); value != 0 {
		query.Set("location_ids", fmt.Sprint(value))
	}
	limit := intParam(params, "limit")
	if limit <= 0 {
		limit = 250
	}
	query.Set("limit", fmt.Sprint(limit))
	result, err := n.doJSON(ctx, cred, http.MethodGet, "/inventory_levels.json?"+query.Encode(), nil)
	return itemsFromArray(listFrom(result, "inventory_levels"), err)
}

func (n *Node) adjustInventory(ctx context.Context, cred Credential, params map[string]any) (map[string]any, error) {
	body := map[string]any{
		"location_id":          int64Param(params, "locationId"),
		"inventory_item_id":    int64Param(params, "inventoryItemId"),
		"available_adjustment": intParam(params, "adjustment"),
	}
	if body["location_id"] == int64(0) || body["inventory_item_id"] == int64(0) {
		return nil, fmt.Errorf("locationId and inventoryItemId are required")
	}
	result, err := n.doJSON(ctx, cred, http.MethodPost, "/inventory_levels/adjust.json", body)
	if err != nil {
		return nil, err
	}
	if object, ok := result["inventory_level"].(map[string]any); ok {
		return object, nil
	}
	return result, nil
}

func (n *Node) setInventory(ctx context.Context, cred Credential, params map[string]any) (map[string]any, error) {
	body := map[string]any{
		"location_id":       int64Param(params, "locationId"),
		"inventory_item_id": int64Param(params, "inventoryItemId"),
		"available":         intParam(params, "available"),
	}
	if body["location_id"] == int64(0) || body["inventory_item_id"] == int64(0) {
		return nil, fmt.Errorf("locationId and inventoryItemId are required")
	}
	result, err := n.doJSON(ctx, cred, http.MethodPost, "/inventory_levels/set.json", body)
	if err != nil {
		return nil, err
	}
	if object, ok := result["inventory_level"].(map[string]any); ok {
		return object, nil
	}
	return result, nil
}
