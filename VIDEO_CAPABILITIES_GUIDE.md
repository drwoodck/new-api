# 视频生成能力详细分类说明

## 核心发现

经过深入代码调查，我发现：**在画布目录的 capabilities 字段中，所有类型的视频生成能力都统一使用 `video_gen`**。

系统**不区分**文生视频、图生视频、首尾帧等不同场景，这些场景的区分是在**具体的 API 调用和参数配置**中完成的。

---

## 1. Capabilities 字段的作用

### 当前支持的 Capabilities
```go
// constant/canvas_contract.go
var capabilityToContract = map[string]string{
    "video_gen": "relay_video_async_v1",  // 所有视频生成
    "image_gen": "relay_image_async_v1",  // 图片生成
}
```

### 映射关系
- **`video_gen`** → `relay_video_async_v1` (异步视频生成协议)
- **`image_gen`** → `relay_image_async_v1` (异步图片生成协议)

**结论**：无论是文生视频、图生视频还是首尾帧，在 capabilities 字段中都填写 `video_gen`。

---

## 2. 视频生成场景的真正区分方式

虽然 capabilities 统一为 `video_gen`，但系统在**底层 API 路由和参数**中区分不同的视频生成场景。

### 2.1 API 路由层面的区分

以 Kling 为例（`router/video-router.go:38-41`）：
```go
// 文生视频
klingV1Router.POST("/videos/text2video", controller.RelayTask)
klingV1Router.GET("/videos/text2video/:task_id", controller.RelayTaskFetch)

// 图生视频
klingV1Router.POST("/videos/image2video", controller.RelayTask)
klingV1Router.GET("/videos/image2video/:task_id", controller.RelayTaskFetch)
```

### 2.2 模型命名层面的区分

某些供应商在模型名称中直接标识场景（`relay/channel/task/ali/constants.go:4-10`）：

```go
// 阿里万相系列模型
"wan2.7-i2v",         // 图生视频（image-to-video）
"wan2.7-t2v",         // 文生视频（text-to-video）
"wan2.5-i2v-preview", // 图生视频 preview 版本
"wan2.2-i2v-flash",   // 图生视频极速版
"wan2.2-i2v-plus",    // 图生视频专业版
```

豆包/Seedance 系列（`relay/channel/task/doubao/constants.go:7-8`）：
```go
"doubao-seedance-1-0-lite-t2v",  // 文生视频
"doubao-seedance-1-0-lite-i2v",  // 图生视频
```

### 2.3 参数结构层面的区分

视频生成场景通过**请求参数结构**区分（`relay/channel/task/ali/adaptor.go:47-48`）：

```go
type AliVideoMedia struct {
    FirstFrameURL  string  `json:"first_frame_url,omitempty"` // 首帧图片URL（首尾帧生视频）
    LastFrameURL   string  `json:"last_frame_url,omitempty"`  // 尾帧图片URL（首尾帧生视频）
}
```

**四种视频生成场景**（参考 `docs/seedance-video-api.md`）：

| 场景 | 参数特征 | 说明 |
|-----|---------|------|
| 文生视频 (T2V) | 只有 `prompt`，无图片/视频输入 | Text-to-Video |
| 图生视频单帧 (I2V) | `images` 数组 1 张图片 | Image-to-Video，首帧 |
| 图生视频首尾帧 | `metadata.content` 中包含首帧和尾帧 | 首尾帧约束生成 |
| 多模态参考生视频 | `metadata.content` 包含参考图片、视频、音频 | 参考素材引导生成 |

---

## 3. 实际案例分析

### 案例 1：Seedance 2.0 系列

**模型名称**：`sd5-seedance-2.0`、`doubao-seedance-1-5-pro-251215`

**Capabilities 配置**：
```
video_gen
```

**支持的场景**（通过参数区分）：
- ✅ 文生视频：只提供 `prompt`
- ✅ 图生视频（首帧）：提供 `images` 数组（1张图）
- ✅ 图生视频（首尾帧）：在 `metadata.content` 中提供首帧和尾帧
- ✅ 多模态参考生视频：在 `metadata.content` 中提供参考图片、视频、音频

