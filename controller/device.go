package controller

import (
	"errors"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// DeviceInfo 是设备列表/上限响应里对外的设备形状。
//
// AccessedTime 直接复用 Token.AccessedTime(每次扣费都更新,见 model/token.go),
// 不在 DeviceBinding 上重复存一份 —— 那样会有两个真值源。0 表示「绑定后从未
// 调用过」,前端应显示「未使用」而不是把 0 当成 1970 年渲染。
type DeviceInfo struct {
	InstallID     string `json:"install_id"`
	ClientVersion string `json:"client_version"`
	AccessedTime  int64  `json:"accessed_time"`
	CreatedTime   int64  `json:"created_time"`
	UpdatedTime   int64  `json:"updated_time"`
}

// buildDeviceInfos 把 DeviceBinding 行 join 对应 Token 的 AccessedTime,
// 组装成对外形状。Token 找不到(比如已被单独删除)时 AccessedTime 按 0 处理,
// 不让一行查询失败拖垮整份列表。
func buildDeviceInfos(devices []model.DeviceBinding) []DeviceInfo {
	infos := make([]DeviceInfo, 0, len(devices))
	for _, d := range devices {
		var accessedTime int64
		if d.TokenId != 0 {
			if token, err := model.GetTokenById(d.TokenId); err == nil {
				accessedTime = token.AccessedTime
			}
		}
		infos = append(infos, DeviceInfo{
			InstallID:     d.InstallID,
			ClientVersion: d.ClientVersion,
			AccessedTime:  accessedTime,
			CreatedTime:   d.CreatedTime,
			UpdatedTime:   d.UpdatedTime,
		})
	}
	return infos
}

// GetUserDevices 列出当前用户绑定的设备。用户会话路由(JWT/PAT),读操作。
func GetUserDevices(c *gin.Context) {
	userId := c.GetInt("id")
	devices, err := model.ListUserDevices(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, buildDeviceInfos(devices))
}

// DeleteUserDevice 用户自助解绑一台设备,连带撤销其令牌。
//
// 必须放在完整用户会话路由(JWT/PAT)而非 TokenAuthReadOnly ——
// 解绑会删除令牌,是高影响操作,要求调用方持有完整的用户身份,
// 而不是仅凭一个 sk- key(否则泄露的 key 本身就能吊销其他设备)。
func DeleteUserDevice(c *gin.Context) {
	userId := c.GetInt("id")
	installID := strings.TrimSpace(c.Param("install_id"))
	if installID == "" {
		common.ApiErrorMsg(c, "install_id 不能为空")
		return
	}
	if err := model.UnbindDevice(userId, installID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "设备不存在"})
			return
		}
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// deviceBindRequest 是画布登录后上报的绑定请求体。
type deviceBindRequest struct {
	InstallID     string `json:"install_id" binding:"required"`
	ClientVersion string `json:"client_version" binding:"required"`
}

// deviceLimitReachedResponse 组装「已达上限」的响应体。
//
// 必须带上已绑设备列表(含 AccessedTime),否则用户看到「已达 3 台上限」却不知道
// 该解绑哪一个 —— 这是决策 4 明确接受的代价(反复重装会积累孤儿槽位)的兜底措施,
// 缓解手段就是让用户能自助解绑,而自助解绑的前提是能看到列表。
func deviceLimitReachedResponse(c *gin.Context, userId int) {
	devices, err := model.ListUserDevices(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": false,
		"code":    "DEVICE_LIMIT_REACHED",
		"message": "已达到设备数量上限，请解绑不再使用的设备后重试。重新安装会占用新的设备槽位，不会自动释放旧槽位。",
		"data": gin.H{
			"limit":   model.MaxDevicesPerUser,
			"devices": buildDeviceInfos(devices),
		},
	})
}

// BindCanvasDevice 供画布登录后调用,把当前安装绑定到调用方持有的 sk- key。
//
// 放在既有 canvasRoute(TokenAuthReadOnly)下 —— 画布此时手上只有 sk- key,
// 还没有完整的用户会话凭据。TokenAuthReadOnly 已经把 userId/tokenId 写进
// context("id"/"token_id"),这里直接读取即可。
func BindCanvasDevice(c *gin.Context) {
	var req deviceBindRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	req.InstallID = strings.TrimSpace(req.InstallID)
	req.ClientVersion = strings.TrimSpace(req.ClientVersion)
	if req.InstallID == "" {
		common.ApiErrorMsg(c, "install_id 不能为空")
		return
	}
	if req.ClientVersion == "" {
		common.ApiErrorMsg(c, "client_version 不能为空")
		return
	}

	userId := c.GetInt("id")
	tokenId := c.GetInt("token_id")

	binding, err := model.BindDevice(userId, req.InstallID, req.ClientVersion, tokenId)
	if err != nil {
		if errors.Is(err, model.ErrDeviceLimitReached) {
			deviceLimitReachedResponse(c, userId)
			return
		}
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, DeviceInfo{
		InstallID:     binding.InstallID,
		ClientVersion: binding.ClientVersion,
		CreatedTime:   binding.CreatedTime,
		UpdatedTime:   binding.UpdatedTime,
	})
}
