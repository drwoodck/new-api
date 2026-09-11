package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// 对账关心的日志类型:消费(2)、错误(5)、退费(6)。
//
// 其余类型(充值/管理/登录)与「这次调用的钱」无关,画布不需要。
// 用 map 而非切片:下面按行判断,是 O(1) 查表。
var canvasUsageLogTypes = map[int]bool{
	model.LogTypeConsume: true,
	model.LogTypeError:   true,
	model.LogTypeRefund:  true,
}

// GetCanvasUsage 给画布拉自己的调用记录,用于消费明细与**计费对账**。
//
// 为什么不是 /api/log/token:那个端点按 **token_id** 过滤,而画布每个用户有
// 10 个画布令牌(service/canvas_token.go 的 CanvasTokenCount),客户端得把 10 把
// key 各查一遍再合并 —— 既慢又容易漏。这里按 **user_id** 过滤,一次到位。
//
// 计费口径与后台完全一致:直接复用 model.GetUserLogs 的同一 SQL 语义,返回的
// `quota` 就是 logs 表里那一列,不重算(差额修正、分组定价、OtherRatios 乘法
// 都在服务端,客户端重算必然失真 —— 画布那边也是照这个前提设计的)。
//
// 最常用的用法是带 `request_id` 精确查一条:画布在发起调用时从响应头
// X-Oneapi-Request-Id 记下了它,事后拿它来问「这次到底扣了多少」。
func GetCanvasUsage(c *gin.Context) {
	// 中间件(TokenAuthReadOnly)已经算好了 userId —— 不重查库,也**不接受**
	// 调用方传 userId:那会让任何一把 key 都能查别人的账
	userId := c.GetInt("id")
	if userId <= 0 {
		common.ApiError(c, nil)
		return
	}

	pageInfo := common.GetPageQuery(c)

	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)

	// request_id 是对账主键:给了它就精确查那一条(可能两条 —— 扣费与退费
	// 在中转站是两条独立记录,靠同一个 request_id 关联)
	requestId := c.Query("request_id")

	// LogTypeUnknown 表示「不按类型过滤」,拉回来之后在下面筛。
	// 为什么不分成三次按类型拉:分页会在每次调用里各算一遍,三次拉取的行数与
	// 顺序都对不齐,客户端拼起来会缺条目。对账场景量本来就小
	// (按 request_id 查时通常就 1-3 条)。
	logs, total, err := model.GetUserLogs(
		userId,
		model.LogTypeUnknown,
		startTimestamp,
		endTimestamp,
		c.Query("model_name"),
		"",
		pageInfo.GetStartIdx(),
		pageInfo.GetPageSize(),
		"",
		requestId,
		"",
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 只留画布关心的三类;其余(充值、登录等)不占位次
	filtered := make([]*model.Log, 0, len(logs))
	for _, l := range logs {
		if canvasUsageLogTypes[l.Type] {
			filtered = append(filtered, l)
		}
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(filtered)
	common.ApiSuccess(c, pageInfo)
}