**参数示例**：

**文生视频**：
```json
{
  "model": "sd5-seedance-2.0",
  "prompt": "一只猫在跑步",
  "duration": 6
}
```

**图生视频（首帧）**：
```json
{
  "model": "sd5-seedance-2.0",
  "prompt": "动画化这张图片",
  "images": ["https://example.com/cat.png"]
}
```

**图生视频（首尾帧）**：
```json
{
  "model": "sd5-seedance-2.0",
  "prompt": "平滑过渡",
  "metadata": {
    "content": [
      {"type": "image_url", "role": "first_frame", "image_url": "https://example.com/first.png"},
      {"type": "image_url", "role": "last_frame", "image_url": "https://example.com/last.png"}
    ]
  }
}
```

### 案例 2：阿里万相系列

阿里通过**模型名称**明确区分：

| 模型名称 | Capabilities | 支持场景 |
|---------|-------------|---------|
| `wan2.7-t2v` | `video_gen` | 仅文生视频 |
| `wan2.7-i2v` | `video_gen` | 图生视频（单帧/首尾帧） |
| `wan2.5-i2v-preview` | `video_gen` | 图生视频（有声） |

### 案例 3：Vidu

Vidu 支持首尾帧和参考图生视频（`relay/channel/task/vidu/adaptor.go:101`）：

**Capabilities 配置**：
```
video_gen
```

**支持场景**：
- ✅ 文生视频
- ✅ 首尾帧生视频
- ✅ 参考图生视频

---

## 4. 前端模型分类识别

前端通过模型名称的模式匹配来识别场景（`web/src/features/channels/lib/model-categories.ts:81`）：

```typescript
{
  pattern: /^(?:t2v|i2v|s2v)-01(?:-|$)/,
  // 匹配 t2v-01、i2v-01、s2v-01 开头的模型
}
```

**缩写含义**：
- `t2v` = Text-to-Video (文生视频)
- `i2v` = Image-to-Video (图生视频)
- `s2v` = Speech-to-Video (语音生视频，较少见)

---

## 5. 翻译对照

在日志和 UI 中使用的术语（`web/src/i18n/locales/zh.json`）：

| 英文 | 中文 | 说明 |
|-----|------|------|
| Text to Video | 文生视频 | 从文本生成视频 |
| Image to Video | 图生视频 | 从图片生成视频 |
| First Frame | 首帧 | 视频的第一帧 |
| Last Frame | 尾帧 | 视频的最后一帧 |
| Reference Image/Video | 参考图/视频 | 多模态引导素材 |

---

## 6. Schema Override 配置

如果需要在画布目录中配置**首尾帧模式切换**，可以使用 Schema Override：

```json
{
  "first_last_or_typed_arrays": "按 body 里已有的模式字段值,在首尾帧与按类型分桶两种协议间切换"
}
```

参考：`web/src/features/system-settings/models/canvas-catalog/components/schema-override/flat-media-editor.tsx:42`

---

## 7. 配置总结

### ✅ 正确的配置方式

无论模型支持什么视频生成场景，在画布目录中统一配置：

```
Remote ID: sd5-seedance-2.0
显示名称: Seedance 2.0 满血
能力: video_gen
Contract: relay_video_async_v1 (自动推导)
```

```
Remote ID: wan2.7-i2v
显示名称: 万相 2.7 图生视频
能力: video_gen
Contract: relay_video_async_v1 (自动推导)
```

```
Remote ID: wan2.7-t2v
显示名称: 万相 2.7 文生视频
能力: video_gen
Contract: relay_video_async_v1 (自动推导)
```

### ❌ 错误的配置方式

不要尝试在 capabilities 字段中细分场景：

```
❌ 能力: t2v           (不支持)
❌ 能力: i2v           (不支持)
❌ 能力: text_to_video (不支持)
❌ 能力: image_to_video (不支持)
❌ 能力: first_last_frame (不支持)
```

