# mynewapi 中转站模型管理表逻辑分析与优化方案

**分析时间**: 2026-09-07  
**问题描述**: 管理员-模型-元信息、上新工作台、系统管理-模型与路由-画布模型目录等表的管理逻辑感觉混乱，需要梳理并优化

---

## 一、当前表结构与功能定位

### 1.1 涉及的核心表

| 表名 | 前端页面 | 路由 | 核心功能 | 权限 |
|------|---------|------|---------|------|
| **models** (元信息) | 管理员-模型-元信息 | `/models/metadata` | 存储所有模型的元数据(model_name, status, sync_official等) | Admin |
| **canvas_catalog_models** (画布目录) | 系统管理-模型与路由-画布模型目录 | `/system-settings/models/canvas-catalog` | 存储画布客户端可见的模型目录(remote_id, display_name, capabilities, enabled, pricing等) | Admin |
| **上新工作台** (虚拟视图) | 管理员-模型-上新工作台 | `/models/onboarding` | 聚合视图：从 models + canvas_catalog_models + channels 聚合出待补元/待定价/已起草三列 | Admin |

### 1.2 数据流与生命周期

```
[管理员添加渠道] → [渠道新增模型]
         ↓
    DraftCatalogEntries (自动巡检，10分钟周期)
         ↓
    同时创建两条记录：
    1. models 表：status=0(停用), sync_official=1
    2. canvas_catalog_models 表：enabled=false(未上架), capabilities/contract 按渠道类型推导
         ↓
    [上新工作台显示]
    - 待补元：canvas_catalog 有，但 models 无或 status=0
    - 待定价：有元信息，但 canvas_catalog 无 group_prices
    - 已起草：已配置但未开闸(enabled=false)
         ↓
    [管理员配置]
    1. 元信息页面：配置 models 表(status, description等)
    2. 画布模型目录页面：配置 canvas_catalog_models(pricing, contract等)
    3. 上新工作台：批量开闸(launch)或忽略(ignore)
         ↓
    [Launch 开闸]
    - canvas_catalog_models.enabled = true
    - models.status = 1 (若该模型的 status 仍为 0)
         ↓
    [画布客户端可见]
```

---

## 二、当前存在的问题

### 2.1 功能重叠与职责不清

| 问题 | 具体表现 | 影响 |
|------|---------|------|
| **两张表职责不清** | models 表和 canvas_catalog_models 表都存储模型信息，边界模糊 | 管理员不知道在哪里配置什么字段 |
| **元信息页面功能薄弱** | 元信息页面只展示 models 表，但缺少批量操作、与渠道关联不明确 | 管理员添加渠道后，看不到新模型提示 |
| **上新工作台与画布目录重复** | 上新工作台显示"已起草"列，画布目录也显示"未配置"，两者都是 enabled=false 的模型 | 两个地方看到同样的数据，不知道在哪里操作 |
| **上游同步入口隐藏** | `/api/onboarding/prefetch` 和 `/sync_from_upstream` 只在上新工作台，但功能是填充 models 表 | 管理员不知道如何从上游同步模型信息 |

### 2.2 UI/UX 问题

| 问题 | 具体表现 |
|------|---------|
| **上新工作台不能滚动** | 页面固定高度，三列内容超出时无法滚动查看 |
| **新模型标注缺失** | 管理员添加渠道后，元信息页面没有"新"标记，不知道哪些是刚同步的 |
| **信息不一致** | 元信息页面显示 model_name，画布目录显示 remote_id (实际是同一个值)，display_name 又不同 |
| **配置流程不顺畅** | 需要在三个地方跳转：元信息配 models → 画布目录配 pricing → 上新工作台开闸 |

### 2.3 数据一致性风险

| 问题 | 具体表现 | 风险 |
|------|---------|------|
| **两表 enabled/status 不同步** | canvas_catalog.enabled=true 但 models.status=0，或反过来 | 画布客户端看到模型但中转站标记为停用 |
| **孤儿记录** | canvas_catalog 有记录但 models 没有，或反过来 | Launch 开闸时可能失败或产生脏数据 |
| **pricing_source 溯源不完整** | 虽然 Plan 3 添加了 pricing_source 标注，但上游同步后没有在 UI 显示来源 | 管理员不知道价格是手工填的还是从上游同步的 |

---

## 三、核心设计改进方案

### 3.1 设计原则

1. **单一职责**：models 表只存元数据(不涉及计费)，canvas_catalog_models 表只存画布配置(含计费)
2. **自动化优先**：渠道添加模型后，自动同步元信息 + 自动起草目录条目 + 明确标注"新"
3. **流程顺畅**：从发现新模型 → 配置 → 开闸，一条主线清晰可见
4. **信息一致**：同一个模型在所有页面显示的信息保持一致(model_name = remote_id, display_name 单独显示)

