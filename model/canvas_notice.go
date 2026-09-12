package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CanvasNoticeTypeInfo 是当前唯一使用的通知类型。
//
// type 只存字符串、不做成常量枚举表:后台对未知值按 info 渲染、画布侧 serde
// 忽略未知字段,以后加 warning / maintenance 之类的新类型既不用改表、也不用
// 前后端同时发版。
const CanvasNoticeTypeInfo = "info"

// CanvasNoticeTargetGroups 是「这条通知发给哪些分组」的分组名集合。
//
// 三种形态由这一个类型统一转换:Go 里是 []string、落库是 JSON 文本、进出
// HTTP API 是 JSON 数组。之所以不写成 plain string 再在每处手工
// Parse/Encode —— 只要有两处漏了转换,表现就是分组筛选静默失效(通知谁都
// 看不到、或管理员在后台看到一串引号),没有任何报错。与 types.PriceTierList、
// model/prefill_group.go 的 JSON 列模式同构。
type CanvasNoticeTargetGroups []string

// Value 实现 driver.Valuer。空集合写成空 JSON 数组而不是 NULL:
// 「一个分组都没选」是一个有意义的取值(见 CanvasNotice.TargetsGroup),
// 存 NULL 会让它和「旧数据没有这一列」混成同一种情况。
func (g CanvasNoticeTargetGroups) Value() (driver.Value, error) {
	if g == nil {
		return "[]", nil
	}
	return json.Marshal([]string(g))
}

// Scan 实现 sql.Scanner:兼容各驱动返回的 []byte / string / nil。
func (g *CanvasNoticeTargetGroups) Scan(value interface{}) error {
	switch v := value.(type) {
	case nil:
		*g = nil
	case []byte:
		g.unmarshal(v)
	case string:
		g.unmarshal([]byte(v))
	default:
		return fmt.Errorf("解析通知目标分组失败:不支持的列类型 %T", value)
	}
	return nil
}

// unmarshal 解不开时按「没有目标分组」处理,并把 error 记进日志。
//
// 不把 error 往上传:一行坏数据不该让整张通知列表(管理端和画布端都是)
// 直接查不出来。而这个方向的降级是安全的 —— 解析失败退化成「谁都不发」,
// 比退化成「发给所有人」安全得多。
func (g *CanvasNoticeTargetGroups) unmarshal(raw []byte) {
	*g = nil
	if len(raw) == 0 {
		return
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		common.SysError(fmt.Sprintf("解析通知目标分组失败,该行按无目标分组处理: %v", err))
		return
	}
	*g = out
}

