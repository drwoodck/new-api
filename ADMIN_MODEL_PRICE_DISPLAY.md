# 中转站管理员模型元信息页面 - 添加分组价格显示

## 修改内容

### 文件
**E:\githubxiangmu\mynewapi\web\src\features\models\components\models-columns.tsx**

### 修改位置
价格列（Price column）的 `cell` 渲染函数

---

## 显示逻辑

### 优先级

1. **分组价格**（group_prices）⭐ 优先
   - 如果 `group_pricing_enabled = true` 且有配置价格的分组
   - 显示第一个分组的价格
   - 如果有多个分组，显示 `(+N)` 标识

2. **统一定价**（回退）
   - `model_price`：固定价格
   - `model_ratio`：倍率

3. **无价格**
   - 显示 `-`

---

## 支持的分组价格格式

### 1. 按次计费
```json
{
  "model_price": 0.05
}
```
**显示**: `$0.0500`

### 2. 按秒计费（视频）
```json
{
  "video_second_price": 0.02
}
```
**显示**: `$0.0200/s`

### 3. 分档价格
```json
{
  "price_tiers": [
    { "price_per_unit": 0.1, "max_value": 1024 },
    { "price_per_unit": 0.2, "max_value": 2048 },
    { "price_per_unit": 0.3, "max_value": 4096 }
  ]
}
```
**显示**: `$0.1000-$0.3000`

### 4. 倍率
```json
{
  "model_ratio": 15,
  "completion_ratio": 30
}
```
**显示**: `15/30×`

---

## 显示示例

### 单个分组价格
```
$0.0500
```

### 多个分组价格
```
$0.0500 (+2)
```
→ 表示有3个分组配置了价格，显示第一个，另外2个折叠

### 统一定价（回退）
```
$0.1234        # model_price
15/30×         # model_ratio/completion_ratio
```

### 无价格
```
-
```

---

## 测试步骤

### 1. 启动开发服务器

```bash
cd E:\githubxiangmu\mynewapi\web
pnpm dev
```

### 2. 访问管理员页面

1. 登录管理员账号
2. 导航到 **模型管理** 页面
3. 查看模型列表的 **Price** 列

### 3. 验证不同场景

#### 场景 1：分组价格模型
- **模型**: 启用了 `group_pricing_enabled` 的模型
- **预期**: 显示 `$X.XXXX` 或 `$X.XXXX (+N)`

#### 场景 2：统一定价模型
- **模型**: 只有 `model_price` 或 `model_ratio`
- **预期**: 显示统一价格或倍率

#### 场景 3：无价格模型
- **模型**: 既无分组价格也无统一价格
- **预期**: 显示 `-`

---

## 代码变更细节

### 变更前
```tsx
cell: ({ row }) => {
  const model = row.original
  const modelPrice = model.model_price
  // ... 只显示统一定价
}
```

### 变更后
```tsx
cell: ({ row }) => {
  const model = row.original

  // 1. 优先显示分组价格
  if (model.group_pricing_enabled && model.group_prices) {
    // 找到配置了价格的分组
    const groupsWithPrice = model.group_prices.filter(gp => {
      return (gp.model_price && gp.model_price > 0) ||
             (gp.video_second_price > 0) ||
             (gp.price_tiers?.length > 0) ||
             (gp.model_ratio > 0)
    })
    
    if (groupsWithPrice.length > 0) {
      // 显示第一个分组价格 + 数量标识
      const firstGroup = groupsWithPrice[0]
      let priceText = formatGroupPrice(firstGroup)
      
      return (
        <div>
          <span>{priceText}</span>
          {groupsWithPrice.length > 1 && (
            <span>(+{groupsWithPrice.length - 1})</span>
          )}
        </div>
      )
    }
  }

  // 2. 回退到统一定价
  const modelPrice = model.model_price
  // ... 原有逻辑
}
```

---

## 与画布项目的对应关系

### 画布项目（myhuabua）
- **文件**: `src/canvas/preflight.ts`
- **函数**: `parseGroupPrice()`
- **用途**: 费用预估时使用分组价格

### 中转站项目（mynewapi）
- **文件**: `web/src/features/models/components/models-columns.tsx`
- **用途**: 管理员页面显示分组价格

**两者逻辑一致**：都是优先使用分组价格，回退到统一定价。

---

## 提交代码

```bash
cd E:\githubxiangmu\mynewapi
git add web/src/features/models/components/models-columns.tsx
git commit -m "feat(admin): display group prices in model list

在管理员模型管理页面的价格列中添加分组价格显示:

1. 优先显示 group_prices（分组价格）
2. 回退到 model_price/model_ratio（统一定价）
3. 支持多种价格格式：
   - 按次计费: \$X.XXXX
   - 按秒计费: \$X.XXXX/s
   - 分档价格: \$X.XXXX-\$Y.YYYY
   - 倍率: X/Y×

4. 多个分组时显示第一个 + 数量标识: \$X.XXXX (+N)

与画布侧费用预估逻辑保持一致。

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

## 截图位置

管理员 → 模型管理 → Price 列

**预期效果**：
```
┌────────┬──────────────┬────────────┬────────┐
│ ID     │ Model        │ Price      │ Status │
├────────┼──────────────┼────────────┼────────┤
│ 123    │ gpt-4        │ $0.0300    │ 启用   │
│ 124    │ claude-3     │ $0.0150 (+2│ 启用   │
│ 125    │ gemini-pro   │ 15/30×     │ 启用   │
│ 126    │ local-model  │ -          │ 禁用   │
└────────┴──────────────┴────────────┴────────┘
```

其中：
- `$0.0300`: 单个分组价格
- `$0.0150 (+2)`: 3个分组，显示第一个
- `15/30×`: 倍率（回退方案）
- `-`: 无价格