### 3.2 表职责重新定义

#### models 表 (元信息表)
**职责**: 存储所有模型的元数据，与计费无关
- model_name (主键)
- description (模型描述)
- status (0=停用, 1=启用, 2=已删除)
- sync_official (是否从官方同步: 0=手工, 1=官方)
- created_time, updated_time

**不应包含**: 任何计费相关字段(pricing, group_price 等应移除或废弃)

#### canvas_catalog_models 表 (画布目录表)
**职责**: 存储画布客户端可见的模型目录，包含所有计费配置
- remote_id (对应 models.model_name)
- display_name (画布显示名)
- capabilities (能力标签: text_gen, video_gen, image_gen)
- enabled (是否上架到画布客户端)
- pricing (秒价/档表 JSON)
- group_prices (分组定价)
- contract (契约模板)
- param_schema, schema_override (参数表单)
- limitations (限制说明)
- requires_vocab (词表要求)
- sort_order (排序)
- pricing_source (价格来源: upstream/manual/default/unset)

**关联约束**: remote_id 必须在 models 表存在(外键或应用层校验)

### 3.3 页面功能重新划分

#### 3.3.1 管理员-模型-元信息 (增强)

**路由**: `/models/metadata`  
**数据源**: models 表  
**核心功能**:
1. ✅ **自动添加**: 管理员添加渠道后，DraftCatalogEntries 自动创建 models 行 (status=0)
2. ✅ **新模型标注**: 创建时间 < 24小时的模型显示"新"标记
3. ✅ **上游同步入口**: 
   - "从上游预填" 按钮 → `/api/onboarding/prefetch?channel_id=X&model_name=Y`
   - "批量同步官方模型信息" 按钮 → `/api/onboarding/sync_from_upstream` (移到这里，不再藏在上新工作台)
4. ✅ **批量操作**: 批量启用/停用 status
5. ✅ **跳转到配置**: 每行有"去配置画布目录"按钮 → 跳转到画布目录页面并预填 remote_id

**UI 改进**:
- 表格列: model_name | description | status | sync_official | created_time | 操作
- 筛选器: 全部/已启用/已停用/新增(24h内)
- 操作按钮: 编辑 | 去配置画布目录 | 预填上游信息

#### 3.3.2 系统管理-模型与路由-画布模型目录 (保持)

**路由**: `/system-settings/models/canvas-catalog`  
**数据源**: canvas_catalog_models 表 (JOIN models 获取 status)  
**核心功能**:
1. ✅ 配置所有画布相关字段 (pricing, group_prices, contract, param_schema 等)
2. ✅ 编辑/删除目录条目
3. ✅ enabled 开关 (独立控制是否上架到画布客户端)
4. ✅ pricing_source 显示 (标注价格来源: 上游/手工/默认/未设置)

**UI 改进**:
- 分页显示: 已配置(enabled=true) / 未配置(enabled=false)
- 表格列: remote_id | display_name | capabilities | pricing摘要 | pricing_source | enabled | 操作
- 操作按钮: 编辑 | 删除 | 预览价格

**滚动问题修复**: 页面容器改为 `overflow-y: auto`，移除固定高度

#### 3.3.3 管理员-模型-上新工作台 (简化)

**路由**: `/models/onboarding`  
**数据源**: 聚合视图 (`/api/onboarding/overview`)  
**核心功能** (简化为审核流):
1. ✅ **待补元**: canvas_catalog 有但 models 无或 status=0 → 点击跳转到元信息页面配置
2. ✅ **待定价**: 有元信息但 canvas_catalog 无 group_prices → 点击跳转到画布目录页面配置定价
3. ✅ **已起草**: 元信息+定价都配好但 enabled=false → **批量开闸** (Launch)

**批量开闸逻辑** (Launch):
- canvas_catalog_models.enabled = true
- models.status = 1 (若 status=0)
- 开闸后自动从"已起草"列移除

**UI 改进**:
- 三列布局保持，但改为可滚动 (`overflow-y: auto`)
- 每列顶部显示数量，底部显示"批量操作"按钮
- 已起草列: 批量开闸 | 批量忽略

**移除功能**:
- ❌ "从上游同步" 按钮 → 移到元信息页面
- ❌ "预填定价" 按钮 → 移到画布目录页面

---

## 四、实施方案

### 4.1 后端改动

#### 4.1.1 models 表清理 (低优先级，可选)
- 废弃 models 表的 pricing 相关字段 (如果有的话)
- 添加 description, category 等元信息字段 (如果缺失)

#### 4.1.2 DraftCatalogEntries 增强
当前已实现：
- ✅ 自动创建 models 行 (status=0)
- ✅ 自动创建 canvas_catalog 行 (enabled=false)

