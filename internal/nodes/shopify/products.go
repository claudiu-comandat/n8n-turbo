package shopify

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func (n *Node) handleProduct(ctx context.Context, cred Credential, operation string, params map[string]any, item dataplane.Item) ([]dataplane.Item, error) {
	switch operation {
	case OpGetAll, "list":
		return n.listProducts(ctx, cred, params)
	case OpGet:
		id := int64Param(params, "productId")
		if id == 0 {
			return nil, fmt.Errorf("productId is required")
		}
		result, err := n.doJSON(ctx, cred, http.MethodGet, fmt.Sprintf("/products/%d.json", id), nil)
		return singleValue(result["product"], err)
	case OpCreate:
		return singleValue(n.createProduct(ctx, cred, params, item))
	case OpUpdate:
		return singleValue(n.updateProduct(ctx, cred, params))
	case OpDelete:
		id := int64Param(params, "productId")
		if id == 0 {
			return nil, fmt.Errorf("productId is required")
		}
		return singleValue(n.doJSON(ctx, cred, http.MethodDelete, fmt.Sprintf("/products/%d.json", id), nil))
	default:
		return nil, fmt.Errorf("unknown product operation %s", operation)
	}
}

func (n *Node) listProducts(ctx context.Context, cred Credential, params map[string]any) ([]dataplane.Item, error) {
	query := url.Values{}
	limit := intParam(params, "limit")
	if limit <= 0 {
		limit = 50
	}
	query.Set("limit", fmt.Sprint(limit))
	for param, queryName := range map[string]string{
		"status":      "status",
		"vendor":      "vendor",
		"productType": "product_type",
		"ids":         "ids",
		"fields":      "fields",
	} {
		if value := stringParam(params, param); value != "" {
			query.Set(queryName, value)
		}
	}
	path := "/products.json?" + query.Encode()
	if boolParam(params, "returnAll") {
		return n.fetchAllPages(ctx, cred, path, "products")
	}
	result, err := n.doJSON(ctx, cred, http.MethodGet, path, nil)
	return itemsFromArray(listFrom(result, "products"), err)
}

func (n *Node) createProduct(ctx context.Context, cred Credential, params map[string]any, item dataplane.Item) (map[string]any, error) {
	product, err := productBody(params, item, false)
	if err != nil {
		return nil, err
	}
	result, err := n.doJSON(ctx, cred, http.MethodPost, "/products.json", map[string]any{"product": product})
	if err != nil {
		return nil, err
	}
	if object, ok := result["product"].(map[string]any); ok {
		return object, nil
	}
	return result, nil
}

func (n *Node) updateProduct(ctx context.Context, cred Credential, params map[string]any) (map[string]any, error) {
	id := int64Param(params, "productId")
	if id == 0 {
		return nil, fmt.Errorf("productId is required")
	}
	product, err := productBody(params, dataplane.Item{JSON: map[string]any{}}, true)
	if err != nil {
		return nil, err
	}
	product["id"] = id
	result, err := n.doJSON(ctx, cred, http.MethodPut, fmt.Sprintf("/products/%d.json", id), map[string]any{"product": product})
	if err != nil {
		return nil, err
	}
	if object, ok := result["product"].(map[string]any); ok {
		return object, nil
	}
	return result, nil
}

func productBody(params map[string]any, item dataplane.Item, partial bool) (map[string]any, error) {
	if raw, err := mapParam(params, "product"); err != nil {
		return nil, err
	} else if raw != nil {
		return raw, nil
	}
	product := map[string]any{}
	for param, key := range map[string]string{
		"title":           "title",
		"descriptionHtml": "body_html",
		"vendor":          "vendor",
		"productType":     "product_type",
		"tags":            "tags",
		"status":          "status",
		"publishedAt":     "published_at",
	} {
		setString(product, key, stringParam(params, param))
	}
	if product["title"] == nil && item.JSON["title"] != nil {
		product["title"] = item.JSON["title"]
	}
	if !partial && product["title"] == nil {
		return nil, fmt.Errorf("title is required")
	}
	if variants, err := arrayParam(params, "variants"); err != nil {
		return nil, err
	} else if len(variants) > 0 {
		product["variants"] = variants
	}
	if options, err := arrayParam(params, "options"); err != nil {
		return nil, err
	} else if len(options) > 0 {
		product["options"] = options
	}
	if images, err := arrayParam(params, "images"); err != nil {
		return nil, err
	} else if len(images) > 0 {
		product["images"] = images
	}
	return product, nil
}

func (n *Node) handleVariant(ctx context.Context, cred Credential, operation string, params map[string]any) ([]dataplane.Item, error) {
	switch operation {
	case OpGet:
		id := int64Param(params, "variantId")
		if id == 0 {
			return nil, fmt.Errorf("variantId is required")
		}
		result, err := n.doJSON(ctx, cred, http.MethodGet, fmt.Sprintf("/variants/%d.json", id), nil)
		return singleValue(result["variant"], err)
	case OpCreate:
		productID := int64Param(params, "productId")
		if productID == 0 {
			return nil, fmt.Errorf("productId is required")
		}
		variant, err := mapParam(params, "variant")
		if err != nil {
			return nil, err
		}
		if variant == nil {
			variant = map[string]any{"price": stringParam(params, "price"), "sku": stringParam(params, "sku")}
			setString(variant, "option1", stringParam(params, "option1"))
		}
		result, err := n.doJSON(ctx, cred, http.MethodPost, fmt.Sprintf("/products/%d/variants.json", productID), map[string]any{"variant": variant})
		return singleValue(result["variant"], err)
	case OpUpdate:
		id := int64Param(params, "variantId")
		if id == 0 {
			return nil, fmt.Errorf("variantId is required")
		}
		variant, err := mapParam(params, "variant")
		if err != nil {
			return nil, err
		}
		if variant == nil {
			variant = map[string]any{"id": id}
			setString(variant, "price", stringParam(params, "price"))
			setString(variant, "sku", stringParam(params, "sku"))
		}
		result, err := n.doJSON(ctx, cred, http.MethodPut, fmt.Sprintf("/variants/%d.json", id), map[string]any{"variant": variant})
		return singleValue(result["variant"], err)
	case OpDelete:
		productID := int64Param(params, "productId")
		variantID := int64Param(params, "variantId")
		if productID == 0 || variantID == 0 {
			return nil, fmt.Errorf("productId and variantId are required")
		}
		return singleValue(n.doJSON(ctx, cred, http.MethodDelete, fmt.Sprintf("/products/%d/variants/%d.json", productID, variantID), nil))
	default:
		return nil, fmt.Errorf("unknown variant operation %s", operation)
	}
}
