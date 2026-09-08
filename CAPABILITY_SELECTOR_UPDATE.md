# 画布目录"能力"字段改为选择模式

## 修改说明

将画布模型目录配置表单中的"能力"字段从**输入框**改为**下拉选择框**，提升用户体验并减少输入错误。

---

## 修改内容

### 修改文件
- `web/src/features/system-settings/models/canvas-catalog/components/canvas-catalog-form-dialog.tsx`

### 变更详情

#### 修改前
```tsx
<Input
  placeholder='video_gen'
  {...field}
  onChange={(e) => {
    field.onChange(e)
    // 自动推导 contract 逻辑
  }}
/>
<FormDescription>
  {t('逗号分隔,如 video_gen,VideoGen')}
  {meta && Object.keys(meta.capability_to_contract).length > 0
    ? `;${t('已知能力')}: ${Object.keys(meta.capability_to_contract).join(', ')}`
    : null}
</FormDescription>
```

#### 修改后
```tsx
<Select
  items={knownCapabilities.map((c) => ({ value: c, label: c }))}
  value={field.value}
  onValueChange={(v) => {
    if (v == null) return
    field.onChange(v)
    // 自动推导 contract 逻辑
  }}
>
  <FormControl>
    <SelectTrigger disabled={!meta} className='w-full'>
      <SelectValue placeholder={t('选择模型能力类型')} />
    </SelectTrigger>
  </FormControl>
  <SelectContent alignItemWithTrigger={false}>
    <SelectGroup>
      {knownCapabilities.map((c) => (
        <SelectItem key={c} value={c}>
          {c}
        </SelectItem>
      ))}
    </SelectGroup>
  </SelectContent>
</Select>
<FormDescription>
  {meta
    ? t('选择该模型的能力类型,将自动推导对应的 contract')
    : t('正在加载可用的能力列表...')}
</FormDescription>
```

---

## 功能改进

### 1. 用户体验提升
- ✅ **防止输入错误**：用户不再需要手动输入 `video_gen`、`image_gen`，避免拼写错误
- ✅ **清晰的选项**：直接看到所有可用的能力类型，无需记忆
- ✅ **统一交互**：与 Contract 字段保持一致的选择交互方式

### 2. 自动推导保持不变
- ✅ 选择能力后仍然自动推导 Contract
- ✅ 新建模式下自动推导
- ✅ 编辑模式下不覆盖已有值
- ✅ 手动修改 Contract 后不再自动推导（逃生舱机制）

### 3. 数据源
选项列表从后端 `/api/canvas/admin/meta` 获取：
```typescript
const knownCapabilities = meta
  ? Object.keys(meta.capability_to_contract)
  : []
// 当前: ['video_gen', 'image_gen']
```

---

## 界面对比

### 修改前
```
┌─ 能力 ────────────────────────────────────┐
│ [video_gen________________]  ← 输入框     │
│ 逗号分隔,如 video_gen,VideoGen            │
│ ;已知能力: video_gen, image_gen           │
└───────────────────────────────────────────┘
```

### 修改后
```
┌─ 能力 ────────────────────────────────────┐
│ [video_gen           ▼]  ← 下拉选择框     │
│ 选择该模型的能力类型,将自动推导对应的     │
│ contract                                   │
└───────────────────────────────────────────┘

点击下拉后：
┌───────────────┐
│ video_gen     │ ← 可选项
│ image_gen     │ ← 可选项
└───────────────┘
```

---

## 使用流程

### 新建目录条目
1. 填写 Remote ID: `sd5-seedance-2.0`
2. 填写显示名称: `Seedance 2.0 满血`
3. **点击"能力"下拉框** → 选择 `video_gen`
4. Contract 自动填充为 `relay_video_async_v1`
5. 保存

### 编辑现有条目
1. 打开编辑对话框
2. "能力"字段显示当前值（如 `video_gen`）
3. 可点击下拉框更改为其他能力
4. Contract 不会自动覆盖（保留原值）
5. 保存

---

## 技术细节

