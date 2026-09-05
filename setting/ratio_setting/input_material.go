package ratio_setting

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
)

// defaultInputMaterialPrices 故意留空:素材计费必须由管理员显式配置后生效,
// 非空默认会把存量模型的账单静默改变(与 defaultVideoSecondPrice 同理)。
var defaultInputMaterialPrices = map[string]types.InputMaterialPriceList{}
var inputMaterialPricesMap = types.NewRWMap[string, types.InputMaterialPriceList]()

// 素材无真值时的系统默认估算秒数(可经 option 配置;使用侧再钳制到
// types.MaxTaskDurationSeconds —— 本包不 import relay 层)。
var (
	MaterialDefaultVideoSeconds = 20
	MaterialDefaultAudioSeconds = 60
)

func InputMaterialPrices2JSONString() string {
	return inputMaterialPricesMap.MarshalJSONString()
}

// UpdateInputMaterialPricesByJSONString 解析并校验 {模型: 素材价表} JSON。
// 每张表经 types.NormalizeInputMaterialPriceList 校验归一化;空表条目视为
// "未配置"直接丢弃;任一表非法则整体拒绝(不部分生效)。
func UpdateInputMaterialPricesByJSONString(jsonStr string) error {
	var raw map[string]types.InputMaterialPriceList
	if err := common.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return err
	}
	normalized := make(map[string]types.InputMaterialPriceList, len(raw))
	for name, list := range raw {
		norm, err := types.NormalizeInputMaterialPriceList(list, types.MaxTaskDurationSeconds)
		if err != nil {
			return fmt.Errorf("模型 %s: %w", name, err)
		}
		if len(norm) == 0 {
			continue
		}
		normalized[name] = norm
	}
	data, err := common.Marshal(normalized)
	if err != nil {
		return err
	}
	return types.LoadFromJsonStringWithCallback(inputMaterialPricesMap, string(data), InvalidateExposedDataCache)
}

// GetInputMaterialPrices 返回模型的全局素材价表。第二个返回值为 false 表示
// 未配置,调用方不做素材计费。
func GetInputMaterialPrices(name string) (types.InputMaterialPriceList, bool) {
	name = FormatMatchingModelName(name)
	list, ok := inputMaterialPricesMap.Get(name)
	if !ok || len(list) == 0 {
		return nil, false
	}
	return list, true
}

func GetInputMaterialPricesCopy() map[string]types.InputMaterialPriceList {
	return inputMaterialPricesMap.ReadAll()
}

// GetMaterialDefaultSeconds 返回素材无真值时的系统默认估算秒数。返回原始
// 配置值,不在此钳制 —— 消费侧用作计费乘数前必须钳到
// types.MaxTaskDurationSeconds(本包不 import relay 层,钳制语义归消费侧)。
// 图片与未知类型返回 0(图片不按时长计费)。
func GetMaterialDefaultSeconds(materialType string) int {
	switch materialType {
	case types.MaterialTypeVideo:
		return MaterialDefaultVideoSeconds
	case types.MaterialTypeAudio:
		return MaterialDefaultAudioSeconds
	default:
		return 0
	}
}