需要补充：
- ⚠️ 通知机制：巡检完成后，在元信息页面显示"发现 N 个新模型"提示

#### 4.1.3 Launch 逻辑增强
当前已实现 (Plan 3/4):
- ✅ canvas_catalog.enabled = true
- ✅ 分组 flag 继承
- ✅ quota_type 互斥清理
- ✅ Valid 闸门准入

需要补充：
- ✅ models.status = 1 (若 status=0) — **已实现** (Plan 3 T4 代码中 LaunchOnboardingModels 有此逻辑)

#### 4.1.4 上游同步 API 移动
当前路由：
- `/api/onboarding/prefetch` (AdminAuth)
- `/api/onboarding/sync_from_upstream` (RootAuth)

建议：保持路由不变，前端调用入口改为元信息页面

### 4.2 前端改动

#### 4.2.1 元信息页面增强 (`web/src/features/models/index.tsx`)

**新增功能**:
1. "新"标记：created_time < 24h 的模型显示徽章
2. 上游同步按钮：
   - "从上游预填单个模型" → 调用 `/api/onboarding/prefetch`
   - "批量同步官方模型信息" → 调用 `/api/onboarding/sync_from_upstream`
3. "去配置画布目录"按钮：跳转到 `/system-settings/models/canvas-catalog?prefill=<model_name>`

**实现要点**:
```tsx
// 1. 新标记
{createdTime > Date.now() - 86400000 && (
  <Badge variant="success">新</Badge>
)}

// 2. 上游同步按钮 (顶部工具栏)
<Button onClick={() => openPrefetchDialog()}>
  从上游预填
</Button>
<Button onClick={() => openSyncDialog()}>
  批量同步官方模型信息
</Button>

// 3. 跳转按钮 (每行操作列)
<Button 
  variant="ghost" 
  onClick={() => navigate({
    to: '/system-settings/models/canvas-catalog',
    search: { prefill: row.model_name }
  })}
>
  去配置画布目录
</Button>
```

#### 4.2.2 上新工作台滚动修复 (`web/src/features/onboarding/onboarding-workbench.tsx`)

**问题**: 三列布局固定高度，内容超出无法滚动

**修复方案**:
```tsx
// WorkbenchColumn 组件
<div className="flex flex-col gap-2 overflow-y-auto max-h-[calc(100vh-300px)]">
  {children}
</div>
```

或者整体容器改为可滚动：
```tsx
// OnboardingWorkbench 根容器
<div className="grid grid-cols-3 gap-4 overflow-y-auto max-h-[calc(100vh-200px)]">
  <WorkbenchColumn title="待补元" />
  <WorkbenchColumn title="待定价" />
  <WorkbenchColumn title="已起草" />
</div>
```

#### 4.2.3 画布模型目录页面增强 (`web/src/features/system-settings/models/canvas-catalog/canvas-catalog-section.tsx`)

**新增功能**:
1. pricing_source 列显示：upstream(上游) / manual(手工) / default(默认) / unset(未设置)
2. URL 预填支持：`?prefill=<model_name>` 时自动打开编辑对话框

**实现要点**:
```tsx
// 1. pricing_source 显示
<TableCell>
  <Badge variant={pricingSourceVariant(row.pricing_source)}>
    {t(pricingSourceLabel(row.pricing_source))}
  </Badge>
</TableCell>

// 2. URL 预填
const [searchParams] = useSearchParams()
useEffect(() => {
  const prefill = searchParams.get('prefill')
  if (prefill) {
    setPrefillRemoteId(prefill)
    setIsDialogOpen(true)
  }
}, [searchParams])
```

### 4.3 文档更新

#### 4.3.1 管理员手册新增章节

**《模型管理三步走》**:
1. **发现新模型** (自动 + 手工)
   - 添加渠道后，系统自动巡检(10分钟周期)并创建模型记录
   - 元信息页面显示"新"标记，管理员可查看并补充描述
2. **配置画布目录** (画布客户端可见的模型)
   - 从元信息页面点击"去配置画布目录"
   - 配置定价(秒价/档表)、契约模板、参数表单等
   - pricing_source 标注价格来源(上游/手工)
3. **开闸上架** (审核通过后上架)
   - 上新工作台查看"已起草"列
   - 批量开闸 → enabled=true + status=1
   - 画布客户端立即可见

#### 4.3.2 设计文档更新

更新现有设计文档：
- `docs/superpowers/plans/2026-09-07-onboarding-workbench.md` 增加"表职责划分"章节
- 新增 `docs/architecture/model-management-tables.md` 详细说明三张表的职责边界

---

## 五、优先级与工作量评估

### 5.1 优先级分级