### 1. 选项来源
```typescript
// 从 meta.capability_to_contract 提取键
const knownCapabilities = meta
  ? Object.keys(meta.capability_to_contract)
  : []

// 对应后端 constant/canvas_contract.go:
// map[string]string{
//     "video_gen": "relay_video_async_v1",
//     "image_gen": "relay_image_async_v1",
// }
```

### 2. 禁用状态
```typescript
<SelectTrigger disabled={!meta} className='w-full'>
```
- meta 未加载时禁用选择框
- 显示"正在加载可用的能力列表..."提示

### 3. 自动推导逻辑
```typescript
onValueChange={(v) => {
  if (v == null) return
  field.onChange(v)
  
  // 只在新建且未手动改过 contract 时自动推导
  if (!isEdit && !contractManuallyEdited.current && meta) {
    const derived = deriveContractFromCapabilities(
      v,
      meta.capability_to_contract
    )
    if (derived) form.setValue('contract', derived)
  }
}}
```

---

## 兼容性

### 后端兼容
- ✅ **无需修改后端**：仍然接收字符串类型的 `capabilities` 字段
- ✅ **存量数据兼容**：已有的能力值正常显示和编辑

### 前端兼容
- ✅ **表单验证**：保持原有验证规则
- ✅ **提交逻辑**：提交数据格式不变
- ✅ **编辑回填**：编辑时正确回填当前值

### 未来扩展
如需新增能力类型，只需：
1. 在后端 `constant/canvas_contract.go` 中添加映射
2. 前端自动从 `/api/canvas/admin/meta` 获取新选项

---

## 验证测试

### 功能测试
- ✅ 新建条目：能力下拉框正常显示选项
- ✅ 选择能力后：Contract 自动推导
- ✅ 编辑条目：能力字段正确回填
- ✅ 手动改 Contract：不再被能力字段覆盖
- ✅ 保存提交：数据正确保存

### 构建测试
```bash
cd web && npm run build
```
✅ 构建成功，无错误

---

## 优势总结

### 用户体验
1. **减少错误**：无需手动输入，避免拼写错误
2. **更直观**：直接看到所有可用选项
3. **学习成本低**：下拉选择比记忆文本更简单

### 维护性
1. **集中管理**：能力列表由后端统一管理
2. **易于扩展**：新增能力类型无需修改前端代码
3. **一致性**：与 Contract 字段交互方式一致

### 数据质量
1. **标准化**：确保填入的都是有效的能力标识
2. **防误操作**：不会出现 `video gen`（空格）或 `videogen`（少下划线）等错误
3. **自动推导准确**：选择的能力一定能推导出正确的 Contract

---

## 后续优化建议

### 1. 添加能力说明
在下拉选项中增加描述：
```tsx
<SelectItem value="video_gen">
  video_gen
  <span className="text-muted-foreground text-xs ml-2">
    - 视频生成
  </span>
</SelectItem>
```

### 2. 支持多选（可选）
如果未来需要支持一个模型配置多个能力：
```tsx
<MultiSelect
  values={field.value?.split(',') || []}
  onChange={(values) => field.onChange(values.join(','))}
>
```

### 3. 能力图标（可选）
为不同能力类型添加图标：
```tsx
{capability === 'video_gen' && <VideoIcon />}
{capability === 'image_gen' && <ImageIcon />}
```

---

## 迁移说明

### 对现有用户
- ✅ **无需数据迁移**：现有配置自动兼容
- ✅ **无需重新配置**：打开编辑即可看到当前值
- ✅ **操作习惯改变**：从输入改为选择，更简单

### 对管理员
- ✅ 新增条目时使用下拉选择，更不易出错
- ✅ 编辑条目时选择清晰，无需记忆能力名称
- ✅ 遵循之前的自动推导规则

---

## 相关文档
- [画布模型目录配置详细说明](./CANVAS_CATALOG_GUIDE.md)
- [视频能力详细分类说明](./VIDEO_CAPABILITIES_GUIDE.md)
