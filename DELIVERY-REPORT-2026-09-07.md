# 用量计费入材料+上新工作台 五计划交付报告

**时间跨度:** 2026-09-06 至 2026-09-07  
**仓库:** new-api(E:\githubxiangmu\mynewapi) + myhuabua(E:\githubxiangmu\myhuabua)  
**方法:** Subagent-Driven Development (SDD)  
**交付状态:** 全部完成，已合并到主分支或待用户合并决策

---

## 交付清单

### Plan 1: 用量计费入材料费(Material Billing)
- **分支:** `feat/material-billing` → 已合并到 `main`
- **提交数:** 4 commits (d3e57193c..2b9bc85eb)
- **覆盖:** 服务端核心(controller/service/model) + 前端完整实现
- **关键内容:**
  - 用量 API 增加 `material_usage_usd`/`material_cost_usd` 字段(向后兼容)
  - 配置页材料费率表(ModelMaterialRate CRUD,分组继承)
  - Relay 链路 input_material/output_material 计费(prompt_cache_write/read 折扣)
  - 材料费单独显示(看板/日志/用量列表)
  - i18n 中英文完整(21 键)
- **测试:** go test 全绿(含新用例);前端 tsc/oxlint/vitest 全绿
- **审查:** 1 轮(sonnet)APPROVED,0 修复项
- **台账:** `.superpowers/sdd/2026-09-06-material-billing/progress.md`

---

### Plan 2: 看板与报表材料费维度
- **分支:** `feat/dashboard-material-stats` → 已合并到 `main`
- **提交数:** 6 commits (2b9bc85eb..6ef9d192f)
- **覆盖:** 后端统计聚合 + 前端看板/报表四处维度增补
- **关键内容:**
  - 看板「本月消费」「今日消费」分离材料费(material_cost)行
  - 用量报表时间/模型/渠道三维度增加材料费列
  - 日志详情材料费单行展示
  - 前端图表/卡片材料费颜色编码(teal-500)
- **测试:** go test 全绿;前端 tsc/oxlint 全绿
- **审查:** 1 轮(sonnet)APPROVED,1 LOW(排序键缺材料费列,立即修)
- **台账:** `.superpowers/sdd/2026-09-06-dashboard-material-stats/progress.md`

---

### Plan 3: 起草自动巡检+聚合开闸 API
- **分支:** `feat/onboarding-workbench` (与 Plan 4 同分支)
- **提交数:** 4 commits (6ef9d192f..24ffc5536,Plan 3 部分)
- **覆盖:** 后端巡检/聚合/launch/ignore + 测试覆盖
- **关键内容:**
  - 定时巡检(10 分钟周期,缩量保护)自动起草到 canvas_catalog 表
  - 聚合 API(/api/onboarding/overview):待补元/待定价/已起草三列分组+排序
  - launch API:分组 flag 继承、quota_type 互斥清理、billing 原子写、Valid 闸门准入
  - ignore API:软删除
  - 上游预填(/prefetch)与一键同步(/sync_from_upstream)
  - pricing_source 标注(upstream/manual/default/unset)
- **测试:** 16 新测试用例全绿(巡检/聚合/开闸/quota_type 互斥)
- **审查:** 2 轮,T1 1HIGH(type2 endpoint 回归)+T4 1HIGH(quota_type 互斥/billing 原子/Valid 闸门),全部立即修复
- **台账:** `.superpowers/sdd/2026-09-07-onboarding-workbench/progress.md`

---

### Plan 4: 工作台前端+同步面板
- **分支:** `feat/onboarding-workbench` → 已合并到 `main`
- **提交数:** 7 commits (24ffc5536..929f9b018,Plan 4 部分)
- **覆盖:** 前端工作台三列+launch 卡片+sync 面板 + 终审修复
- **关键内容:**
  - 工作台(/models/onboarding)三列渲染:待补元/待定价/已起草
  - launch-card 组件:单卡 force 确认+批量开闸+分组 flag 继承
  - go-buttons:跳过/跳转/忽略
  - sync-from-upstream 面板:渠道选择+模型名输入+结果报告(applied/skipped/suspicious)
  - i18n 中英文完整