// CanvasNotice 是管理员写给画布用户的一条通知。
//
// 为什么不复用既有的两套设施:
//   - setting/console_setting 的 Announcements 是全站广播,存在 options 表、
//     既没有定向字段也不该有 —— 它内嵌在**无鉴权的** /api/status 里下发,
//     给广播加一个「发给哪些分组」的字段等于把分组拓扑泄漏给任何匿名请求。
//   - service/user_notify.go 的 NotifyUser 是外部推送(邮件 / webhook / Bark),
//     没有持久化、没有已读、也没有拉取接口 —— 推出去的东西用户换台设备就没了,
//     而画布要的恰恰是「拉取 + 已读跨设备一致」。
type CanvasNotice struct {
	Id      int    `gorm:"primaryKey;autoIncrement" json:"id"`
	Title   string `gorm:"type:varchar(255)" json:"title"`
	Content string `gorm:"type:text" json:"content"`
	Type    string `gorm:"type:varchar(32)" json:"type"`
	// TargetGroups 是收件分组名数组(存 JSON 文本)。分组名在库里本来就是
	// 松散字符串(user.group 是 varchar),没有分组主表可以外键,建中间表只会
	// 多一层没有任何约束力的 join;而分组是个位数、通知是几十条量级,
	// 全量读出来在应用层解析的开销可以忽略。
	TargetGroups CanvasNoticeTargetGroups `gorm:"type:text" json:"target_groups"`
	// CreatedBy 存管理员用户名而不是 id:这张表是运维留痕用的,用户名比一个
	// 查不到人的数字 id 有用,管理员改名或删号也不会让这行读不出来。
	CreatedBy string `gorm:"type:varchar(255)" json:"created_by"`
	// DeletedAt 是**历史遗留列**,新代码不再写入它(删除已改成物理删行)。
	//
	// 为什么留着这一列而不是 DropColumn 丢掉:
	//   - 它是下面 `deleted_at IS NULL` 过滤条件的存在前提。丢列就必须同时
	//     去掉过滤,而过滤是「清行失败时撤回内容不外泄」的唯一兜底 ——
	//     为纯装饰性的 schema 整洁换一条数据泄漏路径不划算。
	//   - 覆盖窗口期(旧二进制还在跑)里旧实例仍会写它,过滤能把那些行挡住。
	//   - 去掉结构体字段还会让**全新部署**建表时没有这一列,届时查询里的
	//     `deleted_at IS NULL` 直接变成 SQL 错误,每次拉取通知都失败。
	//
	// 用显式可空时间列,而不是 GORM 的 gorm.DeletedAt:后者的 Delete() 会静默
	// 变成软删除、所有查询自动追加 deleted_at IS NULL,而管理端要看完整列表,
	// 那层隐式过滤反而得处处 Unscoped 绕开。
	DeletedAt *time.Time `json:"deleted_at"`
	CreatedAt time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

func (CanvasNotice) TableName() string {
	return "canvas_notices"
}

// IsDeleted 表示这条通知带着历史撤回标记。
//
// 新代码不会产生这样的行(删除已是物理删行),它只在**覆盖窗口**里有意义:
// 旧实例软删的行在新实例眼里仍是「已撤回」,于是列表过滤与已读归属校验
// 两处口径继续一致 —— 否则会出现「列表里看不到、却还能标记已读」的缝。
func (n *CanvasNotice) IsDeleted() bool {
	return n.DeletedAt != nil
}

// TargetsGroup 判断这条通知是否发给该分组。
//
// 空分组(取不到有效分组信息)与空目标分组一律判为「不发给任何人」——
// 这是刻意选的错误方向。若把「目标分组为空」理解成广播,管理员在后台漏选
// 一次分组就会把内测通知群发给全部用户;而判成「谁都看不到」最多是漏发
// 一条通知,发现后重新发一条即可。
func (n *CanvasNotice) TargetsGroup(group string) bool {
	if group == "" {
		return false
	}
	for _, g := range n.TargetGroups {
		if g == group {
			return true
		}
	}
	return false
}

// CanvasNoticeRead 是「某用户读过某通知」的记录。
//
// 已读状态存服务端是刻意的选择:画布是多设备客户端,只存本地的话换台设备
// 就重新弹一遍;而「已读」是用户对这条通知的表态,不是设备本地偏好。
// (画布的 install_id 重装即变,靠设备侧存也做不到稳定去重。)
//
// 主键是 (notice_id, user_id) 联合唯一:标记已读天然幂等,重复标记不会
// 报错也不会产生第二行 —— 客户端不需要在本地维护「这条标过没有」。
type CanvasNoticeRead struct {
	NoticeId int       `gorm:"primaryKey" json:"notice_id"`
	UserId   int       `gorm:"primaryKey" json:"user_id"`
	ReadAt   time.Time `gorm:"autoCreateTime" json:"read_at"`
}

func (CanvasNoticeRead) TableName() string {
	return "canvas_notice_reads"
}

// ListCanvasNoticesForGroup 返回发给该分组的通知,按发布时间倒序。
//
// 分组匹配在应用层做,不在 SQL 里 —— target_groups 是 JSON 文本,而
// MySQL / SQLite / PostgreSQL 的 JSON 函数语法各不相同(JSON_CONTAINS /
// json_each / jsonb 运算符),走不了索引,且写错只会在其中一种方言上静默
// 返回空集,表现是「用户永远收不到通知」而没有任何报错。通知总量是几十条
// 量级,全表拉出来在 Go 里过一遍,换一份三种部署下行为一致的实现。
//
// 排序带 id 兜底:同一秒创建的两条通知只按 created_at 排会得到不稳定的
// 顺序,客户端下拉列表会在两次拉取之间自己换位置。
//
// `deleted_at IS NULL` 保留着,哪怕新代码已经不会再写入那一列:它既是
// 覆盖窗口期(旧二进制软删、新二进制读取)唯一挡住撤回内容的过滤,也是
// 启动清理万一失败时的兜底。多一个恒真的条件,换「撤回过的通知绝不会
// 重新下发」这条硬保证。
func ListCanvasNoticesForGroup(group string) ([]CanvasNotice, error) {
	var rows []CanvasNotice
	if err := DB.Where("deleted_at IS NULL").
		Order("created_at DESC, id DESC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]CanvasNotice, 0, len(rows))
	for i := range rows {
		if rows[i].TargetsGroup(group) {
			out = append(out, rows[i])
		}
	}
	return out, nil
}

// GetCanvasNoticeReadMap 返回某用户在这批通知里的已读集合。
//
// 调用方(列表的 read 标记、mark-read 的归属校验)只需要「读过没有」这一个
// 布尔,所以返回集合而不是明细行。
func GetCanvasNoticeReadMap(userId int, noticeIds []int) (map[int]struct{}, error) {
	read := make(map[int]struct{})
	if len(noticeIds) == 0 {
		return read, nil
	}
	var rows []CanvasNoticeRead
	if err := DB.Where("user_id = ? AND notice_id IN ?", userId, noticeIds).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		read[rows[i].NoticeId] = struct{}{}
	}
	return read, nil
}

// MarkCanvasNoticeRead 幂等标记已读:命中已有 (notice_id, user_id) 时什么都不做,
// 不报错、不产生第二行、也不刷新首次已读时间。
//
// 用 OnConflict DoNothing 而不是「先查再插」:后者在客户端并发重试或用户双击下
// 会撞主键报错,而调用方拿到的错误与「真的写失败了」无法区分。
func MarkCanvasNoticeRead(noticeId, userId int) error {
	read := CanvasNoticeRead{NoticeId: noticeId, UserId: userId}
	return DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&read).Error
}

