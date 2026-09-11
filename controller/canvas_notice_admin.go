package controller

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// canvasNoticeUpsertRequest 是后台「画布通知」表单的请求体。
//
// target_groups 用 model.CanvasNoticeTargetGroups 而不是 []string:让
// 「请求里是 JSON 数组、库里是 JSON 文本」共用同一份编解码,不在这里
// 手工 Marshal —— 手工转换漏一次就是「分组筛选静默失效」。
type canvasNoticeUpsertRequest struct {
	// Id 为 0 = 新建,非 0 = 更新该条。
	Id           int                            `json:"id"`
	Title        string                         `json:"title"`
	Content      string                         `json:"content"`
	Type         string                         `json:"type"`
	TargetGroups model.CanvasNoticeTargetGroups `json:"target_groups"`
}

// normalizeTargetGroups 清洗后台传来的分组名:去空白、丢空串、去重、排序。
//
// 去重与排序都只为了后台列表可读 —— 匹配语义与顺序无关。但同一个分组在
// 表单里显示两次会让人以为「这条发了两遍」,所以入库前统一成规范形态。
func normalizeTargetGroups(groups model.CanvasNoticeTargetGroups) model.CanvasNoticeTargetGroups {
	seen := make(map[string]struct{}, len(groups))
	out := make(model.CanvasNoticeTargetGroups, 0, len(groups))
	for _, g := range groups {
		name := strings.TrimSpace(g)
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// GetCanvasNoticesAdmin 列出全部通知,**含已撤回的**。
//
// 与画布端 ListCanvasNoticesForGroup 的差别只在这一个过滤条件上:前端要用
// deleted_at 渲染「是否已删除」列,撤回过的行留在列表里管理员才能回答
// 「这条是不是发过、什么时候撤的」。行里直接带 target_groups 数组(自定义
// 类型的 json tag 就是数组形态),前端不需要再解析一次 JSON 字符串。
func GetCanvasNoticesAdmin(c *gin.Context) {
	rows, err := model.ListAllCanvasNotices()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rows)
}

// SaveCanvasNoticeAdmin 新建(不传 id)或更新(传 id)一条通知。
func SaveCanvasNoticeAdmin(c *gin.Context) {
	var req canvasNoticeUpsertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "请求格式错误: "+err.Error())
		return
	}

	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		common.ApiErrorMsg(c, "标题不能为空")
		return
	}
	// type 缺省补成 info:前端目前只有一个选项,但接口不该让空 type 落库 ——
	// 存进去的空串会让以后「按 type 分流渲染」多一种要兼容的隐式取值。
	noticeType := strings.TrimSpace(req.Type)
	if noticeType == "" {
		noticeType = model.CanvasNoticeTypeInfo
	}

	notice := &model.CanvasNotice{
		Id:           req.Id,
		Title:        req.Title,
		Content:      req.Content,
		Type:         noticeType,
		TargetGroups: normalizeTargetGroups(req.TargetGroups),
	}

	if req.Id == 0 {
		notice.CreatedBy = c.GetString("username")
	} else {
		// 更新前先确认这条存在且没被撤回:对已撤回的通知保存会「成功但
		// 什么都不发生」(撤回标记仍在,画布端依旧看不到),是个看起来
		// 正常、实际静默失效的接口行为,不如直接报错。
		existing, err := model.GetCanvasNoticeById(req.Id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "通知不存在"})
				return
			}
			common.ApiError(c, err)
			return
		}
		if existing.IsDeleted() {
			common.ApiErrorMsg(c, "该通知已删除,不能编辑")
			return
		}
	}

	if err := model.SaveCanvasNotice(notice); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, notice)
}

// DeleteCanvasNoticeAdmin 撤回一条通知,并连带清掉它的已读记录。
//
// 「撤回」而不是物理删除:通知行本身留着(前端「是否已删除」列要用),
// 已读记录则物理删掉 —— 它们只在通知还发着时有意义。两步在 model 层同一
// 事务里完成。
func DeleteCanvasNoticeAdmin(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "通知 id 不合法")
		return
	}
	if err := model.DeleteCanvasNotice(id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "通知不存在"})
			return
		}
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
