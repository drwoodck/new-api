# 档位表 500 错误修复报告

## 问题描述

在"管理员-模型-元信息"页面编辑模型时，打开"分组分别定价"中的"档位表"时，页面立即跳转到 500 错误页面。

## 根因分析

### Phase 1: 初步排查

1. **错误类型识别**：用户看到的"500"实际上来自前端路由级错误边界 `GeneralError` 组件的默认显示，而非后端 HTTP 500 响应
2. **触发点**：打开档位表（Collapsible 展开）时渲染 `TierPriceEditor` 组件

### Phase 2: 根因定位

通过代码追踪发现了**无限重渲染循环**：

**问题代码位置 1**：`web/src/features/system-settings/models/tier-price-editor.tsx:96-98`

```tsx
useEffect(() => {
  onValidationChange?.({ errors, warnings })
}, [errors, warnings, onValidationChange])  // ← onValidationChange 在依赖数组中
```

**问题代码位置 2**：`web/src/features/models/components/drawers/model-mutate-drawer.tsx:1364-1375`

```tsx
onValidationChange={(validation) =>  // ← 内联箭头函数，每次渲染都是新引用
  setGroupPriceRows((prev) =>
    prev.map((r) =>
      r.groupName === row.groupName
        ? { ...r, tierErrors: validation.errors }
        : r
    )
  )
}
```

**触发循环**：
1. 打开档位表 → 渲染 `TierPriceEditor`
2. `useEffect` 执行，调用 `onValidationChange`
3. 父组件执行 `setGroupPriceRows` → 状态更新
4. 父组件重渲染 → `onValidationChange` 获得新的函数引用
5. `TierPriceEditor` 检测到依赖变化 → 再次执行 `useEffect`
6. 回到步骤 2，形成无限循环
7. React 检测到过多重渲染（>50 次）→ 抛出错误
8. 错误边界捕获 → 显示 GeneralError（默认 500 页面）

## 修复方案

**修改文件**：`web/src/features/system-settings/models/tier-price-editor.tsx`

**修复方法**：从 `useEffect` 依赖数组中移除 `onValidationChange`

```tsx
// 修改前
useEffect(() => {
  onValidationChange?.({ errors, warnings })
}, [errors, warnings, onValidationChange])

// 修改后
// 不把 onValidationChange 放入依赖 —— 它是父组件传入的内联函数，每次渲染都是新引用，
// 会触发无限循环：effect 调用 → setGroupPriceRows → 父组件重渲染 → 新引用 → effect 再次触发。
// eslint-disable-next-line react-hooks/exhaustive-deps
useEffect(() => {
  onValidationChange?.({ errors, warnings })
}, [errors, warnings])
```

**为什么这样修复是安全的**：

1. `onValidationChange` 是一个**上报函数**，不依赖任何闭包变量
2. 它只是将最新的 `errors` 和 `warnings` 传递给父组件
3. 父组件接收到的始终是最新值（通过 `errors` 和 `warnings` 依赖保证）
4. 函数引用的变化不影响功能正确性

## 验证

### 1. 编译验证
```bash
cd web && npm run build
```
✅ 构建成功，无错误

### 2. 回归测试
创建了专门的回归测试：`tier-price-editor-stability.test.tsx`

测试用例：
- ✅ 使用内联函数作为 `onValidationChange` 不会触发无限循环
- ✅ 验证状态稳定时父组件不会过度重渲染
- ✅ 渲染次数 < 10（正常范围）

```bash
npm test -- tier-price-editor-stability.test.tsx
```
结果：
```
Test Files  1 passed (1)
Tests       2 passed (2)
Duration    7.09s
```

## 影响范围

### 受影响功能
- 模型管理 - 元信息编辑 - 分组分别定价 - 档位表

### 其他使用 TierPriceEditor 的地方
通过代码搜索确认，`TierPriceEditor` 还在以下位置使用：
- `model-pricing-sheet.tsx:733` - 也使用了 `onValidationChange`

这些位置都将受益于此修复，避免潜在的无限循环问题。

## 技术总结

### 问题模式
**React useEffect 无限循环**：当 effect 的依赖项在每次渲染时都是新引用时，会触发无限循环。

### 常见触发场景
1. 依赖数组中包含内联函数/对象
2. 依赖数组中包含未使用 `useCallback` 包裹的父组件回调
3. effect 内部触发状态更新，导致父组件重渲染

### 修复策略
1. **首选**：从依赖数组中移除不必要的函数依赖（如上报类回调）
2. **备选**：使用 `useCallback` 包裹父组件传入的回调
3. **最后手段**：添加 `eslint-disable` 注释并详细说明原因

### 最佳实践
- ✅ 上报类回调（只传递数据，不依赖闭包）：移除依赖
- ✅ 事件处理函数：使用 `useCallback` 包裹
- ✅ 复杂对象依赖：使用 `useMemo` 稳定引用
- ✅ 添加回归测试防止未来引入相同问题

## 相关提交

- 修复代码：`web/src/features/system-settings/models/tier-price-editor.tsx`
- 回归测试：`web/src/features/system-settings/models/__tests__/tier-price-editor-stability.test.tsx`
