package shopify

const NodeType = "n8n-nodes-base.shopify"

const (
	ResourceOrder     = "order"
	ResourceProduct   = "product"
	ResourceCustomer  = "customer"
	ResourceInventory = "inventory"
	ResourceWebhook   = "webhook"
	ResourceVariant   = "variant"
)

const (
	OpGetAll = "getAll"
	OpGet    = "get"
	OpCreate = "create"
	OpUpdate = "update"
	OpDelete = "delete"
	OpCancel = "cancel"
	OpRefund = "refund"
	OpSearch = "search"
	OpAdjust = "adjust"
	OpSet    = "set"
)
