package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	hosttypes "github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// tierDiagnosticReport 是单个 (模型, 分组, 渠道类型) 组合的体检结论。
type tierDiagnosticReport struct {
	ModelName      string   `json:"model_name"`
	GroupName      string   `json:"group_name"`
	ChannelType    int      `json:"channel_type"`
	TierType       string   `json:"tier_type"`
	BillingUnit    string   `json:"billing_unit"`
	Keys           []string `json:"keys"`
	SimulatedInput map[string]any `json:"simulated_input"`
	ResolvedStatus string   `json:"resolved_status"`
	ResolvedKey    string   `json:"resolved_key"`
	Verdict        string   `json:"verdict"`
	Problem        bool     `json:"problem"`
}

// GetTierDiagnostics 是只读的档位表体检接口(AdminAuth)。
//
// 为什么需要它:档位表的键必须与「计费时实际算出的档位输入」逐字相等才能命中,
// 而后者取决于渠道类型与适配器的取值逻辑 —— 管理员在界面上无从判断,配错了也
// 没有任何报错,档表静默失效、按回退价计费。本接口把真实取值逻辑跑一遍,
// 直接回答「这一档能不能被命中」。
//
// 判据复用 relaycommon.BuildTierInputFromRequest 与 model.ResolveTierPriceFromRow,
// 与计费完全同源 —— 不另写一套判断,避免「诊断说能命中、计费说不能」。
//
// 查询参数: model_name(可选,不给则体检全部有档表的行)。
func GetTierDiagnostics(c *gin.Context) {
	rows, err := loadTierRows(c.Query("model_name"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}

	reports := make([]tierDiagnosticReport, 0, len(rows))
	for i := range rows {
		reports = append(reports, diagnoseTierRow(&rows[i])...)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"reports": reports,
			"checked": len(reports),
			"problem": countProblems(reports),
		},
	})
}

// loadTierRows 取出待体检的行。只保留真正配了档表的行 —— 没档表就无所谓命不命中。
func loadTierRows(modelName string) ([]model.ModelGroupPrice, error) {
	if modelName != "" {
		rows, err := model.GetModelGroupPrices(modelName)
		if err != nil {
			return nil, err
		}
		return filterRowsWithTiers(rows), nil
	}

	all, err := model.GetAllModelGroupPrices()
	if err != nil {
		return nil, err
	}
	rows := make([]model.ModelGroupPrice, 0, len(all))
	for _, byGroup := range all {
		for _, row := range byGroup {
			rows = append(rows, row)
		}
	}
	return filterRowsWithTiers(rows), nil
}

func filterRowsWithTiers(rows []model.ModelGroupPrice) []model.ModelGroupPrice {
	out := make([]model.ModelGroupPrice, 0, len(rows))
	for _, row := range rows {
		if row.PriceTiers != nil && len(*row.PriceTiers) > 0 {
			out = append(out, row)
		}
	}
	return out
}

// diagnoseTierRow 查出该模型的渠道后交给纯函数体检。
func diagnoseTierRow(row *model.ModelGroupPrice) []tierDiagnosticReport {
	channelTypes, err := channelTypesForModel(row.ModelName)
	if err != nil {
		channelTypes = nil
	}
	return diagnoseTierRowForChannels(row, channelTypes)
}

