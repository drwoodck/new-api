package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// MaxDevicesPerUser 是每个用户可同时绑定的设备(安装)数上限。
//
// 设备身份 = install_id(随机 UUID v4,存本地 SQLite),不是硬件指纹 ——
// 见计划决策 4:硬件指纹在依赖树里不存在,且防的是账号共享,与是否同一台机器
// 无关,3 个并发安装就是 3 个槽位。反复重装会积累孤儿槽位,缓解手段是
// 自助解绑(见 UnbindDevice)与设备列表上的 AccessedTime,不是硬件指纹。
const MaxDevicesPerUser = 3

// ErrDeviceLimitReached 表示该用户已绑满 MaxDevicesPerUser 台设备。
var ErrDeviceLimitReached = errors.New("已达到设备数量上限")

// DeviceBinding 记录一个用户安装(install_id)与其执行令牌(sk- key)的绑定关系。
//
// Token 才是设备的真正执行载体:token.Delete() 即时生效(软删 + 失效缓存),
// 之后 GetTokenByKey 拿 ErrRecordNotFound → TokenAuth 401。这张表只是
// install_id → token_id 的映射 + 上限计数,不复制 Token 的执行语义。
//
// 不存主机名/用户名等 PII(决策 4)。「最后使用时间」直接复用 Token.AccessedTime,
// 不在这张表上重复一份 —— 那样会有两个真值源且容易漂移。
type DeviceBinding struct {
	Id int `json:"id" gorm:"primaryKey"`

	// 复合唯一索引 (user_id, install_id),照 ClientContractReport 的形状。
	// 不能只在 install_id 上加唯一索引 —— 那会让两个用户装同一份拷贝时
	// 互相顶掉对方的绑定行。
	UserId int `json:"user_id" gorm:"index;uniqueIndex:idx_device_user_install,priority:1;not null"`

	InstallID string `json:"install_id" gorm:"type:varchar(64);uniqueIndex:idx_device_user_install,priority:2;not null"`

	ClientVersion string `json:"client_version" gorm:"type:varchar(32)"`

	// 该安装当前持有的执行令牌 ID。解绑时要连带删除对应 Token,那才是
	// 真正的吊销。
	TokenId int `json:"token_id" gorm:"index;not null"`

	CreatedTime int64 `json:"created_time" gorm:"bigint"`
	UpdatedTime int64 `json:"updated_time" gorm:"bigint;index"`

	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`
}

// BindDevice 绑定(或幂等续期)一个安装。
//
// 画布每次登录都会调用这个函数,所以必须幂等:同一 (user_id, install_id)
// 重复调用只更新已有行(client_version/token_id/updated_time),不占用新槽位。
// 否则用户连续登录三次就会把自己锁在上限之外。
//
// 计数与插入必须在同一事务里:先查后插不加事务保护的话,并发登录(比如画布
// 在两个窗口同时打开)会在计数检查通过之后各自插入,一起突破上限。
func BindDevice(userId int, installID, clientVersion string, tokenId int) (*DeviceBinding, error) {
	var result *DeviceBinding
	err := DB.Transaction(func(tx *gorm.DB) error {
		now := common.GetTimestamp()

		var existing DeviceBinding
		err := tx.Where("user_id = ? AND install_id = ?", userId, installID).First(&existing).Error
		if err == nil {
			// 命中:幂等续期，不计入新槽位。
			existing.ClientVersion = clientVersion
			existing.TokenId = tokenId
			existing.UpdatedTime = now
			if updErr := tx.Model(&DeviceBinding{}).Where("id = ?", existing.Id).
				Select("client_version", "token_id", "updated_time").
				Updates(&existing).Error; updErr != nil {
				return updErr
			}
			result = &existing
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		// 未命中:同一事务内计数并判上限，避免并发登录突破上限。
		var count int64
		if countErr := tx.Model(&DeviceBinding{}).Where("user_id = ?", userId).Count(&count).Error; countErr != nil {
			return countErr
		}
		if count >= MaxDevicesPerUser {
			return ErrDeviceLimitReached
		}

		binding := &DeviceBinding{
			UserId:        userId,
			InstallID:     installID,
			ClientVersion: clientVersion,
			TokenId:       tokenId,
			CreatedTime:   now,
			UpdatedTime:   now,
		}
		if insErr := tx.Create(binding).Error; insErr != nil {
			return insErr
		}
		result = binding
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ListUserDevices 列出某用户当前绑定的所有设备。
func ListUserDevices(userId int) ([]DeviceBinding, error) {
	var devices []DeviceBinding
	err := DB.Where("user_id = ?", userId).Order("updated_time DESC").Find(&devices).Error
	return devices, err
}

// UnbindDevice 解绑用户自己的一台设备,并连带删除其执行令牌。
//
// 只能解绑自己名下的设备 —— 按 (user_id, install_id) 定位，不允许跨用户解绑。
// Token 删除必须走 token.Delete(),它会在删除前失效缓存(invalidateTokenCacheForMutation),
// 之后 GetTokenByKey 拿 ErrRecordNotFound → TokenAuth 401,这才是真正的即时吊销;
// 绕开它直接对 tokens 表做原始 DELETE 会漏掉缓存失效，旧缓存条目仍会验证通过。
func UnbindDevice(userId int, installID string) error {
	var binding DeviceBinding
	err := DB.Where("user_id = ? AND install_id = ?", userId, installID).First(&binding).Error
	if err != nil {
		return err
	}

	if binding.TokenId != 0 {
		var token Token
		err = DB.Where("id = ? AND user_id = ?", binding.TokenId, userId).First(&token).Error
		if err == nil {
			if delErr := token.Delete(); delErr != nil {
				return delErr
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		// Token 已不存在(比如已被单独删除)不阻塞设备解绑。
	}

	return DB.Delete(&binding).Error
}
