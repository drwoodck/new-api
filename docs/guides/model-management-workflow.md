# 模型管理工作流指南

**版本**: v1.0  
**更新日期**: 2026-09-07

---

## 概述

mynewapi 中转站的模型管理分为三个核心页面，各司其职：

1. **管理员-模型-元信息** - 管理所有模型的元数据
2. **系统管理-模型与路由-画布模型目录** - 配置画布客户端可见的模型
3. **管理员-模型-上新工作台** - 审核并开闸新模型

---

## 模型管理三步走

### 第一步：发现新模型（元信息页面）

**路由**: `/models/metadata`

#### 自动发现
- 添加渠道后，系统每 10 分钟自动巡检
- 新模型自动创建到 `models` 表（status=0 停用）
- 创建 < 24小时的模型显示 **新** 徽章

#### 手动同步
- 点击顶部 **Sync Upstream** 按钮
- 选择同步源（官方/社区）和语言（中文/英文）
- 批量同步官方模型信息（描述、分类等）

#### 配置元信息
- 为新模型填写描述（description）
- 设置分类和标签
- 点击 **去配置画布目录** 按钮跳转到画布目录页面

---

### 第二步：配置画布目录（画布模型目录页面）

**路由**: `/system-settings/models/canvas-catalog`

#### 从元信息页面跳转
- 点击模型行的"去配置画布目录"按钮
- 自动打开编辑对话框，预填模型名称

#### 配置内容
1. **基本信息**
   - 画布显示名（display_name）
   - 能力标签（capabilities: text_gen, video_gen, image_gen）
   - 契约模板（contract）

2. **计费配置**
   - 秒价/档表（pricing）
   - 分组定价（group_prices）
   - 价格来源标注（pricing_source: upstream/manual/default/unset）

3. **高级配置**
   - 参数表单（param_schema, schema_override）
   - 限制说明（limitations）
   - 词表要求（requires_vocab）
   - 排序（sort_order）

#### 页面功能
- **已配置完成**：enabled=true 的模型，客户端已可见
- **待补全配置**：enabled=false 的模型，待配置后开闸

---

### 第三步：开闸上架（上新工作台页面）

**路由**: `/models/onboarding`

#### 三列审核流

##### 1. 待补元
- canvas_catalog 有但 models 无或 status=0
- **操作**: 点击跳转到元信息页面配置

##### 2. 待定价
- 有元信息但 canvas_catalog 无 group_prices
- **操作**: 点击跳转到画布目录页面配置定价

##### 3. 已起草
- 元信息 + 定价都配好但 enabled=false
- **操作**: 批量开闸（Launch）

#### 批量开闸逻辑
开闸后：
- `canvas_catalog_models.enabled = true` （画布客户端可见）
- `models.status = 1` （中转站标记为启用）
- 自动从"已起草"列移除

#### 其他功能
- **从上游同步定价**：一键同步已上线模型的最新定价
- **批量忽略**：标记模型为已处理，暂不上架

---

## 数据流与表职责

### models 表（元信息表）
**职责**: 存储所有模型的元数据，与计费无关

```
models
├── model_name (主键)
├── description (模型描述)
├── status (0=停用, 1=启用, 2=已删除)
├── sync_official (是否从官方同步: 0=手工, 1=官方)
└── created_time, updated_time
```

### canvas_catalog_models 表（画布目录表）
**职责**: 存储画布客户端可见的模型目录，包含所有计费配置

```
canvas_catalog_models
├── remote_id (对应 models.model_name)
├── display_name (画布显示名)
├── capabilities (能力标签)
├── enabled (是否上架到画布客户端)
├── pricing (秒价/档表 JSON)
├── group_prices (分组定价)
├── contract (契约模板)
└── pricing_source (价格来源标注)
```

### 关联约束
- `canvas_catalog_models.remote_id` 必须在 `models` 表存在
- 开闸时同步更新两表的 enabled/status 字段

---

## 常见场景

### 场景 1：添加新渠道后的完整流程

