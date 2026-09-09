# 价格列显示格式修复

## 问题描述

在"管理员-模型-元信息"页面，价格列显示格式不一致：
- 有的模型显示 `$0.0001`（按次计费）
- 有的模型显示 `15×`（按 token 计费，单一倍率）
- **有的模型显示 `37.5/1×`（应该显示但实际显示为 `37.5×`）**

期望：当 `completionRatio` 与 `modelRatio` 不同时，应该显示 `prompt/completion×` 格式。

## 根因分析

### 问题代码

在 `web/src/features/models/components/models-columns.tsx:277-280`：

```tsx
const ratioText = completionRatio && completionRatio !== modelRatio
  ? `${modelRatio}/${completionRatio}`
  : `${modelRatio}`
```

### Bug 详解

**JavaScript falsy 值陷阱**：
- `completionRatio && completionRatio !== modelRatio` 中的 `&&` 会短路求值
- 当 `completionRatio = 0` 时，`0 && ...` 结果为 `0`（falsy）
- 条件判断错误地走向 else 分支，只显示 `modelRatio`

**示例场景**：
```
modelRatio = 37.5
completionRatio = 0  // 有效值：completion token 不计费

当前逻辑：
  completionRatio && completionRatio !== modelRatio
  → 0 && (0 !== 37.5)
  → 0  // falsy
  → 走 else 分支
  → 显示 "37.5×"

正确逻辑应该：
  completionRatio != null && completionRatio !== modelRatio
  → true && true
  → true
  → 走 if 分支
  → 显示 "37.5/0×"
```

### 为什么 0 是有效值？

在定价系统中，`completionRatio = 0` 表示：
- Completion tokens 不计费（免费）
- 或者该模型只对 prompt tokens 计费

这是完全合法的配置，应该正确显示。

## 修复方案

**核心改动**：将 falsy 检查 `completionRatio &&` 改为 null 检查 `completionRatio != null`

```tsx
// 修改前（错误）
const ratioText = completionRatio && completionRatio !== modelRatio
  ? `${modelRatio}/${completionRatio}`
  : `${modelRatio}`

// 修改后（正确）
const ratioText =
  completionRatio != null && completionRatio !== modelRatio
    ? `${modelRatio}/${completionRatio}`
    : `${modelRatio}`
```

**为什么用 `!= null`**：
- `!= null` 会同时过滤 `null` 和 `undefined`
- 但保留 `0`、`false`、`""` 等有效值
- 这是 JavaScript 中检查"是否存在"的推荐模式

## 显示逻辑说明

### 价格显示优先级

```
1. 按次计费（modelPrice > 0）
   → 显示 "$0.0001"

2. 按 token 计费（modelRatio > 0）
   ├─ completionRatio 存在且与 modelRatio 不同
   │  → 显示 "15/30×"（prompt/completion）
   │
   └─ completionRatio 不存在或等于 modelRatio
      → 显示 "15×"（统一倍率）

3. 无价格配置
   → 显示 "-"
```

### 显示格式示例

| modelPrice | modelRatio | completionRatio | 显示结果 | 说明 |
|-----------|-----------|----------------|---------|------|
| 0.05 | - | - | `$0.0500` | 按次计费 |
| - | 15 | 30 | `15/30×` | prompt 15倍，completion 30倍 |
| - | 37.5 | 0 | `37.5/0×` | prompt 计费，completion 免费 |
| - | 15 | 15 | `15×` | 统一倍率 |
| - | 15 | null | `15×` | 未配置 completion，默认与 prompt 相同 |
| - | 0 | - | `-` | 无价格配置 |

## 技术要点

### JavaScript 中的 falsy 值

Falsy 值（在布尔上下文中被视为 false）：
- `false`
- `0`
- `""` (空字符串)
- `null`
- `undefined`
- `NaN`

**陷阱**：
```tsx
// ❌ 错误：0 会被当作 false
if (value && value !== other) { ... }

// ✅ 正确：明确检查 null/undefined
if (value != null && value !== other) { ... }

// ✅ 正确：明确检查数字类型
if (typeof value === 'number' && value !== other) { ... }
```

### 为什么不用 `!== null && !== undefined`？

```tsx
// 冗余写法
if (value !== null && value !== undefined) { ... }

// 简洁等价写法（推荐）
if (value != null) { ... }
```

**注意**：`!=` (loose equality) 在这里是**故意使用**的：
- `value != null` 同时检查 `null` 和 `undefined`
- `value !== null` 只检查 `null`，不检查 `undefined`

## 验证

### 构建验证
```bash
cd web && npm run build
```
✅ 构建成功

### 测试场景

创建或编辑模型，设置不同的价格配置：

| 场景 | 配置 | 期望显示 | 验证 |
|-----|------|---------|------|
| 按次计费 | modelPrice = 0.05 | `$0.0500` | ✅ |
| 统一倍率 | modelRatio = 15, completionRatio = 15 | `15×` | ✅ |
| 不同倍率 | modelRatio = 15, completionRatio = 30 | `15/30×` | ✅ |
| Completion 免费 | modelRatio = 37.5, completionRatio = 0 | `37.5/0×` | ✅ 修复前显示 `37.5×` |
| 未配置 completion | modelRatio = 15, completionRatio = null | `15×` | ✅ |

## 影响范围

### 受影响组件
- `web/src/features/models/components/models-columns.tsx` - 模型元信息价格列

### 不受影响功能
- ✅ 计费系统：使用后端逻辑，不受前端显示影响
- ✅ 其他价格显示位置：如定价页面、渠道页面等

## 相关问题

### 为什么会出现 completionRatio = 0？

1. **模型只计 prompt tokens**：某些模型（如 embedding 模型）只对输入计费
2. **特殊定价策略**：某些供应商的 completion tokens 免费
3. **未配置时的默认值**：`GetCompletionRatio()` 找不到配置时返回 `0`

### 其他地方有类似问题吗？

已搜索整个前端代码库，确认：
- 其他价格显示位置使用不同的逻辑
- 或者明确处理了 `0` 值的情况
- 此问题仅存在于模型元信息的价格列

## 最佳实践

### 前端数值检查

```tsx
// ❌ 避免：用 && 检查数值是否存在
if (count && count > 0) { ... }  // count=0 会被误判为不存在

// ✅ 推荐：明确检查类型或 null
if (count != null && count > 0) { ... }
if (typeof count === 'number' && count > 0) { ... }

// ❌ 避免：用 || 设置默认值
const value = input || 10  // input=0 会被替换为 10

// ✅ 推荐：用 ?? (nullish coalescing)
const value = input ?? 10  // 只有 null/undefined 才使用默认值
```

### TypeScript 类型定义

在类型定义中明确可选性：

```tsx
interface Model {
  model_price?: number      // 可选：可能不存在
  model_ratio?: number      // 可选：可能不存在
  completion_ratio?: number // 可选：可能不存在
}

// 使用时：
if (model.completion_ratio != null) {
  // 这里 completion_ratio 可能是 0，也可能是其他数字
}
```

## 相关提交

- `web/src/features/models/components/models-columns.tsx`
  - 修复价格列的 completionRatio 判断逻辑
  - 将 `completionRatio &&` 改为 `completionRatio != null`
