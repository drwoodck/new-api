package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

type contractStatsResponse struct {
	// 在线装机总数 —— 即便没有任何契约被支持,后台也要能显示分母
	OnlineInstalls int64                `json:"online_installs"`
	WindowDays     int                  `json:"window_days"`
	Contracts      []model.ContractStat `json:"contracts"`
}

// GetCanvasContractStats 返回每个契约在在线装机里的支持率,
// 供后台在给模型指派契约时显示「有多少客户端支持它」。
//
// 这个数字是本期存在的理由:在不做自动更新的前提下,
// 运营方上架新契约的模型时否则无法判断影响面。
func GetCanvasContractStats(c *gin.Context) {
	windowDays := 0 // 0 让模型层用默认值
	if v := c.Query("window_days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			windowDays = n
		}
	}

	stats, err := model.GetContractSupportStats(windowDays)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	online, err := model.CountOnlineInstalls(windowDays)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// map → 切片,便于前端稳定渲染
	list := make([]model.ContractStat, 0, len(stats))
	for _, s := range stats {
		list = append(list, s)
	}

	common.ApiSuccess(c, contractStatsResponse{
		OnlineInstalls: online,
		// 回报**实际生效**的窗口,不是传进来的原始值 —— 参数缺省时 windowDays
		// 是 0,而统计用的是默认 30 天,回 0 等于报一个假值给前端。
		WindowDays: model.EffectiveReportWindowDays(windowDays),
		Contracts:  list,
	})
}
