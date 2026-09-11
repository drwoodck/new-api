package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// canvasNoticesResponse 对应画布端的实际响应信封(common.ApiSuccess):
// data 直接就是通知数组,与同组 device-bind / client-contracts 一致。
type canvasNoticesResponse struct {
	Success bool               `json:"success"`
	Message string             `json:"message"`
	Data    []canvasNoticeWire `json:"data"`
}

// setupCanvasNoticeTestDB 与 setupCatalogGroupFilterTestDB 同一建库方式:
// 路由前插一个中间件,把 constant.ContextKeyUsingGroup 与 "id" 写进 context,
// 模拟 TokenAuthReadOnly 已经算好的有效分组与用户 id,不重走完整鉴权链路
// (那需要真实的 token / user 记录)。
//
// 同一个测试函数里多次调用该 helper(换分组 / 换用户)会连到同一个内存库 ——
// 库名取自 t.Name(),共享数据正是这样多个「视角」测试想要的效果。
func setupCanvasNoticeTestDB(t *testing.T, effectiveGroup string, userId int) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	model.DB = db
	require.NoError(t, db.AutoMigrate(&model.CanvasNotice{}, &model.CanvasNoticeRead{}))

	router := gin.New()
	router.Use(func(c *gin.Context) {
		if effectiveGroup != "" {
			common.SetContextKey(c, constant.ContextKeyUsingGroup, effectiveGroup)
		}
		c.Set("id", userId)
		c.Next()
	})
	router.GET("/api/canvas/notices", GetCanvasNotices)
	router.POST("/api/canvas/notices/:id/read", MarkCanvasNoticeRead)
	return router
}

// listNotices 走一遍真实的拉取链路,返回解析后的通知数组。
func listNotices(t *testing.T, router *gin.Engine) []canvasNoticeWire {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/canvas/notices", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp canvasNoticesResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.True(t, resp.Success, "拉取通知应当成功: %s", resp.Message)
	return resp.Data
}

// markRead 走一遍真实的已读链路,返回 HTTP 状态码。
func markRead(t *testing.T, router *gin.Engine, noticeId int) int {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/canvas/notices/%d/read", noticeId), nil)
	router.ServeHTTP(w, req)
	return w.Code
}

// TestCanvasNoticesFilteredByGroup 锁住最基本的一条:通知只发给目标分组。
//
// 两个分组各有一个用户,同一条通知对 vip 可见、对 default 不可见 ——
// 分组匹配若失效(比如把 target_groups 当成普通字符串做包含判断),
// 会出现「default 用户也能看到 vip 的内测通知」,而这是最难被发现的一类
// 泄漏:客户端不会报错,只是多显示了内容。
func TestCanvasNoticesFilteredByGroup(t *testing.T) {
	vipRouter := setupCanvasNoticeTestDB(t, "vip", 7)
	defaultRouter := setupCanvasNoticeTestDB(t, "default", 8)

	model.DB.Create(&model.CanvasNotice{
		Title: "vip 内测", Content: "只发给 vip",
		Type: model.CanvasNoticeTypeInfo, TargetGroups: model.CanvasNoticeTargetGroups{"vip"},
		CreatedBy: "admin",
	})
	model.DB.Create(&model.CanvasNotice{
		Title: "全员须知", Content: "发给 default",
		Type: model.CanvasNoticeTypeInfo, TargetGroups: model.CanvasNoticeTargetGroups{"default"},
		CreatedBy: "admin",
	})

	vipNotices := listNotices(t, vipRouter)
	require.Len(t, vipNotices, 1)
	assert.Equal(t, "vip 内测", vipNotices[0].Title)

	defaultNotices := listNotices(t, defaultRouter)
	require.Len(t, defaultNotices, 1)
	assert.Equal(t, "全员须知", defaultNotices[0].Title)
}

