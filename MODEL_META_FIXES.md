# 模型元信息页面修复说明

## 修复的问题

### 1. 渠道添加模型时自动创建元信息记录
**问题描述**: 通过渠道添加的模型只在 `abilities` 表中创建记录，不会自动在 `models` 表中创建对应的元信息记录，导致元信息页面无法显示所有模型。

**解决方案**:
- 修改 `model/ability.go` 中的 `AddAbilities` 和 `UpdateAbilities` 方法
- 添加 `ensureModelsExist` 函数，在添加 abilities 时自动检查并创建缺失的模型元信息记录
- 新创建的模型默认状态为启用 (status=1)，匹配规则为精确匹配 (name_rule=0)

**修改文件**:
- `model/ability.go`: 添加自动创建模型元信息的逻辑

### 2. 显示"说明"列
**问题描述**: `description` 列存在但默认隐藏，用户无法直接看到模型的说明信息。

**解决方案**:
- 将 `description` 列从默认隐藏列表中移除
- 移除列定义中的 `meta: { mobileHidden: true }` 属性

**修改文件**:
- `web/src/features/models/components/models-table.tsx`: 更新 `initialColumnVisibility` 配置
- `web/src/features/models/components/models-columns.tsx`: 移除移动端隐藏属性

### 3. 添加"价格"列
**问题描述**: 元信息页面缺少价格信息列，管理员无法查看模型的定价信息。

**解决方案**:
- 在后端 `Model` 结构体中添加价格相关字段（`model_ratio`, `model_price`, `completion_ratio`）
- 在 `enrichModels` 函数中填充价格信息
- 添加 `GetModelPricingMap` 函数从定价缓存获取价格数据
- 在前端添加"价格"列，显示格式：
  - 如果是按次计费（`model_price > 0`）：显示 `$0.0001` 格式
  - 如果是按 token 计费（`model_ratio > 0`）：显示 `15×` 或 `15/30×`（prompt/completion）格式
  - 如果没有价格信息：显示 `-`

**修改文件**:
- `model/model_meta.go`: 添加价格字段
- `model/pricing.go`: 添加 `GetModelPricingMap` 函数
- `controller/model_meta.go`: 在 `enrichModels` 中填充价格信息
- `web/src/features/models/types.ts`: 添加价格字段类型定义
- `web/src/features/models/components/models-columns.tsx`: 添加价格列定义

## 技术细节

### 后端修改

#### 1. 自动创建模型元信息 (`model/ability.go`)
```go
func ensureModelsExist(db *gorm.DB, modelNames map[string]struct{}) error {
    // 1. 检查哪些模型已存在
    // 2. 批量创建缺失的模型记录
    // 3. 使用 OnConflict{DoNothing: true} 处理并发插入
}
```

#### 2. 价格信息填充 (`controller/model_meta.go`)
```go
func enrichModels(models []*model.Model) {
    // 获取定价映射
    pricingMap := model.GetModelPricingMap()
    
    // 为每个模型填充价格信息
    for _, m := range models {
        if pricing := pricingMap[m.ModelName]; pricing != nil {
            m.ModelRatio = pricing.ModelRatio
            m.ModelPrice = pricing.ModelPrice
            m.CompletionRatio = pricing.CompletionRatio
        }
    }
}
```

### 前端修改

#### 价格列显示逻辑
```typescript
// 按次计费
if (modelPrice && modelPrice > 0) {
  return `$${modelPrice.toFixed(4)}`
}

// 按 token 计费
if (modelRatio && modelRatio > 0) {
  const ratioText = completionRatio && completionRatio !== modelRatio
    ? `${modelRatio}/${completionRatio}`
    : `${modelRatio}`
  return `${ratioText}×`
}

// 无价格信息
return '-'
```

## 验证方法

1. **验证自动创建模型**:
   - 创建或更新渠道，添加新的模型名称
   - 检查"模型-元信息"页面，新模型应该自动出现
   - 检查 `models` 表，应该有对应的记录

2. **验证说明列显示**:
   - 访问"模型-元信息"页面
   - "说明"列应该默认显示
   - 编辑模型添加说明，保存后应该在列表中可见

3. **验证价格列显示**:
   - 访问"模型-元信息"页面
   - "价格"列应该显示在表格中
   - 不同计费类型的模型应该显示不同的价格格式：
     - 按次计费：`$0.0001`
     - 按 token 计费：`15×` 或 `15/30×`
     - 无价格：`-`

## 兼容性说明

- 所有修改都是增量的，不会影响现有数据
- 自动创建模型功能只在添加/更新渠道时触发，不会修改已存在的模型
- 价格信息是运行时字段（`gorm:"-"`），不会修改数据库 schema
- 前端修改向后兼容，旧数据会正常显示（无价格时显示 `-`）

## 后续优化建议

1. 为自动创建的模型填充更多元数据（如从官方数据源同步描述、图标等）
2. 添加批量更新价格的功能
3. 支持在元信息页面直接编辑价格
4. 添加价格历史记录功能
