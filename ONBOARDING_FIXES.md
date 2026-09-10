# 上新工作台缺陷修复总结

## 修复的问题

### 1. 模型列表滚动问题
**问题描述**：三列模型列表超出屏幕时无法向下滚动。

**根本原因**：
- `onboarding-workbench.tsx` 使用了 `<>` Fragment 作为根元素，没有设置正确的 flex 布局
- 三列 grid 容器使用 `min-h-0 flex-1`，但没有外层容器来控制整体高度
- `SyncFromUpstream` 组件在 grid 外面，导致整体高度计算错误

**修复方案**：
```tsx
// 修改前
<>
  <div className='grid min-h-0 flex-1 grid-cols-1 gap-4 lg:grid-cols-3'>
    {/* 三列 */}
  </div>
  <SyncFromUpstream />
</>

// 修改后
<div className='flex h-full flex-col gap-4 overflow-hidden'>
  <div className='grid min-h-0 flex-1 grid-cols-1 gap-4 lg:grid-cols-3'>
    {/* 三列 */}
  </div>
  <div className='shrink-0'>
    <SyncFromUpstream />
  </div>
</div>
```

**文件变更**：
- `web/src/features/onboarding/onboarding-workbench.tsx` (105-224行)
- `web/src/features/models/index.tsx` (122行)

---

### 2. 已关闭渠道的模型仍显示
**问题描述**：当渠道被禁用（status ≠ 1）后，该渠道的模型仍然出现在上新工作台的三列中。

**根本原因**：
- `GetEnabledModels()` 只检查 `abilities.enabled = true`
- 没有 JOIN `channels` 表检查 `channels.status = 1`
- 导致已禁用渠道的模型仍被认为是"已启用"

**修复方案**：
```go
// 修改前
func GetEnabledModels() []string {
	var models []string
	DB.Table("abilities").Where("enabled = ?", true).Distinct("model").Pluck("model", &models)
	return models
}

// 修改后
func GetEnabledModels() []string {
	var models []string
	DB.Table("abilities").
		Joins("INNER JOIN channels ON abilities.channel_id = channels.id").
		Where("abilities.enabled = ? AND channels.status = ?", true, 1).
		Distinct("abilities.model").
		Pluck("abilities.model", &models)
	return models
}
```

**同步修复**：
- `GetGroupEnabledModels()` 也进行了相同的修复，保持一致性

**文件变更**：
- `model/ability.go` (61-66行, 43-59行)

**影响范围**：
- 上新工作台"待补元数据"列：通过 `GetMissingModels()` → `GetEnabledModels()`
- 上新工作台"待定价"列：通过 `GetOnboardingOverview()` → `GetEnabledModels()`
- 目录接口 `GetCanvasCatalog()`：通过 `GetGroupEnabledModels()`

---

### 3. 底部按钮消失
**问题描述**：当上新工作台显示模型列表后，底部的"已上线模型更新"面板（包含"拉取预览"和"一键更新"按钮）被挤出可视区域。

**根本原因**：
- 父容器 `<div className='min-h-0 flex-1'>` 对所有 section 统一应用
- Onboarding section 需要特殊的 flex 布局才能让 `SyncFromUpstream` 可见
- 三列 grid 占用了所有可用空间，`SyncFromUpstream` 被挤出视口

**修复方案**：
```tsx
// 修改前
<div className='min-h-0 flex-1'>{renderSectionContent()}</div>

// 修改后
{activeSection === 'onboarding' ? (
  <div className='flex min-h-0 flex-1 flex-col overflow-hidden'>
    {renderSectionContent()}
  </div>
) : (
  <div className='min-h-0 flex-1'>{renderSectionContent()}</div>
)}
```

配合 `onboarding-workbench.tsx` 中的修复，确保：
1. 三列 grid 可以滚动
2. `SyncFromUpstream` 始终固定在底部可见

**文件变更**：
- `web/src/features/models/index.tsx` (122-127行)

---

## 技术细节

### 布局层次结构（修复后）

```
SectionPageLayout
└─ Content (overflow-hidden on fixedContent)
   └─ Tabs + Section Content wrapper
      └─ Onboarding Section (flex flex-col overflow-hidden)
         └─ OnboardingWorkbench (flex h-full flex-col gap-4 overflow-hidden)
            ├─ Grid Container (min-h-0 flex-1 grid)
            │  ├─ WorkbenchColumn (overflow-hidden)
            │  │  └─ Inner (overflow-y-auto) ← 滚动发生在这里
            │  ├─ WorkbenchColumn (overflow-hidden)
            │  │  └─ Inner (overflow-y-auto)
            │  └─ WorkbenchColumn (overflow-hidden)
            │     └─ Inner (overflow-y-auto)
            └─ SyncFromUpstream (shrink-0) ← 固定在底部，始终可见
```

### 数据库查询变化

**修改前**：
```sql
SELECT DISTINCT model FROM abilities WHERE enabled = true
```

**修改后**：
```sql
SELECT DISTINCT abilities.model 
FROM abilities 
INNER JOIN channels ON abilities.channel_id = channels.id 
WHERE abilities.enabled = true AND channels.status = 1
```

### 测试验证要点

1. **滚动测试**：
   - 添加 50+ 个模型到某一列
   - 验证该列可以向下滚动
   - 验证其他列独立滚动
   - 验证底部 `SyncFromUpstream` 始终可见

2. **渠道过滤测试**：
   - 启用渠道 A，添加模型 `test-model-1`
   - 验证 `test-model-1` 出现在工作台
   - 禁用渠道 A（设置 `channels.status = 0`）
   - 刷新页面，验证 `test-model-1` 不再出现

3. **底部面板测试**：
   - 打开上新工作台
   - 验证"已上线模型更新"面板可见
   - 验证"拉取预览"和"一键更新"按钮可点击
   - 添加大量模型后再次验证面板可见性

---

## 回归风险评估

### 低风险
- `onboarding-workbench.tsx` 布局修改：仅影响上新工作台页面
- `models/index.tsx` 条件渲染：仅针对 onboarding section

### 中风险
- `GetEnabledModels()` 查询修改：
  - **影响范围**：所有调用该函数的地方
  - **已知调用点**：
    - `GetMissingModels()`（上新工作台"待补元数据"）
    - `GetOnboardingOverview()`（上新工作台"待定价"）
  - **风险缓解**：添加 JOIN 只是增加过滤条件，不改变返回值类型

- `GetGroupEnabledModels()` 查询修改：
  - **影响范围**：分组模型查询
  - **已知调用点**：`GetCanvasCatalog()`（目录接口）
  - **预期行为**：已禁用渠道的模型不再出现在用户目录中（符合预期）

---

## 修复文件清单

1. `web/src/features/onboarding/onboarding-workbench.tsx`
2. `web/src/features/models/index.tsx`
3. `model/ability.go`

---

## 提交建议

```bash
git add web/src/features/onboarding/onboarding-workbench.tsx
git add web/src/features/models/index.tsx
git add model/ability.go

git commit -m "fix(onboarding): fix workbench scrolling, disabled channel filtering, and bottom panel visibility

- Fix scrolling issue in three-column layout by adding proper flex container
- Filter out models from disabled channels (status != 1) in GetEnabledModels/GetGroupEnabledModels
- Ensure SyncFromUpstream panel stays visible at bottom with shrink-0
- Add special layout handling for onboarding section in models index

Fixes #XXX"
```