1. **添加渠道**（渠道管理页面）
   - 添加新的 API 渠道（如 OpenAI, Anthropic）

2. **等待自动巡检**（10 分钟周期）
   - 系统自动创建 models 行（status=0）
   - 系统自动创建 canvas_catalog 行（enabled=false）

3. **配置元信息**（元信息页面）
   - 发现"新"徽章标记的模型
   - 填写描述、分类等信息
   - 点击"去配置画布目录"

4. **配置画布目录**（画布目录页面）
   - 自动打开编辑对话框
   - 配置显示名、契约、定价等
   - 保存

5. **开闸上架**（上新工作台页面）
   - 在"已起草"列找到该模型
   - 批量开闸 → enabled=true + status=1
   - 画布客户端立即可见

### 场景 2：从上游同步模型信息

1. **批量同步元信息**（元信息页面）
   - 点击 **Sync Upstream** 按钮
   - 选择同步源和语言
   - 系统自动填充 description 等字段

2. **配置画布目录**（同场景 1 步骤 4）

3. **开闸上架**（同场景 1 步骤 5）

### 场景 3：更新已上线模型的定价

1. **从上游同步定价**（上新工作台页面）
   - 点击"从上游同步定价"按钮
   - 系统自动更新已上线模型的 group_prices
   - pricing_source 标记为 'upstream'

2. **手动调整**（画布目录页面）
   - 编辑模型，修改 group_prices
   - pricing_source 自动标记为 'manual'

---

## 最佳实践

### 1. 新模型标记
- 创建 < 24小时的模型自动显示"新"徽章
- 及时处理新模型，避免积压

### 2. 价格溯源
- 从上游同步的定价标记为 'upstream'
- 手工填写的定价标记为 'manual'
- 未配置的定价标记为 'unset'
- 便于后续审计和调整

### 3. 批量操作
- 使用上新工作台的批量开闸功能
- 一次性处理多个已配置好的模型
- 提高上架效率

### 4. 数据一致性
- 开闸时自动同步 enabled/status
- 避免手动修改数据库
- 使用系统提供的审核流程

---

## 故障排查

### 问题 1：新模型没有显示"新"徽章
**原因**: created_time 字段未正确设置  
**解决**: 检查 DraftCatalogEntries 巡检任务是否正常运行

### 问题 2：点击"去配置画布目录"后对话框未打开
**原因**: URL 参数未正确传递  
**解决**: 检查浏览器控制台错误，确认路由配置正确

### 问题 3：开闸后画布客户端看不到模型
**原因**: enabled=true 但 status=0  
**解决**: 使用上新工作台的批量开闸功能，确保两表同步更新

### 问题 4：上新工作台列表不滚动
**原因**: CSS overflow 属性未设置  
**解决**: 已在 2026-09-07 修复，升级到最新版本

---

## API 参考

### 元信息页面相关

#### 获取模型列表
```
GET /api/models?p=1&page_size=20
```

#### 批量同步上游
```
POST /api/onboarding/sync_from_upstream
Body: { locale: 'zh', source: 'official' }
```

### 画布目录页面相关

#### 获取目录概览
```
GET /api/canvas/admin/catalog-overview
```

#### 创建/更新目录条目
```
POST /api/canvas/admin/models
PUT /api/canvas/admin/models/:id
```

### 上新工作台相关

#### 获取工作台概览
```
GET /api/onboarding/overview
```

#### 批量开闸
```
POST /api/onboarding/launch
Body: { model_names: ['gpt-4', 'claude-3-5-sonnet'] }
```

---

## 更新日志

### 2026-09-07
- ✅ 修复上新工作台滚动问题
- ✅ 元信息页面添加"新"徽章
- ✅ 元信息页面添加"去配置画布目录"按钮
- ✅ 画布目录页面添加 URL 预填支持
- ✅ 完善模型管理三步走流程

---

**文档版本**: v1.0  
**维护者**: QuantumNous Team  
**反馈**: support@quantumnous.com