这些标识符不在系统的 `capabilityToContract` 映射表中，会导致无法自动推导 contract。

---

## 8. 场景区分的设计哲学

### 为什么不在 Capabilities 中细分？

1. **协议统一性**：所有视频生成场景使用同一个异步协议 `relay_video_async_v1`
2. **参数灵活性**：同一个模型可能支持多种场景（如 Seedance 支持所有四种）
3. **避免组合爆炸**：如果每个场景都是独立 capability，需要 `video_gen_t2v`、`video_gen_i2v`、`video_gen_first_last` 等大量组合
4. **向后兼容性**：新增场景（如参考视频生成）不需要修改 capability 映射

### 实际区分层级

```
能力层 (Capabilities)
    ↓
    video_gen  ← 统一入口
    ↓
协议层 (Contract)
    ↓
    relay_video_async_v1  ← 统一协议
    ↓
路由层 (API Endpoint)
    ↓
    /videos/text2video
    /videos/image2video
    ↓
参数层 (Request Body)
    ↓
    prompt only          → 文生视频
    + images[1]          → 图生视频（首帧）
    + first/last frames  → 首尾帧
    + reference media    → 参考生成
```

---

## 9. 常见问题

### Q1: 我的模型只支持图生视频，capabilities 还是填 video_gen 吗？
**答**：是的，仍然填 `video_gen`。场景限制在模型说明、API 文档或参数校验中体现。

### Q2: 如何告诉用户这个模型支持什么场景？
**答**：在以下地方说明：
1. 模型管理的"说明"字段
2. 画布目录的"限制说明"字段
3. API 文档

示例说明：
```
支持场景：
- 文生视频 (T2V)
- 图生视频单帧 (I2V)
- 图生视频首尾帧

不支持：
- 多模态参考生成
```

### Q3: 万相的 t2v 和 i2v 模型 capabilities 都是 video_gen，如何区分？
**答**：通过模型名称区分：
- `wan2.7-t2v` → 用户看到模型名就知道是文生视频
- `wan2.7-i2v` → 用户看到模型名就知道是图生视频

### Q4: 可以同时配置 video_gen 和其他自定义能力吗？
**答**：可以，capabilities 支持多个值用逗号分隔：
```
video_gen,VideoGen,custom_capability
```
但自动推导 contract 只看映射表中的已知值（`video_gen`）。

---

## 10. 开发者备忘

### 如需新增视频生成场景：
1. ✅ **不需要**修改 `constant/canvas_contract.go` 的 capability 映射
2. ✅ **不需要**修改画布目录的 capabilities 字段
3. ✅ **需要**在对应的 channel adaptor 中实现新的参数解析逻辑
4. ✅ **需要**在 API 文档中说明新场景的参数格式
5. ✅ **可选**在模型说明中添加支持场景的描述

### 如需新增完全不同的视频协议：
1. 在 `constant/canvas_contract.go` 中添加新的 capability → contract 映射
   ```go
   "video_gen_sync": "relay_video_sync_v1",
   ```
2. 在画布客户端实现新的 contract 处理逻辑
3. 更新画布目录配置指南

---

## 总结

**关键要点**：
- ✅ 所有视频生成模型的 capabilities 都填 `video_gen`
- ✅ 文生视频、图生视频、首尾帧等场景通过**API 路由、模型名称和参数结构**区分
- ✅ 不要在 capabilities 中使用 `t2v`、`i2v` 等标识符
- ✅ Contract 统一为 `relay_video_async_v1`
- ✅ 场景限制在模型说明和 API 文档中体现

**配置模板**：
```
Remote ID: [模型标识]
显示名称: [友好名称，可注明场景如"XX 文生视频"或"XX 图生视频"]
能力: video_gen
Contract: relay_video_async_v1 (自动推导)
说明: 支持的场景：文生视频、图生视频（首帧）...
```
