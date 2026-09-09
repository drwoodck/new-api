# 模型显示名称功能

## 功能概述

在模型元信息编辑页面增加"模型显示名称"字段，画布模型目录自动读取该字段作为显示名称。

## 需求背景

- 模型名称（`model_name`）：用于计费和 API 调用的唯一标识符（如 `gpt-4-turbo`）
- 显示名称（`display_name`）：用户友好的展示名称（如 `GPT-4 Turbo`）
- 画布模型目录需要显示友好的模型名称，而非技术标识符

## 实现方案

### 1. 数据库层

**新增字段**：`models` 表添加 `display_name` 列

```sql
ALTER TABLE models ADD COLUMN display_name VARCHAR(256);
```

**字段说明**：
- 类型：`VARCHAR(256)`
- 可选字段，允许为空
- 空值时回退到 `model_name` 或目录存量 `display_name`

### 2. 后端修改

#### model/model_meta.go

**Model 结构体**：
```go
type Model struct {
    Id           int            `json:"id"`
    ModelName    string         `json:"model_name" gorm:"size:128;not null;uniqueIndex:uk_model_name_delete_at,priority:1"`
    DisplayName  string         `json:"display_name,omitempty" gorm:"type:varchar(256)"`
    Description  string         `json:"description,omitempty" gorm:"type:text"`
    // ... 其他字段
}
```

**Update 方法**：添加 `display_name` 到白名单
```go
func (mi *Model) Update() error {
    mi.UpdatedTime = common.GetTimestamp()
    return DB.Model(&Model{}).Where("id = ?", mi.Id).
        Select("model_name", "display_name", "description", "icon", "tags", "vendor_id", "endpoints", "status", "sync_official", "name_rule", "group_pricing_enabled", "updated_time").
        Updates(mi).Error
}
```

#### model/canvas_catalog.go

**新增批量查询函数**：
```go
// GetModelMetaDisplayNameMap 按模型名批量取 models 表的显示名称。
// 用于画布模型目录自动读取模型显示名称。
// 空显示名称不入 map,调用方据此回退 remote_id 作为显示名称。
func GetModelMetaDisplayNameMap(modelNames []string) (map[string]string, error) {
    out := make(map[string]string, len(modelNames))
    if len(modelNames) == 0 {
        return out, nil
    }
    var rows []Model
    if err := DB.Select("model_name", "display_name").
        Where("model_name IN ?", modelNames).Find(&rows).Error; err != nil {
        return nil, err
    }
    for _, r := range rows {
        if r.DisplayName != "" {
            out[r.ModelName] = r.DisplayName
        }
    }
    return out, nil
}
```

#### controller/canvas_catalog.go

**批量查询显示名称**：
```go
metaDisplayNames, err := model.GetModelMetaDisplayNameMap(remoteIDs)
if err != nil {
    common.SysError(fmt.Sprintf("读取模型显示名称失败,目录显示名称回退 remote_id: %v", err))
    metaDisplayNames = map[string]string{}
}
```

**toWireModel 函数**：
```go
func toWireModel(m *model.CanvasCatalogModel, groupModels map[string]struct{}, group string, metaDescriptions map[string]string, metaDisplayNames map[string]string, groupPrices map[string]model.ModelGroupPrice) canvasCatalogWireModel {
    w := canvasCatalogWireModel{
        RemoteID:      m.RemoteID,
        DisplayName:   m.DisplayName,
        // ... 其他字段
    }
    
    // 显示名称统一真源：优先 models 表的 display_name，没有该行或显示名称为空时
    // 回退目录的 display_name（存量兼容）
    if displayName := metaDisplayNames[m.RemoteID]; displayName != "" {
        w.DisplayName = displayName
    }
    
    // ... 其他逻辑
}
```

### 3. 前端修改

#### web/src/features/models/types.ts

**Model 接口**：
```typescript
export interface Model {
  id: number
  model_name: string
  display_name?: string
  description?: string
  // ... 其他字段
}
```

#### web/src/features/models/components/drawers/model-mutate-drawer.tsx

**表单 Schema**：
```typescript
const extendedModelFormSchema = z.object({
  id: z.number().optional(),
  model_name: z.string().min(1, 'Model name is required'),
  display_name: z.string(),
  description: z.string(),
  // ... 其他字段
})
```

**默认值**：
```typescript
defaultValues: {
  model_name: '',
  display_name: '',
  description: '',
  // ... 其他字段
}
```

**表单字段**：
```tsx
<FormField
  control={form.control}
  name='display_name'
  render={({ field }) => (
    <FormItem>
      <FormLabel>{t('Display Name')}</FormLabel>
      <FormControl>
        <Input
          placeholder={t('GPT-4, Claude 3 Opus, etc.')}
          {...field}
        />
      </FormControl>
      <FormDescription>
        {t('Friendly name shown in Canvas catalog')}
      </FormDescription>
      <FormMessage />
    </FormItem>
  )}
/>
```

## 数据流

### 1. 写入流程

```
用户编辑模型
  ↓
输入 display_name 字段
  ↓
提交表单
  ↓
createModel/updateModel API
  ↓
写入 models.display_name 列
```