// diagnoseTierRowForChannels 是纯函数部分:不做 DB 查询,便于表驱动测试。
func diagnoseTierRowForChannels(row *model.ModelGroupPrice, channelTypes []int) []tierDiagnosticReport {
	tiers := *row.PriceTiers

	// 取不到渠道不代表没问题(可能只是还没绑渠道),但要如实说明,不能假装体检过了。
	if len(channelTypes) == 0 {
		return []tierDiagnosticReport{{
			ModelName:      row.ModelName,
			GroupName:      row.GroupName,
			TierType:       tiers[0].TierType,
			BillingUnit:    tiers[0].BillingUnit,
			Keys:           tierKeys(tiers),
			ResolvedStatus: "unknown",
			Verdict:        "该模型没有可用渠道,无法模拟实际取值",
		}}
	}

	reports := make([]tierDiagnosticReport, 0, len(channelTypes))
	for _, channelType := range channelTypes {
		// 空请求:画布发来的字段名(resolution/aspect_ratio)与 TaskSubmitReq 的
		// 字段名(size/metadata)对不上,会被 encoding/json 直接丢弃 —— 空请求
		// 恰恰就是线上实际发生的情况。真实请求若带 metadata,取值可能不同,
		// 所以下面把模拟值原样报出来供人对照。
		input := relaycommon.BuildTierInputFromRequest(
			channelType,
			relaycommon.TaskSubmitReq{},
			relaycommon.DefaultVideoDurationSeconds(channelType),
		)
		resolved := model.ResolveTierPriceFromRow(row.ModelName, row.GroupName, input, row)

		report := tierDiagnosticReport{
			ModelName:   row.ModelName,
			GroupName:   row.GroupName,
			ChannelType: channelType,
			TierType:    tiers[0].TierType,
			BillingUnit: tiers[0].BillingUnit,
			Keys:        tierKeys(tiers),
			SimulatedInput: map[string]any{
				"resolution":       input.Resolution,
				"image_size":       input.ImageSize,
				"duration_seconds": input.DurationSeconds,
				"mode":             input.Mode,
			},
			ResolvedStatus: statusLabel(resolved.Status),
			ResolvedKey:    resolved.TierKey,
		}
		report.Verdict, report.Problem = verdictFor(channelType, tiers[0].TierType, input, resolved.Status)
		reports = append(reports, report)
	}
	return reports
}

// verdictFor 把匹配结果翻译成一句人话。problem 为 true 表示这张档表在该渠道下
// 不可命中 —— 即配置无效,计费会走回退路径。
func verdictFor(channelType int, tierType string, input hosttypes.TierInput, status model.TierResolution) (string, bool) {
	if status == model.TierResolutionResolved {
		return "可命中:该渠道会算出 " + dimensionLabel(tierType) + "=" + dimensionValue(tierType, input), false
	}
	got := dimensionValue(tierType, input)
	if got == "" {
		return "不可命中:该渠道不产生" + dimensionLabel(tierType) +
			"值(渠道类型 " + strconv.Itoa(channelType) + " 不从请求里取这个维度)", true
	}
	return "不可命中:该渠道算出的是" + dimensionLabel(tierType) + "=" + got +
		",而档表里没有这个键", true
}

// statusLabel 把整型枚举转成可读字符串 —— 直接输出数字对排查没有帮助。
func statusLabel(s model.TierResolution) string {
	switch s {
	case model.TierResolutionResolved:
		return "resolved"
	case model.TierResolutionUnavailable:
		return "unavailable"
	case model.TierResolutionNoTable:
		return "no_table"
	}
	return "unknown"
}

func dimensionLabel(tierType string) string {
	switch tierType {
	case hosttypes.TierTypeResolution:
		return "分辨率"
	case hosttypes.TierTypeImageSize:
		return "图像尺寸"
	case hosttypes.TierTypeMode:
		return "模式"
	case hosttypes.TierTypeRequest:
		return "时长"
	}
	return tierType
}

func dimensionValue(tierType string, input hosttypes.TierInput) string {
	switch tierType {
	case hosttypes.TierTypeResolution:
		return input.Resolution
	case hosttypes.TierTypeImageSize:
		return input.ImageSize
	case hosttypes.TierTypeMode:
		return input.Mode
	case hosttypes.TierTypeRequest:
		if input.DurationSeconds <= 0 {
			return ""
		}
		return strconv.Itoa(input.DurationSeconds) + "s"
	}
	return ""
}

func tierKeys(tiers hosttypes.PriceTierList) []string {
	keys := make([]string, 0, len(tiers))
	for _, t := range tiers {
		keys = append(keys, t.Key)
	}
	return keys
}

// channelTypesForModel 取该模型实际可路由到的渠道类型。
//
// 列名必须带 abilities. 前缀:channels 也有同名维度列,JOIN 之后未限定会成
// 歧义列名(与 GetGroupEnabledModels 踩过的是同一个坑)。
func channelTypesForModel(modelName string) ([]int, error) {
	var types []int
	err := model.DB.Table("abilities").
		Joins("INNER JOIN channels ON abilities.channel_id = channels.id").
		Where("abilities.model = ? AND abilities.enabled = ? AND channels.status = ?", modelName, true, 1).
		Distinct("channels.type").
		Pluck("channels.type", &types).Error
	return types, err
}

func countProblems(reports []tierDiagnosticReport) int {
	n := 0
	for _, r := range reports {
		if r.Problem {
			n++
		}
	}
	return n
}
