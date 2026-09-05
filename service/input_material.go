package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// materialSource 取值与 types.ResolvedInputMaterial.Source 契约一致。
const (
	materialSourceExplicit        = "explicit"
	materialSourceInlineMeasured  = "inline_measured"
	materialSourceUrlProbed       = "url_probed"
	materialSourceEstimated       = "estimated"
	materialSourceSettleCorrected = "settle_corrected"
)

// 提交链路的短预算同步探测(占用请求时延,压在 2.5s)与后台补探测的长预算
// (结果只进缓存,不阻塞提交,由结算修正消费)。
const (
	materialProbeBudget   = 2500 * time.Millisecond
	materialReprobeBudget = 30 * time.Second
)

// audioExtByMime 把 data URI 的 audio/* MIME 映射到 common.GetAudioDuration
// 认识的扩展名;未收录的 MIME 走不了内联实测,落估算路径。
var audioExtByMime = map[string]string{
	"audio/mpeg":     ".mp3",
	"audio/mp3":      ".mp3",
	"audio/wav":      ".wav",
	"audio/x-wav":    ".wav",
	"audio/wave":     ".wav",
	"audio/vnd.wave": ".wav",
	"audio/mp4":      ".m4a",
	"audio/m4a":      ".m4a",
	"audio/x-m4a":    ".m4a",
	"audio/ogg":      ".ogg",
	"audio/oga":      ".ogg",
	"audio/opus":     ".opus",
	"audio/flac":     ".flac",
	"audio/x-flac":   ".flac",
	"audio/aac":      ".aac",
	"audio/webm":     ".webm",
	"audio/aiff":     ".aiff",
	"audio/x-aiff":   ".aiff",
}

// ResolveInputMaterials 把检测出的素材清单定稿为计费快照并算出素材费额度。
// 时长优先级:请求显式(explicitSeconds>0,钳制)→ 探测缓存 → 短预算同步探测
// (2.5s)→ 系统默认估算;探测失败的 URL 素材起后台 goroutine 长预算补探测
// 写缓存(结果只进缓存,不阻塞提交;结算时经 RefreshMaterialDurationsAtSettle
// 消费)。图片素材不探测,按张计。
//
// 素材费 = Σ(图:每张价;视频/音频:时长 × 每秒价) × groupRatio,
// 转换走 common.QuotaFromFloatChecked,饱和时通过 clamp 返回给调用方审计。
//
// explicitSeconds 由调用方(relay 提交链路)从请求显式时长提示生成,只对视频
// 素材给非零值、音频恒 0:请求没显式说时长时返回 0,让该素材继续走探测/估算,
// 结算时可被缓存真值修正。
func ResolveInputMaterials(c *gin.Context, materials []types.ResolvedInputMaterial, prices types.InputMaterialPriceList, groupRatio float64, explicitSeconds func(material types.ResolvedInputMaterial) int) ([]types.ResolvedInputMaterial, int, *common.QuotaClamp) {
	if len(materials) == 0 || len(prices) == 0 {
		return nil, 0, nil
	}
	priceByType := make(map[string]types.InputMaterialPrice, len(prices))
	for _, p := range prices {
		priceByType[p.MaterialType] = p
	}

	total := 0.0
	resolved := make([]types.ResolvedInputMaterial, 0, len(materials))
	var firstClamp *common.QuotaClamp
	for _, m := range materials {
		p, ok := priceByType[m.MaterialType]
		if !ok {
			continue // 该类型未配置素材价 = 不计费
		}
		if m.MaterialType == types.MaterialTypeImage {
			m.UnitPrice = p.PricePerUnit
			m.Source = materialSourceExplicit // 图片无时长,来源字段仅作占位
		} else {
			m.UnitPrice = p.PricePerSecond
			m.Seconds, m.Source = resolveMaterialSeconds(m, p, explicitSeconds)
		}
		entry := m.UnitPrice
		if m.MaterialType != types.MaterialTypeImage {
			entry = m.Seconds * m.UnitPrice
		}
		entry *= groupRatio
		quota, clamp := common.QuotaFromFloatChecked(entry * common.QuotaPerUnit)
		if clamp != nil && firstClamp == nil {
			firstClamp = clamp
		}
		total += float64(quota)
		resolved = append(resolved, m)
	}
	materialQuota := int(total) // 每项已各自饱和,累加值远离溢出
	return resolved, materialQuota, firstClamp
}