### 2. 读取流程（画布目录）

```
GetCanvasCatalog 端点
  ↓
批量读取 remoteIDs
  ↓
GetModelMetaDisplayNameMap(remoteIDs)
  ↓
SELECT model_name, display_name FROM models WHERE model_name IN (...)
  ↓
构建 map[string]string (model_name -> display_name)
  ↓
toWireModel 逐条填充：
  ├─ 优先使用 models.display_name
  └─ 回退目录存量 canvas_catalog_models.display_name
  ↓
下发到画布客户端
```

## 兜底逻辑

### 显示名称优先级

1. **models.display_name**（最高优先级）
   - 管理员在元信息编辑页面设置的显示名称
   
2. **canvas_catalog_models.display_name**（存量兼容）
   - 画布目录原有的显示名称
   - 用于向后兼容，未迁移的旧数据
   
3. **remote_id**（最终兜底）
   - 模型的技术标识符
   - 当前两者都为空时使用

### 错误处理

- **查询失败**：按 fail-open 处理
  - 记录 `SysError` 日志
  - 返回空 map
  - 自然回退到目录存量或 `remote_id`
  - 不让一次查询故障弄垮整份目录

## 数据库迁移

### 自动迁移

GORM AutoMigrate 会自动添加新列：

```go
// main.go 或 model/main.go 的初始化代码
db.AutoMigrate(&model.Model{})
```

### 手动迁移（可选）

如果不使用 AutoMigrate，可以手动执行：

```sql
-- SQLite
ALTER TABLE models ADD COLUMN display_name TEXT;

-- MySQL
ALTER TABLE models ADD COLUMN display_name VARCHAR(256);

-- PostgreSQL
ALTER TABLE models ADD COLUMN display_name VARCHAR(256);
```

## 验证

### 1. 后端验证

```bash
# 编译验证
go build -o /dev/null ./model
go build -o /dev/null ./controller
```

### 2. 前端验证

```bash
cd web
npm run build
```

### 3. 功能验证

**测试步骤**：

1. **编辑模型**：
   - 访问"管理员-模型-元信息"页面
   - 点击编辑某个模型
   - 填写"显示名称"字段（如 `GPT-4 Turbo`）
   - 保存

2. **验证存储**：
   ```sql
   SELECT model_name, display_name FROM models WHERE model_name = 'gpt-4-turbo';
   ```
   预期结果：`display_name` 列显示 `GPT-4 Turbo`

3. **验证画布目录**：
   - 访问画布模型目录 API：`GET /api/canvas/catalog`
   - 检查响应中的 `display_name` 字段
   - 预期：显示 `GPT-4 Turbo`（而非 `gpt-4-turbo`）

## 影响范围

### 受影响功能

- ✅ 模型元信息编辑页面（新增字段）
- ✅ 画布模型目录 API（自动读取显示名称）

### 不受影响功能

- ✅ 模型计费：仍使用 `model_name`
- ✅ API 调用：仍使用 `model_name`
- ✅ 渠道绑定：仍使用 `model_name`
- ✅ 其他页面显示：不影响

## 向后兼容性

### 数据兼容

- **新字段可选**：`display_name` 允许为空
- **存量数据**：无需迁移，空值自动回退
- **画布目录**：优先使用新字段，回退旧字段

### API 兼容

- **请求**：`display_name` 为可选字段
- **响应**：添加新字段，不影响现有字段
- **画布客户端**：serde 忽略未知字段，向后兼容

## 最佳实践

### 显示名称命名规范

- **技术标识符** (`model_name`)：小写，连字符分隔（如 `gpt-4-turbo`）
- **显示名称** (`display_name`)：标题大小写，空格分隔（如 `GPT-4 Turbo`）

### 示例

| model_name | display_name |
|-----------|-------------|
| `gpt-4-turbo` | `GPT-4 Turbo` |
| `claude-3-opus-20240229` | `Claude 3 Opus` |
| `gemini-1.5-pro` | `Gemini 1.5 Pro` |
| `grok-beta` | `Grok Beta` |

## 相关文件

### 后端

- `model/model_meta.go` - Model 结构体和更新方法
- `model/canvas_catalog.go` - 批量查询显示名称
- `controller/canvas_catalog.go` - 画布目录端点

### 前端

- `web/src/features/models/types.ts` - Model 接口定义
- `web/src/features/models/components/drawers/model-mutate-drawer.tsx` - 编辑表单

### 数据库

- `models` 表 - 新增 `display_name` 列

## 注意事项

1. **数据库迁移**：首次启动时 GORM 会自动添加新列
2. **空值处理**：空显示名称会自动回退，无需担心显示问题
3. **批量更新**：如需批量设置显示名称，可编写迁移脚本
4. **性能影响**：批量查询使用 `IN` 子句，查询效率高

## 后续优化建议

1. **批量编辑**：支持批量设置显示名称
2. **自动生成**：根据 `model_name` 自动生成友好的 `display_name`
3. **国际化**：支持多语言显示名称
4. **同步工具**：从上游供应商同步官方显示名称
