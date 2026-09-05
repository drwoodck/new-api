# 设计文档:输入素材计费 + 定价打通 + 统一上新工作台

- 日期:2026-09-04
- 状态:待用户审阅
- 范围:后端(Go)+ 前端(web/,React 19 + Rsbuild)

## 1. 背景与目标

1. 视频模型在"分组按档按秒计费"基础上,新增**输入素材计费**:上传视频/音频按秒、上传图片按张
2. "计费与支付-模型定价"与"模型与路由-画布模型目录"的定价信息**打通一致**
3. 画布目录上新简化:巡检 `autoAdded > 0` 时自动起草目录条目(contract 由 capability 推导,`enabled=false` 起草态);表单选项化、字段带说明;管理端从"填表"退化为"审核+开闸"
4. 目录条目"说明"与模型管理页模型说明**统一真源**;修复目录说明保存失效 bug
5. 模型管理页(`/models/metadata`)上新流程简化为"审核+开闸"
6. 上新流程总方案:用户选定**方案 B——统一上新工作台**

已确认的关键决策:

| 决策点 | 结论 |
|---|---|
| 素材计费技术路线 | 新增**加法**计费维度(不扩展 price_tiers 档型,不用乘法倍率) |
| 素材时长获取 | 提交时探测 URL 真值(2~3s 预算)+ 结算时修正;失败回退估算 |
| 无时长兜底 | 估算:视频默认 20s、音频默认 60s(可配置) |
| 固定价分组化 | 所有固定价格(按次/秒价/分档表/素材价)支持按分组独立定价 |
| 价格文案 | 自动生成,手填可覆盖 |
| 上新流程 | 方案 B:统一上新工作台(巡检自动起草作为进料) |
| "模式设置"所指 | 模型管理页 `/models/metadata` |

## 2. 现状要点(代码事实)

- 视频计费三级解析:`price_tiers` 分档表 → 全局 `VideoSecondPrice` → 按次/倍率(`relay/helper/price.go` `ModelPriceHelperPerCall`);分档/秒价经 `PriceData` 预扣,结算在 `service/task_polling.go` `settleVideoSecondBilling`
- `price_tiers`:`types/price_tier.go`,tier_type ∈ request/resolution/image_size/mode,billing_unit ∈ second/request;分组表存 `model_group_price.price_tiers`(text JSON)
- `ModelGroupPrice`(`model/model_group_price.go:32-42`):ModelRatio/CompletionRatio/ModelPrice/PriceTiers 四个可空字段;**无秒价列、无素材价列**;分别定价开关为 `models.group_pricing_enabled`
- 输入素材计费现状:仅豆包一处硬编码 `video_input` 乘法倍率(`relay/channel/task/doubao/constants.go`)
- 请求素材入口:`TaskSubmitReq.Image/Images/InputReference/Metadata`(`relay/common/relay_info.go`);豆包素材走 `metadata.content[]`(video_url/audio_url)
- 巡检(`controller/channel_upstream_update.go`):仅写 `channels.models` + `abilities`;`autoAdded > 0` 只计数进通知;调度默认 30 分钟(`controller/system_task_handlers.go`)
- capability→contract 映射已在位:`constant/canvas_contract.go` `capabilityToContract` + `ContractForCapability` + `IsSupportedContract`
- 目录模型(`model/canvas_catalog.go`):`Enabled *bool`(三态)、`Pricing` 为自由展示文本、`Description` 独立列;`Update()` 为 Select 白名单(含 description)
- **说明保存失效根因**:`catalog-overview` 接口不返回 description/pricing/limitations/param_schema/schema_override 等列;`overviewRowToCatalogModel` 将其置空;编辑提交后空串覆盖库值(`controller/canvas_catalog_admin.go` + `canvas-catalog-section.tsx`)
- 说明双源:用户端定价页用 `models.Description`(`model/model_meta.go`);画布目录另有一份 description 列,互不相通
- 缺失模型聚合已存在:`GET /api/models/missing`(`model/missing_models.go`)
- 音频头部解析已存在:`common.GetAudioDuration`(`common/audio.go`,mp3/wav/flac/m4a/ogg/opus/aiff/webm/aac)——multipart 场景的辅助路径
- 前端 `CONTRACT_BY_CAPABILITY` 与后端映射表重复定义(`canvas-catalog-form-dialog.tsx`),存在漂移风险

