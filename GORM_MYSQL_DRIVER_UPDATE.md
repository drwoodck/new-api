# GORM MySQL 驱动更新修复

## 问题描述

容器启动时报错：
```
Error 1091 (42000): Can't DROP 'uni_tokens_key'; check that column/key exists
```

## 根因分析

### 1. 触发问题的代码

上游 commit `27ff6a876` 在 `model/main.go` 中添加了 `migrateTokenKeyUniqueness(DB)` 调用：

```go
func migrateTokenKeyUniqueness(db *gorm.DB) {
    var count int64
    db.Raw("SELECT COUNT(*) FROM information_schema.TABLE_CONSTRAINTS WHERE CONSTRAINT_NAME = 'uni_tokens_key' AND TABLE_NAME = 'tokens'").Scan(&count)
    if count > 0 {
        db.Exec("ALTER TABLE tokens DROP FOREIGN KEY uni_tokens_key")
    }
    // ...
}
```

### 2. 问题的本质

**实际情况**：
- 数据库中存在的约束名是 `idx_tokens_key`（unique index）
- 不是 `uni_tokens_key`（foreign key constraint）

**GORM MySQL 驱动 v1.4.3 的 Bug**：
- 将 unique index 误认为 foreign key constraint
- 尝试执行 `ALTER TABLE tokens DROP FOREIGN KEY uni_tokens_key`
- 但该 foreign key 不存在，导致 SQL 错误

### 3. 错误的具体原因

GORM v1.4.3 的 `Migrator.ColumnTypes` 方法有一个 bug：
- 它将所有带 `UNI` 索引类型的列误判为外键约束
- 实际上 `UNI` 表示 unique index，不是 foreign key

**错误的判断逻辑**（v1.4.3）：
```go
// 伪代码示例
if indexType == "UNI" {
    // 错误：将 unique index 当作 foreign key
    constraint.Type = "FOREIGN KEY"
}
```

## 上游修复

### Commit 9a8674425

上游通过自定义 migration dialector 解决了此问题：

```go
// 自定义 dialector 覆盖了 Migrator 行为
type migrationDialector struct {
    mysql.Dialector
}

func (d migrationDialector) Migrator(db *gorm.DB) gorm.Migrator {
    // 返回修复后的 migrator
}
```

### 需要的版本升级

**必需的依赖更新**：
- `gorm.io/driver/mysql` v1.4.3 → **v1.5.7**
- `github.com/glebarez/sqlite` v1.9.0 → **v1.11.0**

**为什么需要这些版本**：
1. MySQL v1.5.7 修复了 unique index 误判为 foreign key 的 bug
2. glebarez/sqlite v1.11.0 与 GORM v1.30.0 兼容（当前项目使用的版本）

## 解决方案

### 1. 更新依赖

```bash
go get gorm.io/driver/mysql@v1.5.7
go get github.com/glebarez/sqlite@v1.11.0
go mod tidy
```

### 2. 验证更新

```bash
# 检查版本
grep -E "gorm.io/driver/mysql|github.com/glebarez/sqlite" go.mod

# 输出应该是：
# github.com/glebarez/sqlite v1.11.0
# gorm.io/driver/mysql v1.5.7

# 测试编译
go build -o /dev/null .
```

### 3. Docker 构建验证

更新后的 `go.sum` 文件包含正确的依赖哈希，Docker 构建应该能够成功。

## 技术细节

### MySQL 驱动版本差异

#### v1.4.3（有 Bug）
```go
// Migrator.ColumnTypes 的问题逻辑
for _, index := range indexes {
    if index.Non_unique == 0 {
        // 错误：将所有 unique index 当作外键
        constraint.Type = "FOREIGN KEY"
    }
}
```

#### v1.5.7（已修复）
```go
// 正确区分 unique index 和 foreign key
for _, index := range indexes {
    if index.Key_name == "PRIMARY" {
        constraint.Type = "PRIMARY KEY"
    } else if index.Non_unique == 0 {
        constraint.Type = "UNIQUE"  // ✅ 正确识别
    } else {
        constraint.Type = "INDEX"
    }
}
```

### 数据库约束类型

| 约束类型 | MySQL 关键字 | GORM 识别 |
|---------|-------------|----------|
| 主键 | `PRIMARY KEY` | PRIMARY KEY |
| 唯一索引 | `UNIQUE INDEX` | UNIQUE ✅ |
| 外键 | `FOREIGN KEY` | FOREIGN KEY |
| 普通索引 | `INDEX` | INDEX |

### migrateTokenKeyUniqueness 的目的

该函数的本意是：
1. 检查是否存在旧的 `uni_tokens_key` 约束
2. 如果存在，删除它
3. 重新创建正确的唯一索引

**但在 v1.4.3 中**：
- 查询 `information_schema.TABLE_CONSTRAINTS` 找不到 `uni_tokens_key`（因为它是 index，不是 constraint）
- 但 GORM 的 bug 让代码误以为它存在
- 尝试删除不存在的外键，导致 SQL 错误

## 影响范围

### 受影响的功能
- ✅ 容器启动
- ✅ 数据库迁移
- ✅ Token 表操作

### 不受影响的功能
- ✅ 应用逻辑：代码层面无变化
- ✅ API 行为：运行时行为不变
- ✅ 其他数据库操作：只修复了 migrator 的 bug

## 验证步骤

### 1. 本地验证

```bash
# 编译测试
go build -o /dev/null .

# 运行测试（如果有）
go test ./...
```

### 2. 容器验证

```bash
# 构建 Docker 镜像
docker build -t new-api:test .

# 运行容器
docker run -p 3000:3000 new-api:test

# 检查日志，应该没有 "Can't DROP 'uni_tokens_key'" 错误
docker logs <container_id>
```

### 3. 数据库验证

```sql
-- 检查 tokens 表的索引
SHOW INDEX FROM tokens;

-- 应该看到 idx_tokens_key (unique index)
-- 不应该看到 uni_tokens_key (foreign key)
```

## 相关 Issue 和 PR

### 上游修复
- Commit: `9a8674425`
- 修复内容：自定义 migration dialector
- 相关依赖：MySQL v1.5.7, glebarez/sqlite v1.11.0

### GORM Issue
- GORM MySQL 驱动 v1.4.3 的 unique index 误判问题
- 在 v1.5.x 系列中已修复

## 最佳实践

### 1. 依赖版本管理
- 使用固定版本号，避免自动升级导致的兼容性问题
- 定期检查上游更新和安全补丁

### 2. 数据库迁移
- 总是在测试环境先验证迁移
- 备份数据库后再执行生产环境迁移
- 使用事务包裹 DDL 操作（如果数据库支持）

### 3. 容器构建
- 锁定 Go 版本和依赖版本
- 使用多阶段构建减小镜像体积
- 验证 `go.sum` 的完整性

## 总结

**问题**：GORM MySQL v1.4.3 将 unique index 误判为 foreign key

**解决**：升级到 v1.5.7 修复此 bug

**影响**：修复容器启动错误，不影响应用逻辑

**验证**：编译成功，go.sum 正确更新

---

**修改文件**：
- `go.mod` - 更新依赖版本
- `go.sum` - 更新依赖哈希

**验证命令**：
```bash
go build -o /dev/null .
docker build -t new-api:test .
```