// TestCanvasNoticesEmptyTargetGroupsReachesNobody 锁住「空目标分组 = 谁都不发」。
//
// 这是本功能里唯一一个**错误方向必须刻意选**的地方:如果把空数组理解成
// 广播,管理员在后台漏选一次分组,内测通知就会群发给全部用户,而且没有
// 任何提示;反过来判成「谁都看不到」最多是漏发一条,重新发一条即可。
func TestCanvasNoticesEmptyTargetGroupsReachesNobody(t *testing.T) {
	router := setupCanvasNoticeTestDB(t, "default", 7)

	model.DB.Create(&model.CanvasNotice{
		Title: "漏选了分组的通知", Content: "不该发给任何人",
		Type:         model.CanvasNoticeTypeInfo,
		TargetGroups: model.CanvasNoticeTargetGroups{}, // 空 = 不发
		CreatedBy:    "admin",
	})

	assert.Empty(t, listNotices(t, router),
		"目标分组为空时必须是「谁都不发」,绝不能当成广播")

	// 取不到有效分组(context key 缺省)时同理 —— 宁可不下发,也不下发别人的。
	noGroupRouter := setupCanvasNoticeTestDB(t, "", 7)
	assert.Empty(t, listNotices(t, noGroupRouter),
		"取不到分组信息时不下发任何通知")
}

// TestCanvasNoticesExcludeDeleted 撤回的通知对画布端等同于不存在。
//
// 撤回必须让通知从列表里消失,并且**不能再被标记已读** —— 否则
// 「列表里已经没有这条,却还能给它写已读」就变成了一个可被外部触发的状态,
// 已读表会重新长出本该随撤回一起清掉的行。
func TestCanvasNoticesExcludeDeleted(t *testing.T) {
	router := setupCanvasNoticeTestDB(t, "vip", 7)

	notice := &model.CanvasNotice{
		Title: "要被撤回的通知", Content: "...",
		Type: model.CanvasNoticeTypeInfo, TargetGroups: model.CanvasNoticeTargetGroups{"vip"},
		CreatedBy: "admin",
	}
	require.NoError(t, model.DB.Create(notice).Error)
	require.Len(t, listNotices(t, router), 1)

	require.NoError(t, model.DeleteCanvasNotice(notice.Id))

	assert.Empty(t, listNotices(t, router), "已撤回的通知不再下发")
	assert.Equal(t, http.StatusNotFound, markRead(t, router, notice.Id),
		"已撤回的通知不能再被标记已读")
}

// TestMarkCanvasNoticeReadIsIdempotent 标记已读必须幂等:标两次不报错,
// 且 (notice_id, user_id) 只留一行。
//
// 画布是「拉取后自动标已读 + 用户还能手动再点一次」的用法,重复调用是常态;
// 若靠「先查再插」实现,客户端重试或双击就会撞主键报错,而调用方拿到的错误
// 与「真的写失败了」无法区分。
func TestMarkCanvasNoticeReadIsIdempotent(t *testing.T) {
	router := setupCanvasNoticeTestDB(t, "vip", 7)

	notice := &model.CanvasNotice{
		Title: "已读幂等", Content: "...",
		Type: model.CanvasNoticeTypeInfo, TargetGroups: model.CanvasNoticeTargetGroups{"vip"},
		CreatedBy: "admin",
	}
	require.NoError(t, model.DB.Create(notice).Error)

	require.Equal(t, http.StatusOK, markRead(t, router, notice.Id))
	require.Equal(t, http.StatusOK, markRead(t, router, notice.Id),
		"重复标记已读必须仍然成功(幂等)")

	var rows []model.CanvasNoticeRead
	require.NoError(t, model.DB.Where("notice_id = ? AND user_id = ?", notice.Id, 7).
		Find(&rows).Error)
	require.Len(t, rows, 1, "重复标记不得产生第二行")

	notices := listNotices(t, router)
	require.Len(t, notices, 1)
	assert.True(t, notices[0].Read, "标记后列表里的 read 应为 true")
}