## 3. 设计

### 3.1 输入素材计费(需求 1)

**计费语义**:`总费用 = 生成费(现有分档/秒价/按次) + 素材费(加法)`,素材费不进 OtherRatios(乘法体系)。

**价格类型**(新 `types/input_material.go`):

```go
type InputMaterialPrice struct {
    MaterialType   string  `json:"material_type"`   // "image" | "video" | "audio"
    PricePerUnit   float64 `json:"price_per_unit"`   // 图片:每张
    PricePerSecond float64 `json:"price_per_second"` // 视频/音频:每秒
    DefaultSeconds int     `json:"default_seconds"`  // 无真值时估算秒数,0=系统默认
}
type InputMaterialPriceList []InputMaterialPrice // Valuer/Scanner,存 text JSON
```

校验:material_type 枚举内、每类型至多一条;价格 ∈ [0, MaxTierPrice 同级上限];DefaultSeconds 钳制到 `MaxTaskDurationSeconds`。

**存储**:

- 全局:新 option 键 `InputMaterialPrices`(JSON:model → list),`ratio_setting` 注册,模式对齐 `VideoPriceTiers`(整表校验、非法整体拒绝)
- 分组:`model_group_price` 新增可空列 `input_material_prices *types.InputMaterialPriceList gorm:"type:text"`,经 `ReplaceModelGroupPrices` 事务校验写入
- 语义:分别定价模式用行内表;统一模式用全局表 × GroupRatio(与秒价同语义)

**系统默认估算秒数**:视频 20s、音频 60s,option 键 `MaterialDefaultVideoSeconds` / `MaterialDefaultAudioSeconds`(int,默认 20/60),使用前钳制到 `MaxTaskDurationSeconds`。

**素材检测**(新 `relay/common/input_material.go`,集中一处,全渠道共用):

- 图片:`Image != ""` 计 1 + `Images[]` 逐条计数;超上限(`MaxInputMaterialCount = 100`,新常量)400 拒绝(与 `MaxImageN` 同风格)
- 视频/音频:扫描 `metadata["content"]` 数组的 `video_url`/`audio_url` 条目(豆包 shape 通用化),兼容顶层 `metadata["video_url"]`/`["audio_url"]`
- 每条素材记录:URL 或内联、请求显式时长(若有,钳制)

**时长解析**(新 `service/` 编排 + 新媒体头探测包):

优先级:请求显式 → URL 头探测 → 估算。

- URL 探测 `ProbeURL`:HTTP Range 取头/尾片段读媒体头,总预算 2~3s;第一阶段格式:mp4/mov/m4a(moov box 前后探测)、mp3(Xing/帧头)、wav、webm;不支持或失败 → 估算
- SSRF/安全护栏:scheme 白名单(http/https)、DNS 解析后禁私网/环回/链路本地、重定向 ≤ 3、读取字节 ≤ 256KB、总超时、按 URL 哈希缓存探测结果(避免重复探测)、并发上限
- 提交时:预算内同步探测;探测不到则后台 goroutine 长预算补探测写缓存,供结算修正
- 结算时:优先级 缓存真值 > 提交时值 > 估算;素材费重算修正,走 `QuotaRoundChecked` 并留审计标记
- 经本系统资产管理上传、元数据已知的素材直接用已知真值,不探测

**计费链路**(遵守 AGENTS.md 计费安全不变量):

- `PriceData` 新增素材清单与 `MaterialPrice`;`TaskBillingContext` 新增 `InputMaterials`(清单+计价快照)与 `MaterialQuota`
- 素材费 = Σ(图:n × 每张价;视频/音频:时长 × 每秒价) × GroupRatio;转换走 `QuotaFromFloatChecked`
- 预扣 = 生成预扣 + 素材费;不足照常 insufficient-quota fail
- 结算:素材清单提交时已定,**素材费为固定项冻结**,`settleVideoSecondBilling` 只调生成部分;终值 = 生成结算 + MaterialQuota(修正时更新并审计)
- 双计防护:素材价配置生效时跳过豆包 `video_input` 倍率(与 `applyVideoSecondPricing` 覆盖 seconds 同手法);未配置维持旧行为
- 日志:`other` 记录素材清单、每条 resolved 时长与来源(inline_measured/url_probed/estimated/settle_corrected);钳制经 `attachQuotaSaturation`

