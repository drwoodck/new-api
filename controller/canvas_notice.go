package controller

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// canvasNoticeWire 是画布客户端看到的通知形状。
//
// 刻意不从 model.CanvasNotice 直接 marshal 出去:存储行里的 target_groups
// 是**收件人**信息,没有任何理由下发给收件人自己 —— 客户端只需要知道
// 「这条是不是发给我的」(能拿到就说明是)。少下发一个字段,就少一处
// 将来被误用成「客户端自己按分组过滤」的入口。
//
// 契约是跨仓库的(canvas 客户端用 serde 反序列化),字段名改动等同于破坏性
// 变更,加字段前先确认客户端会忽略未知字段。
type canvasNoticeWire struct {
	Id        int       `json:"id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
	// Read 是**该用户**的已读状态,不是通知自身的属性。
	Read bool `json:"read"`
}

// GetCanvasNotices 供画布客户端拉取发给自己的通知。
//
// 挂在 canvasRoute(TokenAuthReadOnly)下,与 device-bind / client-contracts
// 同组 —— 画布此时手上只有 canvas key,没有完整用户会话;该中间件已经算好
// 并写进了「id」(用户 id)与 ContextKeyUsingGroup(有效分组 = token.Group
// 覆盖 user.Group),这里直接读,不重查库。
//
// 信封用 common.ApiSuccess 而不是像 GetCanvasCatalog 那样直接 c.Data:
// 同组另外两个端点(device-bind / client-contracts)都走 ApiSuccess,
// catalog 是例外(它要自己算 ETag、写 Cache-Control,必须拿到原始 body)。
// 这里没有协商缓存的需求,跟多数派走。
func GetCanvasNotices(c *gin.Context) {
	effectiveGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	userId := c.GetInt("id")

	// 空 group 会让 ListCanvasNoticesForGroup 一条都匹配不到(刻意如此,
	// 见 model.CanvasNotice.TargetsGroup)—— 取不到分组时宁可不下发,
	// 也不下发别人的通知。
	notices, err := model.ListCanvasNoticesForGroup(effectiveGroup)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	noticeIds := make([]int, 0, len(notices))
	for i := range notices {
		noticeIds = append(noticeIds, notices[i].Id)
	}

	// 已读集合查询失败按 fail-open 处理:一次瞬时 DB 故障最多让客户端把
	// 已读的通知再显示一遍,不该让整个通知列表拉不出来 —— 与目录端点
	// 「查询失败不降级整份响应」同一条原则。
	readMap, err := model.GetCanvasNoticeReadMap(userId, noticeIds)
	if err != nil {
		common.SysError("读取画布通知已读状态失败,本次全部按未读下发: " + err.Error())
		readMap = map[int]struct{}{}
	}

	items := make([]canvasNoticeWire, 0, len(notices))
	for i := range notices {
		_, read := readMap[notices[i].Id]
		items = append(items, canvasNoticeWire{
			Id:        notices[i].Id,
			Title:     notices[i].Title,
			Content:   notices[i].Content,
			Type:      notices[i].Type,
			CreatedAt: notices[i].CreatedAt,
			Read:      read,
		})
	}

	common.ApiSuccess(c, items)
}

// MarkCanvasNoticeRead 把一条通知标记为该用户已读(幂等)。
//
// 必须先做归属校验:不校验的话,任何登录用户都能拿 id 递增去标记**别人**
// 收到的通知 —— 已读表是全局共用的,写进去的是 (notice_id, 自己 user_id),
// 虽然污染不到别人,但管理员看到的「这条通知的触达情况」会变成噪声。
// 校验口径必须与列表完全一致(TargetsGroup),两处不能各写一套。
func MarkCanvasNoticeRead(c *gin.Context) {
	effectiveGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	userId := c.GetInt("id")

	noticeId, err := strconv.Atoi(c.Param("id"))
	if err != nil || noticeId <= 0 {
		common.ApiErrorMsg(c, "通知 id 不合法")
		return
	}

	notice, err := model.GetCanvasNoticeById(noticeId)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "通知不存在"})
			return
		}
		common.ApiError(c, err)
		return
	}
	// 归属校验必须与列表口径完全一致(ListCanvasNoticesForGroup),两处不能
	// 各写一套 —— 否则会留下「列表里已经看不到这条、却还能给它写已读」这种
	// 可被外部触发的缝。
	//
	// IsDeleted 那一半在新代码里恒为假(删除已是物理删行),保留它是为了覆盖
	// 窗口期:旧实例软删的行,新实例的列表看不到,这里也必须一并拒绝。
	if notice.IsDeleted() || !notice.TargetsGroup(effectiveGroup) {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "通知不存在"})
		return
	}

	if err := model.MarkCanvasNoticeRead(noticeId, userId); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