// GetCanvasNoticeById 按 id 取一条,不存在返回 gorm.ErrRecordNotFound。
func GetCanvasNoticeById(id int) (*CanvasNotice, error) {
	var notice CanvasNotice
	if err := DB.First(&notice, id).Error; err != nil {
		return nil, err
	}
	return &notice, nil
}

// ListAllCanvasNotices 返回全部通知,按发布时间倒序。
//
// 不过滤 deleted_at:管理端列表的语义是「库里真实存在的通知」。撤回过的
// 历史行由 main.go 的 purgeSoftDeletedCanvasNotices 在每次启动时清掉;
// 在那之前它们被上面的过滤挡着不会下发,这里读出来只是覆盖窗口里的短暂状态。
func ListAllCanvasNotices() ([]CanvasNotice, error) {
	// 初始化成非 nil 空切片:没有通知时 GORM 会把 nil 原样留在变量里,
	// 序列化出去就是 data: null 而不是 [] —— 前端能靠 ?? [] 兜住,但
	// 「空列表」在契约上就该是空数组,不该多一种形态让每个消费方各兜一次。
	rows := make([]CanvasNotice, 0)
	if err := DB.Order("created_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// SaveCanvasNotice 新建(id 为 0)或更新一条通知。
func SaveCanvasNotice(notice *CanvasNotice) error {
	if notice.Id == 0 {
		return DB.Create(notice).Error
	}
	targetGroups, err := notice.TargetGroups.Value()
	if err != nil {
		return err
	}
	// 显式列出可改的列:created_at / created_by / deleted_at 都不在其中 ——
	// 「谁在什么时候发的」不该被一次正文编辑改写;deleted_at 同理,一次编辑
	// 不该让覆盖窗口里已撤回的行重新下发给用户。
	//
	// 用 map 而不是结构体做 Updates:GORM 对结构体的零值字段是「跳过」语义,
	// 而把内容改成空串、把目标分组清空都是合法编辑,用结构体会静默不生效。
	return DB.Model(&CanvasNotice{}).Where("id = ?", notice.Id).Updates(map[string]interface{}{
		"title":         notice.Title,
		"content":       notice.Content,
		"type":          notice.Type,
		"target_groups": targetGroups,
		"updated_at":    time.Now(),
	}).Error
}

// DeleteCanvasNotice 删除一条通知,连带清掉它的已读记录。
//
// 是**物理删除**而不是打撤回标记:调用返回后这条通知就不存在了,管理端
// 列表里不会再出现它,也不存在「删了但还在」的中间态。
//
// 已读记录必须先删:它们是 (notice_id, user_id) 行,通知行一没,这些行
// 既没有查询会读、又永远等不到清理,只会让这张随通知数增长的表白涨。
//
// 两步必须同一个事务 —— 漏了删已读,同 id 复用时会出现一批「凭空已读」;
// 反过来漏了删通知行,用户会看到一条已清空已读的旧通知重新变成未读。
//
// 行不存在时由 tx.First 返回 gorm.ErrRecordNotFound,controller 转 404:
// 重复删同一条会得到「通知不存在」而不是静默成功 —— 这正是管理端想要的,
// 「我删的这条已经没了」应当被看见。
func DeleteCanvasNotice(id int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var notice CanvasNotice
		if err := tx.First(&notice, id).Error; err != nil {
			return err
		}
		if err := tx.Where("notice_id = ?", id).Delete(&CanvasNoticeRead{}).Error; err != nil {
			return err
		}
		// 按主键删整行。CanvasNotice 的 DeletedAt 是普通 *time.Time 而非
		// gorm.DeletedAt,不实现软删除接口,所以这里是真 DELETE。
		return tx.Delete(&CanvasNotice{}, id).Error
	})
}