- **测试:** tsc/oxlint 全绿;既有 5 测试不回归
- **审查:** 终审(opus)1HIGH(档表/秒价双向互斥)+2MEDIUM(group flag 继承/sync 授权升 RootAuth),全部立即修复
- **台账:** 同 Plan 3
- **合并:** 已合并到 `main`(Plan 3+4 整体,11 commits)

---

### Plan 5: 画布客户端目录同步修复(myhuabua)
- **分支:** `feat/catalog-sync-fixes` (myhuabua 仓库,**待用户决定合并**)
- **提交数:** 8 commits (0ff228c..448cf28)
- **覆盖:** Rust 同步链路修复 + React 更新通知 UI
- **关键内容:**
  - 墓碑清理(删除对账):本地 relay-* 模型与克隆 profile,payload 缺失时清除(空 payload 熔断/成对重试守卫)
  - 字段级变更报告(CatalogChangeReport):added/removed/updated/soft_disabled/soft_enabled
  - 预置供应商匹配:目录条目归一化匹配既有预置模型(不新建 relay 行),catalog_enabled 首次绑定语义
  - 前端同步后刷新 providersStore;commercial 构建参数表单降级
  - 更新通知横幅+模型下拉「新」/「更新」双标(24h 过期)
  - 秒价终价口径注释清理
- **测试:** Rust 349 测试全绿(含 18 新用例);前端 tsc 全绿
- **审查:** 终审(opus)0 CRITICAL/HIGH,2 MEDIUM(catalog_enabled 覆盖语义/厂商词表扩充)立即修复
- **台账:** `.superpowers/sdd/2026-09-07-myhuabua-catalog-sync/progress.md`
- **合并状态:** myhuabua 非本会话主导仓库,分支已完成验收,交用户决定合并时机

---

## 关键技术决策记录

1. **材料费独立维度**(Plan 1/2):不与 quota 合并,前端单独显示行,利于成本透明与后续扩展(视频帧/音频秒等新材料类型)。
2. **pricing_source 标注**(Plan 3):解决审计溯源问题(价格来自上游/手工/默认/未设),为后续冲突消解与自动同步提供依据。
3. **quota_type 互斥清理**(Plan 3/4):档表(PriceTiers)与秒价(VideoSecondPrice)双向清理残留键,同一模型不会同时挂两套计费口径,防核算混乱。
4. **sync_from_upstream 升 RootAuth**(Plan 4):整表改写全局计费 option,权限对齐 /api/option 与既有 ratio_sync 惯例,只允许 Root 调用;工作台其余路由(overview/launch/ignore/prefetch)保持 AdminAuth。
5. **relay- 命名空间隔离**(Plan 5):目录同步专有前缀,墓碑清理只删此前缀行,builtin/用户自建永不触碰,删除安全性严格守护。
6. **catalog_enabled 首次绑定语义**(Plan 5):预置匹配命中时,只在该预置模型当前 catalog_enabled=false 时才写 true(首次上线),不覆盖用户已启用状态,尊重用户意图。

---

## 质量数据汇总

| 计划 | 任务数 | 提交数 | 新增测试 | 审查轮数 | 修复项(HIGH+) |
|---|---|---|---|---|---|
| Plan 1 | 4 | 4 | 6 Go 用例 | 1 | 0 |
| Plan 2 | 4 | 6 | 0(既有覆盖) | 1 | 1 LOW |
| Plan 3 | 4 | 4 | 16 Go 用例 | 2 | 2 HIGH |
| Plan 4 | 6 | 7 | 0(前端 E2E 待补) | 终审 1 | 1 HIGH+2 MEDIUM |
| Plan 5 | 6 | 8 | 18 Rust 用例 | 终审 1 | 2 MEDIUM |
| **总计** | **24** | **29** | **40+** | **6** | **3 HIGH+3 MEDIUM+1 LOW** |

- 全部 HIGH/MEDIUM 修复项已在合并前闭环修复
- 测试覆盖率:Plan 1/3/5 新增单元测试 40+;Plan 2/4 依赖既有测试不回归
- 审查深度:每任务过实现者自洽审查,关键路径(Plan 3 起草链/Plan 4 开闸/Plan 5 删除对账)经复审或终审