// resolveMaterialSeconds 单条素材的时长与来源,按显式 → 内联实测 → 探测缓存
// → 短预算探测 → 系统默认估算的优先级落定;所有结果钳制到
// MaxTaskDurationSeconds(时长是计费乘数)。
func resolveMaterialSeconds(m types.ResolvedInputMaterial, p types.InputMaterialPrice, explicitSeconds func(material types.ResolvedInputMaterial) int) (float64, string) {
	if explicitSeconds != nil {
		if s := explicitSeconds(m); s > 0 {
			return math.Min(float64(s), float64(relaycommon.MaxTaskDurationSeconds)), materialSourceExplicit
		}
	}
	// 内联音频:base64 data URI 直接实测(复用 GetAudioDuration),实测不到
	// (解码失败/格式不支持/0 时长)落探测缓存与估算。
	if strings.HasPrefix(m.URL, "data:audio") {
		if i := strings.Index(m.URL, ";base64,"); i >= 0 {
			mime := m.URL[len("data:"):]
			if j := strings.IndexByte(mime, ';'); j >= 0 {
				mime = mime[:j]
			}
			ext := audioExtByMime[mime]
			data, err := base64.StdEncoding.DecodeString(m.URL[i+len(";base64,"):])
			if ext != "" && err == nil {
				if d, err := common.GetAudioDuration(context.Background(), bytes.NewReader(data), ext); err == nil {
					if seconds, ok := saneProbeSeconds(d); ok {
						return math.Min(seconds, float64(relaycommon.MaxTaskDurationSeconds)), materialSourceInlineMeasured
					}
				}
			}
		}
	}
	// 探测缓存:同 URL 早前探测过(含上一次提交的后台补探测)直接复用。
	if d, ok := CachedMediaDuration(m.URL); ok {
		return math.Min(d, float64(relaycommon.MaxTaskDurationSeconds)), materialSourceUrlProbed
	}
	// 短预算同步探测。
	if d, ok := ProbeMediaDuration(m.URL, materialProbeBudget); ok {
		return math.Min(d, float64(relaycommon.MaxTaskDurationSeconds)), materialSourceUrlProbed
	}
	// 短探测失败的 URL 素材:后台长预算补探测,fire-and-forget,成果只进
	// 缓存(结算时经 RefreshMaterialDurationsAtSettle 修正),不回写本请求。
	if isHTTPURL(m.URL) {
		url := m.URL
		go func() {
			// 机会主义 try-acquire:信号量忙(8 槽被前台短探测等占用)时直接
			// 放弃,不排队——下次同 URL 提交还会再触发,零损失。
			select {
			case mediaProbeSem <- struct{}{}:
			default:
				return
			}
			defer func() { <-mediaProbeSem }()
			if d, ok := probeMediaDuration(url, materialReprobeBudget); ok {
				urlHash := sha256.Sum256([]byte(url))
				logger.LogInfo(context.Background(), fmt.Sprintf("素材时长后台补探测成功 url_hash=%x seconds=%.0f", urlHash[:8], d))
			}
		}()
	}
	// 系统默认估算:优先用该模型配置的 DefaultSeconds,否则系统默认
	// (视频 20s / 音频 60s)。
	def := p.DefaultSeconds
	if def <= 0 {
		if m.MaterialType == types.MaterialTypeVideo {
			def = ratio_setting.MaterialDefaultVideoSeconds
		} else {
			def = ratio_setting.MaterialDefaultAudioSeconds
		}
	}
	return math.Min(float64(def), float64(relaycommon.MaxTaskDurationSeconds)), materialSourceEstimated
}

// isHTTPURL 判定是否远程 URL:只有 http(s) 素材值得探测/补探测,data: 与
// 其他形态直接跳过。
func isHTTPURL(url string) bool {
	return strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://")
}

// RefreshMaterialDurationsAtSettle 结算修正:对提交时为估算来源的 URL 素材,
// 用缓存真值(后台补探测的成果)重定时长。返回修正后的清单与是否有变化;
// 不修改入参,修正后的条目来源标记为 settle_corrected。
func RefreshMaterialDurationsAtSettle(resolved []types.ResolvedInputMaterial) ([]types.ResolvedInputMaterial, bool) {
	changed := false
	out := make([]types.ResolvedInputMaterial, len(resolved))
	copy(out, resolved)
	for i := range out {
		m := &out[i]
		if m.Source != materialSourceEstimated || !isHTTPURL(m.URL) {
			continue
		}
		if d, ok := CachedMediaDuration(m.URL); ok {
			d = math.Min(d, float64(relaycommon.MaxTaskDurationSeconds))
			if math.Abs(d-m.Seconds) > 1e-9 {
				m.Seconds = d
				m.Source = materialSourceSettleCorrected
				changed = true
			}
		}
	}
	return out, changed
}
