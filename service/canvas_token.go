package service

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
)

const (
	CanvasTokenPrefix = "画布token-"
	CanvasTokenCount  = 10
)

// EnsureCanvasTokens 确保用户有10个画布专用token。
// 检查现有"画布token-*"数量，不足10个则补齐创建。
// 幂等：已有10个时直接返回成功。
// 命名冲突时跳过该编号继续创建下一个。
func EnsureCanvasTokens(userId int) error {
	// 1. 查询现有画布token
	var existing []model.Token
	err := model.DB.Where("user_id = ? AND name LIKE ?", userId, CanvasTokenPrefix+"%").
		Find(&existing).Error
	if err != nil {
		common.SysError(fmt.Sprintf("查询用户 %d 的画布token失败: %v", userId, err))
		return fmt.Errorf("查询画布token失败: %w", err)
	}

	existingCount := len(existing)
	if existingCount >= CanvasTokenCount {
		common.SysLog(fmt.Sprintf("用户 %d 已有 %d 个画布token，无需创建", userId, existingCount))
		return nil
	}

	// 2. 构建已存在的编号集合
	existingNumbers := make(map[int]bool)
	for _, token := range existing {
		var num int
		if n, err := fmt.Sscanf(token.Name, CanvasTokenPrefix+"%d", &num); err == nil && n == 1 {
			existingNumbers[num] = true
		}
	}

	// 3. 补齐创建缺失的token
	now := common.GetTimestamp()
	created := 0
	for i := 1; i <= CanvasTokenCount; i++ {
		if existingNumbers[i] {
			continue
		}

		key, err := common.GenerateKey()
		if err != nil {
			common.SysError(fmt.Sprintf("为用户 %d 生成画布token-%d的key失败: %v", userId, i, err))
			continue // 单个失败不阻断整体
		}

		token := model.Token{
			UserId:             userId,
			Name:               fmt.Sprintf("%s%d", CanvasTokenPrefix, i),
			Key:                key,
			CreatedTime:        now,
			AccessedTime:       now,
			ExpiredTime:        -1,
			UnlimitedQuota:     true,
			ModelLimitsEnabled: false,
			Status:             1,
			Group:              "",
		}

		// 跟随用户的auto分组设置
		if setting.DefaultUseAutoGroup {
			token.Group = "auto"
		}

		if err := token.Insert(); err != nil {
			// 名称冲突（并发创建）或其他DB错误，记录后继续
			common.SysError(fmt.Sprintf("为用户 %d 插入画布token-%d失败: %v", userId, i, err))
			continue
		}
		created++
	}

	common.SysLog(fmt.Sprintf("为用户 %d 补齐创建了 %d 个画布token（已有 %d 个）",
		userId, created, existingCount))
	return nil
}
