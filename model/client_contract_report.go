package model

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 计入支持率的默认时间窗。半年前上报过一次的装机不该拉低支持率,
// 但行本身保留 —— 排查「哪个客户还在用老版本」时有用。
const defaultReportWindowDays = 30

// ClientContractReport 是每个画布装机的最新契约支持声明。
//
// 一装机一行(按 install_id upsert),不做追加日志:画布每次登录/同步都上报,
// 追加会无界增长,而支持率只关心每个装机的最新状态。
type ClientContractReport struct {
	Id int `json:"id" gorm:"primaryKey"`

	// 画布侧生成的随机装机标识,跨重启稳定。唯一索引 —— upsert 的冲突键。
	InstallID string `json:"install_id" gorm:"type:varchar(64);uniqueIndex;not null"`

	ClientVersion string `json:"client_version" gorm:"type:varchar(32);index"`

	// 该装机声明支持的契约名列表,JSON 数组。
	// 存 blob 而非联接表 —— 聚合在 Go 侧遍历完成,当前装机量级(百到千)够用。
	Contracts string `json:"contracts" gorm:"type:text"`

	// 客户端支持的 schema 词表版本,用于判断参数模式兼容性
	SchemaVocab int `json:"schema_vocab"`

	// 端点已认证,顺手存下 —— 排查「哪个客户还在用老版本」比只有装机数有用
	UserId int `json:"user_id" gorm:"index"`

	CreatedTime int64 `json:"created_time" gorm:"bigint"`
	UpdatedTime int64 `json:"updated_time" gorm:"bigint;index"`

	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`
}

func (r *ClientContractReport) Validate() error {
	if strings.TrimSpace(r.InstallID) == "" {
		return errors.New("install_id 不能为空")
	}
	if strings.TrimSpace(r.ClientVersion) == "" {
		return errors.New("client_version 不能为空")
	}
	return nil
}

// normalizeContracts 去重并剔除空白项 —— 客户端可能重复上报同一契约
func normalizeContracts(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, c := range in {
		c = strings.TrimSpace(c)
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out
}

// SetContracts 把契约列表序列化进 Contracts 字段
func (r *ClientContractReport) SetContracts(contracts []string) error {
	b, err := json.Marshal(normalizeContracts(contracts))
	if err != nil {
		return err
	}
	r.Contracts = string(b)
	return nil
}

func (r *ClientContractReport) GetContracts() []string {
	if r.Contracts == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(r.Contracts), &out); err != nil {
		return nil
	}
	return out
}

// Upsert 按 install_id 插入或更新。
//
// 用 ON CONFLICT 而非「先查再写」—— 同一装机的并发上报(登录与同步几乎同时触发)
// 在先查再写下会双写,唯一索引会让其中一个报错。
func (r *ClientContractReport) Upsert() error {
	if err := r.Validate(); err != nil {
		return err
	}
	now := common.GetTimestamp()
	if r.CreatedTime == 0 {
		r.CreatedTime = now
	}
	r.UpdatedTime = now

	return DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "install_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"client_version", "contracts", "schema_vocab", "user_id", "updated_time",
		}),
	}).Create(r).Error
}

// windowCutoff 返回「在线」窗口的起始时间戳。
// 非正的天数回退到默认 —— 否则截断点会算到未来,统计分母变 0。
func windowCutoff(windowDays int) int64 {
	if windowDays <= 0 {
		windowDays = defaultReportWindowDays
	}
	return time.Now().AddDate(0, 0, -windowDays).Unix()
}

// ContractStat 是单个契约的支持情况
type ContractStat struct {
	Contract  string `json:"contract"`
	Supported int    `json:"supported"` // 支持它的在线装机数
	Total     int    `json:"total"`     // 在线装机总数(分母)
}

// GetContractSupportStats 统计每个契约在在线装机里的支持率。
//
// 全表扫描窗口内的行并在 Go 侧聚合。装机数到万级时会变慢 ——
// 若真到那个量级,改成物化计数表(每次上报增量更新)。
// 当前量级(单店铺,百到千装机)不值得为它加复杂度。
func GetContractSupportStats(windowDays int) (map[string]ContractStat, error) {
	cutoff := windowCutoff(windowDays)

	var rows []ClientContractReport
	err := DB.Select("contracts").
		Where("updated_time >= ?", cutoff).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}

	total := len(rows)
	stats := make(map[string]ContractStat)

	for _, row := range rows {
		for _, c := range row.GetContracts() {
			s := stats[c]
			s.Contract = c
			s.Supported++
			stats[c] = s
		}
	}

	// 分母对每个契约都是在线装机总数
	for k, s := range stats {
		s.Total = total
		stats[k] = s
	}

	return stats, nil
}

// CountOnlineInstalls 返回窗口内的在线装机数。
// 即便没有任何契约被支持,后台也要能显示分母。
func CountOnlineInstalls(windowDays int) (int64, error) {
	var n int64
	err := DB.Model(&ClientContractReport{}).
		Where("updated_time >= ?", windowCutoff(windowDays)).
		Count(&n).Error
	return n, err
}