// TestMarkCanvasNoticeReadRejectsOtherGroupsNotice 归属校验:不能给别组的
// 通知写已读。
//
// 已读表是全局共用的,写进去的是 (notice_id, 自己的 user_id) —— 虽然污染
// 不到别人,但不校验的话任何登录用户都能拿 id 递增把整张已读表填满自己,
// 管理员看到的「这条通知有多少人读过」就全是噪声。响应刻意与「通知不存在」
// 一致(404),不区分「存在但不属于你」与「不存在」,免得变成一个能探测
// 其他分组有没有通知的接口。
func TestMarkCanvasNoticeReadRejectsOtherGroupsNotice(t *testing.T) {
	vipRouter := setupCanvasNoticeTestDB(t, "vip", 7)
	defaultRouter := setupCanvasNoticeTestDB(t, "default", 8)

	notice := &model.CanvasNotice{
		Title: "vip 专属", Content: "...",
		Type: model.CanvasNoticeTypeInfo, TargetGroups: model.CanvasNoticeTargetGroups{"vip"},
		CreatedBy: "admin",
	}
	require.NoError(t, model.DB.Create(notice).Error)

	assert.Equal(t, http.StatusNotFound, markRead(t, defaultRouter, notice.Id),
		"非目标分组的用户不得标记这条通知已读")

	var count int64
	require.NoError(t, model.DB.Model(&model.CanvasNoticeRead{}).
		Where("notice_id = ? AND user_id = ?", notice.Id, 8).Count(&count).Error)
	assert.Zero(t, count, "被拒绝的请求不得留下已读记录")

	// 目标分组的用户自己标记仍然成功 —— 上面的 404 是归属校验,不是接口坏了。
	assert.Equal(t, http.StatusOK, markRead(t, vipRouter, notice.Id))
}

// TestCanvasNoticeReadStateIsPerUser 已读是「每个用户一份」而不是「每条通知一个
// 全局标记」:同一个分组里 A 读过不影响 B 未读。
//
// 这条正是已读表用 (notice_id, user_id) 联合主键、而不是给通知行加一个
// read 布尔的原因 —— 后者在第二个用户标记时会被第一个用户的已读状态覆盖。
func TestCanvasNoticeReadStateIsPerUser(t *testing.T) {
	userARouter := setupCanvasNoticeTestDB(t, "vip", 7)
	userBRouter := setupCanvasNoticeTestDB(t, "vip", 8)

	notice := &model.CanvasNotice{
		Title: "每人一份已读", Content: "...",
		Type: model.CanvasNoticeTypeInfo, TargetGroups: model.CanvasNoticeTargetGroups{"vip"},
		CreatedBy: "admin",
	}
	require.NoError(t, model.DB.Create(notice).Error)

	require.Equal(t, http.StatusOK, markRead(t, userARouter, notice.Id))

	aNotices := listNotices(t, userARouter)
	require.Len(t, aNotices, 1)
	assert.True(t, aNotices[0].Read, "A 读过")

	bNotices := listNotices(t, userBRouter)
	require.Len(t, bNotices, 1)
	assert.False(t, bNotices[0].Read, "B 没读过 —— 已读绝不能是全局标记")
}

// shopCanvasNoticesAdminResponse 是管理端列表的响应信封。
type canvasNoticesAdminResponse struct {
	Success bool                 `json:"success"`
	Message string               `json:"message"`
	Data    []model.CanvasNotice `json:"data"`
}

// setupCanvasNoticeAdminTestDB 注册管理端三个端点。
//
// 不走 AdminAuth(那需要真实的用户会话),这里只测控制器本身的
// 编解码与事务行为 —— 「路由挂上了、被 AdminAuth 拦住」由
// router/canvas_admin_route_test.go 那一类断言负责。
func setupCanvasNoticeAdminTestDB(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	model.DB = db
	require.NoError(t, db.AutoMigrate(&model.CanvasNotice{}, &model.CanvasNoticeRead{}))

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("username", "admin")
		c.Next()
	})
	router.GET("/api/canvas/admin/notices", GetCanvasNoticesAdmin)
	router.POST("/api/canvas/admin/notices", SaveCanvasNoticeAdmin)
	router.DELETE("/api/canvas/admin/notices/:id", DeleteCanvasNoticeAdmin)
	return router
}