### 3.2 固定价格全面分组独立定价(需求 1 附加确认)

- `model_group_price` 新增 `video_second_price *float64` 可空列;分别定价模式每组独立秒价,统一模式维持 全局秒价 × GroupRatio;nil 语义与现有 `ModelPrice` 列对齐
- 接入点:`ModelPriceHelperPerCall`/`buildVideoSecondPriceData`(计费)、`resolveCanvasGroupPrice`(目录下发)、`settleVideoSecondBilling`(结算)
- 素材价列随同一迁移落地(两列一次 AutoMigrate,实现分步)
- 现状盘点:按次 `ModelPrice`、分档表 `PriceTiers` 已有分组列 ✅;秒价补齐;模型定价页 tool-prices 等其余固定价键在实现阶段盘点,如属固定价同模式补齐
- 前端:模型管理抽屉分组定价编辑器加"秒价/素材价";模型定价页全局键保持为全局默认值

### 3.3 定价文案打通(需求 2)

- 计费真源唯一:模型定价页(全局 option)+ `model_group_price`;**目录只投影,不改计费逻辑**
- 新增 `GeneratePricingSummary(model, group)`:复用 `resolveCanvasGroupPrice` 解析结果生成文案——分档表逐档列出、秒价 `X/秒`、按次 `X/次`、token 模型按用户端定价页同口径、未定价显式"未定价";含素材价时追加 `输入图 X/张 · 输入视频 X/秒 …`
- 下发:`GetCanvasCatalog` 与 `catalog-overview` 中,`Pricing` 为空 → 自动生成;非空 → 视为手填覆盖;wire 新增 `pricing_source: auto|custom`,UI 显示徽标
- **秒价下发口径修复(客户端审计发现)**:`video_second_price` 路径目前下发原始秒价+分组倍率分离字段,客户端(固定 ratio=1 且忽略该字段)会低估约 3×;改为与档表/按次同口径——**下发已乘分组倍率的终价**,老客户端被顺带修复
- **wire 透传 vendor/tags**(供客户端预置匹配,见 3.9):条目附带 `models` 表 meta 的 vendor 名称与 tags(真源 `models` 表,与说明统一同方向)
- 审核卡/目录行提供 deep-link,直达该模型的定价编辑 sheet
- 已知边界:客户端当前不渲染 `pricing` 自由文本(结构化 group_price 才是用户可见价格),自动文案的主要价值在管理端与新客户端

### 3.4 巡检自动起草(需求 3 进料端)

- 挂接点:`checkAndPersistChannelUpstreamModelUpdates` 自动应用后 `autoAdded > 0` 时
- 每个新增模型:目录无该 remote_id 条目则创建起草条目——`enabled=false`、display_name=模型名、capabilities 按渠道类型映射(视频任务渠道→video_gen,图片渠道→image_gen,未知→留空)、contract 由 `ContractForCapability` 推导(未知留空,条目自然留在"未配置")
- 渠道类型→capability 映射:constant 新表(任务/图片渠道类型枚举),未知渠道不猜
- 同时 models 表缺行的起草 meta 行:`status=0`(现有枚举 1=上线/0=停用,0 即起草态,用户端定价页本就跳过非 1 行;无需新增枚举值)
- 幂等:已存在条目绝不触碰;总开关 option(默认开——起草态 enabled=false 不影响线上)
- 巡检通知追加"画布目录新增起草 N 条"

### 3.5 统一上新工作台(需求 3/5/6,方案 B)

- 新页面(模型管理侧入口),聚合三类待办:
  1. 待补元数据:abilities 有、models 表无(复用 `/api/models/missing` 逻辑)
  2. 待定价:abilities 有但无任何价格配置(ModelPrice/Ratio/Tiers/秒价/素材价全无)
  3. 待上线目录:起草态(enabled=false)目录条目