| 优先级 | 改动项 | 原因 | 工作量 |
|--------|-------|------|--------|
| **P0 (立即修复)** | 上新工作台滚动问题 | 影响基本可用性 | 0.5h |
| **P1 (本周)** | 元信息页面"新"标记 | 核心痛点：管理员不知道哪些是新模型 | 2h |
| | 元信息页面"去配置画布目录"跳转 | 流程顺畅性 | 1h |
| | 画布目录 pricing_source 显示 | 价格溯源透明度 | 1h |
| **P2 (下周)** | 元信息页面上游同步按钮移动 | UX 改进，但不影响功能 | 3h |
| | 画布目录 URL 预填支持 | 配合跳转功能 | 2h |
| | 文档更新 | 管理员手册 + 设计文档 | 4h |
| **P3 (可选)** | models 表清理 | 历史遗留，不影响当前功能 | 8h |

### 5.2 总工作量
- P0: 0.5小时
- P1: 4小时
- P2: 9小时
- P3: 8小时
- **总计**: 21.5小时 (约 3 个工作日)

---

## 六、风险与缓解

### 6.1 数据一致性风险
**风险**: 改动后可能出现 canvas_catalog.enabled=true 但 models.status=0 的情况  
**缓解**: 
- Launch 逻辑增加双写校验 (已在 Plan 3/4 实现)
- 增加后台巡检任务，每日检查并自动修复不一致记录

### 6.2 向后兼容风险
**风险**: 移动上游同步按钮后，已习惯旧流程的管理员可能找不到  
**缓解**: 
- 保留旧路由，增加前端跳转提示
- 更新文档并在首次登录时显示"功能位置调整"通知

### 6.3 UI 回归风险
**风险**: 滚动修复可能影响其它页面布局  
**缓解**: 
- 只修改 `onboarding-workbench.tsx`，不改全局样式
- 增加 E2E 测试覆盖上新工作台滚动场景

---

## 七、后续优化建议

### 7.1 自动化增强
1. **智能定价推荐**: 根据同类模型的历史定价，自动推荐新模型的 group_prices
2. **批量配置模板**: 管理员可保存"视频生成模型配置模板"，一键应用到多个新模型
3. **开闸审批流**: 大批量开闸(>10个模型)需要二次确认或审批工作流

### 7.2 监控与告警
1. **新模型通知**: 巡检发现新模型后，发送邮件/Webhook 通知管理员
2. **配置完整度监控**: 看板显示"待配置模型数"趋势图
3. **定价异常检测**: 检测 group_prices 中异常高/低的价格，标记为"需人工复核"

### 7.3 用户体验优化
1. **配置向导**: 首次添加渠道后，显示"模型配置向导"，引导管理员完成三步走
2. **批量编辑**: 画布目录页面支持批量修改 capabilities、contract 等字段
3. **预览功能**: 开闸前预览"画布客户端将看到的模型卡片"

---

## 八、实施计划

### Phase 1: 紧急修复 (本周，P0+P1)
- [ ] 修复上新工作台滚动问题 (0.5h)
- [ ] 元信息页面增加"新"标记 (2h)
- [ ] 元信息页面增加"去配置画布目录"跳转 (1h)
- [ ] 画布目录页面显示 pricing_source (1h)
- [ ] 测试验证 (1h)

### Phase 2: 流程优化 (下周，P2)
- [ ] 元信息页面移入上游同步按钮 (3h)
- [ ] 画布目录页面 URL 预填支持 (2h)
- [ ] 更新管理员手册 (2h)
- [ ] 更新设计文档 (2h)
- [ ] E2E 测试补充 (2h)

### Phase 3: 技术债清理 (可选，P3)
- [ ] models 表字段清理 (4h)
- [ ] 数据一致性巡检任务 (4h)
- [ ] 性能优化(索引、缓存) (4h)

---

## 九、总结

### 当前问题核心
1. **职责不清**: models 和 canvas_catalog_models 两表边界模糊
2. **流程割裂**: 配置需要在三个页面跳转，缺少主线引导
3. **信息缺失**: 新模型无标记，价格来源不透明
4. **UI 缺陷**: 上新工作台不能滚动

### 优化核心思路
1. **明确职责**: models = 元数据，canvas_catalog = 画布配置
2. **顺畅流程**: 元信息 → 画布目录 → 上新工作台 (三步走)
3. **透明溯源**: 新标记 + pricing_source 显示
4. **修复缺陷**: 滚动 + 跳转支持

### 预期效果
- 管理员添加渠道后，能立即看到新模型(元信息页面"新"标记)
- 配置流程顺畅(元信息 → 一键跳转画布目录 → 上新工作台开闸)
- 信息一致透明(pricing_source 显示价格来源)
- UI 可用性提升(上新工作台可滚动)

---

**文档版本**: v1.0  
**作者**: Kiro (Claude Opus 5)  
**审核**: 待用户确认