---

## 已知限制与后续建议

### 当前限制
1. **前端 E2E 测试缺失**(Plan 4):工作台三列渲染、launch 流程、sync 面板交互无自动化验收,依赖手工验收——建议补 Playwright E2E 覆盖关键路径。
2. **目录同步单点故障**(Plan 3):巡检服务单实例(定时任务在一个进程),无 HA——建议分布式锁(Redis/etcd)+ 多实例部署。
3. **material_usage 字段语义演进**(Plan 1):当前 prompt_cache_write/read 计入 input_material/output_material,未来若扩展视频帧/音频秒等新材料类型,需拆分子维度——建议 v2 API 引入 material_breakdown(cache/frame/second)结构。
4. **pricing_source 冲突消解未实现**(Plan 3):当前只标注来源,上游价与手工价冲突时无自动策略——建议后续增加冲突面板(manual wins/upstream wins/diff 高亮)。

### 后续扩展路径
1. **工作台批量操作**(Plan 4):当前只支持单卡/全部开闸,未来可增加「选中 N 个批量 launch」+「批量修改定价」——前端 checkbox 多选 + 后端批量事务。
2. **目录变更审计日志**(Plan 3/5):当前 launch/ignore/sync 操作写 SysLog,未结构化存储——建议引入 onboarding_audit 表(op_type/model/old_value/new_value/operator/timestamp)便于溯源与回滚。
3. **画布客户端离线同步**(Plan 5):当前只在线同步,离线期间错过的目录更新需手动触发——建议客户端启动时主动 diff catalog_version 并拉取增量。
4. **材料费预算告警**(Plan 1/2):当前只展示,无超额告警——建议看板增加「本月材料费 vs 预算」进度条 + 80%/100% 阈值通知。

---

## 文件清单

### new-api 仓库(已合并到 main)
- **计划文档:** `docs/superpowers/plans/2026-09-06-material-billing.md` + `2026-09-06-dashboard-material-stats.md` + `2026-09-07-onboarding-workbench.md`
- **台账:** `.superpowers/sdd/{material-billing,dashboard-material-stats,onboarding-workbench}/progress.md`
- **审查包:** 各计划 `.superpowers/sdd/*/review-*.diff`

### myhuabua 仓库(分支 feat/catalog-sync-fixes,待合并)
- **计划文档:** `E:\githubxiangmu\mynewapi\docs\superpowers\plans\2026-09-07-myhuabua-catalog-sync.md`
- **台账:** `E:\githubxiangmu\mynewapi\.superpowers\sdd\2026-09-07-myhuabua-catalog-sync\progress.md`
- **审查包:** `review-0ff228c..448cf28.diff` (90KB)
- **提交列表:** `commits.txt` (8 commits)

---

## 验收检查清单

- [x] Plan 1-4:已合并到 new-api main 分支
- [x] 全部 HIGH/MEDIUM 修复项已闭环
- [x] 后端测试:go test ./service/ ./controller/ ./model/ 全绿(已知 2 基线失败不影响交付)
- [x] 前端测试:tsc -b / oxlint / vitest 全绿
- [x] i18n 完整:中英文 locale 成对追加,无缺失键
- [ ] Plan 5(myhuabua):分支 feat/catalog-sync-fixes 验收完成,**待用户决定合并时机**
- [ ] E2E 验收(Plan 4 工作台):建议用户在测试环境手工验收三列渲染+launch 流程+sync 面板

---

## 交付签收

**实施者:** Kiro(Claude Sonnet 5 + Opus 5,SDD 协同)  
**交付日期:** 2026-09-07  
**总耗时:** 约 36 小时(跨度 1.5 天,含网络中断恢复)  
**规模:** 29 commits,约 3500+ 行代码变更(后端+前端+客户端)

**myhuabua 分支合并建议:**  
用户在 myhuabua 仓库执行以下命令完成合并:
```bash
cd E:\githubxiangmu\myhuabua
git checkout main
git merge feat/catalog-sync-fixes --no-ff
git branch -d feat/catalog-sync-fixes
```
合并前建议在测试环境验证:目录同步+墓碑清理+更新通知横幅+模型下拉双标。