- 后端:`GET /api/onboarding/overview` 聚合;`POST /api/onboarding/launch`(单/批量,校验已定价,未定价需显式 force + 前端二次确认);`POST /api/onboarding/ignore`(忽略名单,option 存储)
- 每模型一张**审核卡**:来源渠道、三态检查清单、capability/contract 下拉(受支持列表+自动推导)、价格文案自动预览、说明带出 models.Description、deep-link 定价编辑;**字段自动预填自上游 `/api/pricing`(见 3.6),含计费草稿/说明/vendor,管理端仅核对+开闸**
- 操作:**开闸**(meta status 置上线 + 目录 enabled=true)/ 编辑(打开对应抽屉)/ 忽略;支持批量开闸
- 巡检起草条目自动进入第三列;开闸后从待办消失

### 3.6 上游定价/元数据自动拉取预填(工作台提效关键)

**现状**:`controller/ratio_sync.go` `FetchUpstreamRatios` 已支持 4 种上游格式(type1 `/api/ratio_config`、**type2 `/api/pricing`**、type3 OpenRouter、type4 models.dev),type2 已提取 model_name/quota_type/model_ratio/model_price/completion_ratio/cache/image/audio/billing_expr,并具备 10MB 限读、3 次重试、并发上限等护栏;但仅被"上游倍率同步"页手动使用,且未解析新版 `/api/pricing` 已有的 `video_second_price`/`price_tiers`/`description`/vendor 等字段。

**设计**:

- 把 ratio_sync 的上游拉取机制抽为可复用服务(保留按模型原始条目的形态,不折叠成倍率 map),type2 解析扩展:`video_second_price`、`price_tiers`(经 `NormalizePriceTierList` 校验)、`description`、vendor/tags/icon、`enable_groups`
- 工作台审核卡**自动预填**:渠道有 base_url 时拉取该上游 `/api/pricing`,按模型名匹配——计费草稿(quota_type→按次价或倍率组、秒价、分档表、tiered_expr)、说明草稿(→ models.Description 真源)、vendor/图标草稿;卡片标注来源"来自上游 ××"
- 触发时机:(a) 巡检起草时顺带拉取(超时受限、失败不阻塞起草);(b) 工作台卡片"重新拉取"按钮;(c) 新增渠道后的首次巡检自然覆盖
- **草稿不落库**:预填数据实时拉取 + 短缓存(内存/Redis TTL),仅在开闸时写入正式配置,避免草稿态一致性管理
- **上游数据不可信**:数值一律过现有价格边界(MaxTierPrice 同级)、tier 表规范化、billing_expr 编译冒烟(复用保存时校验);沿用 ratio_sync 的 confidence 哨兵(37.5/1.0)标记可疑数据;外呼护栏沿用 ratio_sync 既有实现
- **对称输出**:本站 `/api/pricing` 在素材计费上线后同步暴露 `input_material_prices` 与素材时长语义,使下游中转站也能预填(与本项目保护 new-api 品牌的方向一致)

### 3.7 说明统一 + 保存 bug 修复(需求 4)

- Bug 修复:目录编辑对话框打开时先 `GET /api/canvas/admin/models/:id` 拉全量再渲染——一次性杜绝 description/pricing/limitations/param_schema/schema_override/requires_vocab/sort_order 被空值覆盖
- 真源统一为 `models.Description`(用户端定价页已在用):目录表单说明区改只读带出 + "去模型管理页编辑"链接;wire 下发优先 meta.Description,无 meta 行回退目录存量列(列保留仅兼容旧数据);目录 CRUD 不再接受 description 写入(入参忽略,保持旧客户端兼容)

### 3.8 目录表单简化(需求 3 表单端)

- 三区布局:**基础**(remote_id/display_name/capability 下拉/contract 下拉(受支持合约列表,自动推导,保留支持率提示)/enabled 开关)→ **计费**(自动文案预览 + 可覆盖手填 + 定价 deep-link)→ **高级折叠**(param_schema、schema_override 可视化编辑器、requires_vocab、sort_order、limitations)
- 每个字段带 tooltip:设置方法 + 影响
- 审核卡 = 基础区的精简投影,与工作台一致
- 前端 `CONTRACT_BY_CAPABILITY` 改为取自后端(新只读接口或复用现有配置下发),消除双源
- 清理死代码:`canvas-catalog-table.tsx`、`useCanvasCatalogModels`

### 3.9 画布客户端(myhuabua)审计结论与修复项

