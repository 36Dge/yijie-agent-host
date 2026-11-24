package session

import (
	"encoding/json"
	"regexp"
	"strconv"
)

// Generic marketplace tool names are the first 30 bytes of SHA-256(service + LF + method).
// Legacy direct calls use the original method name under the sorftime server.
var sorftimeTitles = func() map[string]string {
	methods := []string{
		"product_trend", "product_detail", "product_search", "product_search_from_name",
		"category_search_from_product_name", "category_name_search", "category_report",
		"category_products", "category_products_extended", "common_usage_query", "get_time",
	}
	names := make(map[string]string, 2*len(methods))
	for _, method := range methods {
		names["yijie_market/"+marketplaceToolName("sorftime", method)] = method
		names["sorftime/"+method] = method
	}
	return names
}()

var sorftimeNodeID = regexp.MustCompile(`^[0-9]{1,20}$`)

func sorftimeCallTitle(wire nativeWireItem) string {
	method := sorftimeTitles[wire.Server+"/"+wire.Tool]
	if method == "" {
		return ""
	}
	var args struct {
		ASIN         string `json:"asin"`
		Trend        string `json:"type"`
		Node         string `json:"node_id"`
		ProductName  string `json:"product_name"`
		CategoryName string `json:"category_name"`
		Name         string `json:"name"`
		SearchName   string `json:"search_name"`
		Keyword      string `json:"keyword"`
		Page         int    `json:"page"`
	}
	if len(wire.Arguments) != 0 && json.Unmarshal(wire.Arguments, &args) != nil {
		return ""
	}
	asin, node := args.ASIN, args.Node
	if !nativeMcpASIN.MatchString(asin) {
		asin = ""
	}
	if !sorftimeNodeID.MatchString(node) {
		node = ""
	}
	var action, target string
	var paginated bool
	switch method {
	case "product_trend":
		action, target = "查询商品趋势", asin
		switch args.Trend {
		case "DailySalesVolume":
			action = "查询商品日销量"
		case "SalesVolume":
			action = "查询商品月销量"
		}
	case "product_detail":
		action, target = "查看 Amazon 商品详情", asin
	case "category_search_from_product_name":
		action, target, paginated = "查询 Amazon 类目", publicTarget(args.ProductName), true
	case "category_name_search":
		action, target = "按名称查询类目", publicTarget(args.CategoryName)
	case "category_report":
		action, target = "查询类目报告", node
	case "category_products", "category_products_extended":
		action, target, paginated = "查询类目商品", node, method == "category_products_extended"
	case "product_search", "product_search_from_name":
		action, target, paginated = "搜索商品", asin, true
		for _, value := range []string{args.Name, args.SearchName, args.Keyword, node} {
			if target == "" {
				target = publicTarget(value)
			}
		}
	case "common_usage_query":
		return "查询 Sorftime 剩余额度"
	case "get_time":
		return "查询 Sorftime 服务时间"
	}
	if target != "" {
		action += " · " + target
	}
	if paginated && args.Page > 0 {
		action += " · 第 " + strconv.Itoa(args.Page) + " 页"
	}
	return action
}