// TestCanvasNoticesAdminRoundTrip 管理端「新建 → 列表 → 撤回」的完整往返,
// 并顺带锁住 target_groups 的三种形态转换。
//
// 这条最值得测的原因是它的失败是**静默**的:请求里是数组、库里是 JSON 文本、
// 列表响应里又要还原成数组 —— 中间任何一步漏了转换,接口仍然返回
// success:true,只是前端看到的分组列变成一串带引号的字符串或者空。而
// 分组筛选用的是同一个字段,漏转换的下一步就是通知发不出去。
func TestCanvasNoticesAdminRoundTrip(t *testing.T) {
	router := setupCanvasNoticeAdminTestDB(t)

	// (1) 新建:重复、空串、带空白的分组都应在入库前被规范掉
	body := `{"title":"  分组通知  ","content":"正文","type":"info",
	          "target_groups":["vip",""," vip ","default"]}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/canvas/admin/notices",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var created struct {
		Success bool               `json:"success"`
		Message string             `json:"message"`
		Data    model.CanvasNotice `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	require.True(t, created.Success, "新建应当成功: %s", created.Message)
	require.NotZero(t, created.Data.Id, "新建后应当拿到自增 id")
	assert.Equal(t, "分组通知", created.Data.Title, "标题入库前应去掉首尾空白")
	assert.Equal(t, "admin", created.Data.CreatedBy)
	assert.Equal(t, model.CanvasNoticeTargetGroups{"default", "vip"}, created.Data.TargetGroups,
		"分组应去空白、去空串、去重并排序")

	noticeId := created.Data.Id

	// (2) 列表:target_groups 必须是数组(不是 JSON 字符串),已撤回的也在
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/api/canvas/admin/notices", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var listed canvasNoticesAdminResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listed))
	require.True(t, listed.Success)
	require.Len(t, listed.Data, 1)
	assert.Equal(t, model.CanvasNoticeTargetGroups{"default", "vip"}, listed.Data[0].TargetGroups)
	assert.False(t, listed.Data[0].IsDeleted())

	// 原始 JSON 里 target_groups 得是数组字面量 —— 上面反序列化成 []string
	// 是靠结构体标签兜住的,直接看字节才能证明线上形状确实是数组。
	assert.Contains(t, w.Body.String(), `"target_groups":["default","vip"]`)

	// (3) 撤回:行仍在列表里(前端「是否已删除」列要用),但画布端再也拉不到
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodDelete,
		fmt.Sprintf("/api/canvas/admin/notices/%d", noticeId), nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/api/canvas/admin/notices", nil)
	router.ServeHTTP(w, req)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listed))
	require.Len(t, listed.Data, 1, "撤回过的通知仍留在管理端列表里")
	assert.True(t, listed.Data[0].IsDeleted(), "撤回后 deleted_at 应有值")
}

// TestCanvasNoticesAdminEmptyListIsArray 一条通知都没有时,data 必须是 []
// 而不是 null。
//
// 单独成一个测试(而不是塞进上面的往返里):测试用的内存库 DSN 取自
// t.Name(),同一个测试函数里第二次建库会连到同一份数据上,「空列表」这个
// 前提根本构造不出来。前端两处消费方都能靠 ?? [] 兜住 null,但空列表在
// 契约上就该是空数组,不该多一种形态让每个消费方各兜一次。
func TestCanvasNoticesAdminEmptyListIsArray(t *testing.T) {
	router := setupCanvasNoticeAdminTestDB(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/canvas/admin/notices", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"data":[]`)
}