2026-09-04 对客户端(独立仓库 `myhuabua`,Tauri + React)做了三链路审计:目录落地 **可用**、上下架 **半成品**、计费一致 **部分**。以下为客户端侧工作项(独立计划,不阻塞服务端各阶段):

1. **预置供应商自动匹配(新需求)**:目录条目先与客户端已预置的全部供应商模型匹配——`remote_id`/模型名精确匹配 → 归一化匹配(去厂商前缀/大小写/分隔符)+ 服务端新增的 vendor/tags 辅助;命中 → 模型归属该预置供应商、参数 schema 以中转站下发的非空字段优先覆盖预置同名字段;全未命中 → 维持现状落到 `builtin-kungai` 按 `param_schema`/`schema_override` 建档
2. **删除对账(高优)**:同步 payload 中缺失的 remote_id 增加墓碑清理(删本地 `relay-*` 模型与克隆 profile),替代现状"被删模型无限存活";缩量保护(0 或腰斩拒收)保留
3. **同步后刷新**(高优):`catalog://synced` 后重载 `providersStore`,已打开画布立即可见新模型
4. **commercial 构建参数表单**(高优核实):`endpoint_profile_list*` 命令被编译掉但 `useEndpointProfile` 仍调用 → profile=null、表单为空;核实并修复(放行命令或补 provisioning 路径)
5. **秒价口径适配核验**:服务端改为下发终价后,客户端 `preflight.ts` 的 `group_ratio=1` 行为恰好正确;移除误导性注释,`group_ratio_applied` 字段消费决策(展示"已含分组倍率"或忽略)
6. 低优:`pricing` 文本渲染(挂 3.3 自动文案)、`schema_vocab` 消费、`catalog_version` 语义修正
7. 已核对无需改:软下线分类、ETag/304、合约上报、设备绑定、按次/档表预估

## 4. 实施阶段(可独立交付、独立验证)

1. **修复层**:编辑覆盖 bug + 说明统一 + 表单三区简化 + 死代码清理(零计费风险,先行)
2. **素材计费**:类型/存储迁移(与 3 的两列一次迁移)+ 检测 + 时长探测 + 计费链 + 双计防护 + 测试
3. **固定价分组化**:秒价分组列接入解析/结算 + 前端编辑器
4. **定价文案**:自动生成 + wire/pricing_source + 徽标 + deep-link
5. **上新工作台**:巡检起草 + 聚合/开闸/忽略接口 + 工作台页面 + 模型管理页"全部起草" + 上游 `/api/pricing` 拉取预填(ratio_sync 服务化复用)
6. **客户端修复(myhuabua 仓库,独立计划)**:3.9 所列 1–5 项,其中删除对账/同步刷新/commercial 表单为高优;与服务端阶段 4 的秒价口径修复配对验证

## 5. 测试策略

- 检测:content[] 各形态、Images 计数、上限 400、显式时长钳制
- 探测:各格式固定字节 fixture 解析、Range 失败/超时回退、SSRF 拒绝(私网/非 https scheme)、缓存命中
- 计费:图片张数、视频真值/估算/结算修正三分支、分组独立秒价与素材价、豆包双计防护、素材费冻结 + 修正审计、溢出钳制(quota_math 既有风格)
- 巡检起草:幂等、未知能力留空、开关关闭不写
- 工作台:三列聚合正确性、开闸校验(未定价需 force)、忽略名单
- 上游预填:type2 新字段解析、非法 tier 表/价格拒绝、confidence 哨兵、拉取失败不阻塞起草
- 数据库:SQLite/MySQL/PostgreSQL 三库 AutoMigrate 新列
- 前端:表单分区、徽标、工作台卡片交互(遵循 `web/AGENTS.md`)

## 6. 计费安全不变量对照(AGENTS.md)

- 素材数量上限 400 拒绝;显式/探测/默认时长一律钳 `MaxTaskDurationSeconds`;素材价格上限与 `MaxTierPrice` 同级
- 全链 `QuotaFromFloatChecked`/`QuotaRoundChecked` + `attachQuotaSaturation` 审计;不直写 OtherRatios;预扣不足必 fail;结算差额含素材冻结项
- 用户可控 URL 探测全套 SSRF 护栏;素材计价快照入 TaskBillingContext,结算不重查活表(与 TierSnapshot 同原则)
