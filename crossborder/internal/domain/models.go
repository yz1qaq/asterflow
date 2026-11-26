package domain

import "time"

// 订单状态。
const (
	OrderAwaitingShipment = "awaiting_shipment"
	OrderPartiallyShipped = "partially_shipped"
	OrderCancelled        = "cancelled"
)

// OrderItem 是订单中的一项商品明细。
type OrderItem struct {
	SKU      string  `json:"sku"` //"SKU-BLACK-M-01",
	Quantity int     `json:"quantity"`
	Price    float64 `json:"price"`
}

// Order 描述跨境订单。
type Order struct {
	ID       string `json:"id"`       //TTS-20260801-1001，订单号
	ShopID   string `json:"shop_id"`  //SHOP-US-001，所属店铺
	Market   string `json:"market"`   //US，销售市场
	Currency string `json:"currency"` //	USD，金额使用的币种

	Amount float64 `json:"amount"` //订单金额
	Status string  `json:"status"` //awaiting_shipment，等待发货

	FulfillmentWH string    `json:"fulfillment_warehouse"` //WH-CN-SZ，负责履约的仓库
	ShipBy        time.Time `json:"ship_by"`               //最晚发货时间

	Items []OrderItem `json:"items"`

	CancellationOpen bool `json:"cancellation_open"` //	当前是否允许取消
}

// InventoryBalance 表示某个仓库中某种商品的库存。
type InventoryBalance struct {
	WarehouseID string `json:"warehouse_id"`
	SKU         string `json:"sku"`
	Available   int    `json:"available"` // 可用数量
	Reserved    int    `json:"reserved"`  // 已预留数量
}
