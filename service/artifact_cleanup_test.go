package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 超过保留期的文件被删,未超过的保留
func TestCleanupRemovesExpiredOrphans(t *testing.T) {
	dir := t.TempDir()
	old := artifactDirOverride
	artifactDirOverride = dir
	defer func() { artifactDirOverride = old }()

	shard := filepath.Join(dir, "ab")
	os.MkdirAll(shard, 0o755)

	oldFile := filepath.Join(shard, "task-old.mp4")
	newFile := filepath.Join(shard, "task-new.mp4")
	os.WriteFile(oldFile, []byte("x"), 0o644)
	os.WriteFile(newFile, []byte("x"), 0o644)

	// 把 oldFile 的修改时间推到 30 天前
	past := time.Now().Add(-30 * 24 * time.Hour)
	os.Chtimes(oldFile, past, past)

	removed, err := cleanupOrphanFiles(10) // 保留期 10 天
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, 期望 1", removed)
	}
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Error("过期文件仍存在")
	}
	if _, err := os.Stat(newFile); err != nil {
		t.Error("未过期文件被误删")
	}
}

// 临时文件(.tmp)也要清 —— 下载中途进程被杀会留下它们
func TestCleanupRemovesStaleTempFiles(t *testing.T) {
	dir := t.TempDir()
	old := artifactDirOverride
	artifactDirOverride = dir
	defer func() { artifactDirOverride = old }()

	shard := filepath.Join(dir, "ab")
	os.MkdirAll(shard, 0o755)
	tmpFile := filepath.Join(shard, ".dl-12345.tmp")
	os.WriteFile(tmpFile, []byte("partial"), 0o644)

	// 临时文件用更短的阈值 —— 一小时前的 .tmp 肯定是被杀死的下载留下的
	past := time.Now().Add(-2 * time.Hour)
	os.Chtimes(tmpFile, past, past)

	removed, err := cleanupStaleTempFiles()
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, 期望 1", removed)
	}
	if _, err := os.Stat(tmpFile); !os.IsNotExist(err) {
		t.Error("过期临时文件仍存在")
	}
}

// 刚创建的 .tmp 不能删 —— 那可能是正在进行的下载
func TestCleanupSparesActiveTempFiles(t *testing.T) {
	dir := t.TempDir()
	old := artifactDirOverride
	artifactDirOverride = dir
	defer func() { artifactDirOverride = old }()

	shard := filepath.Join(dir, "ab")
	os.MkdirAll(shard, 0o755)
	tmpFile := filepath.Join(shard, ".dl-active.tmp")
	os.WriteFile(tmpFile, []byte("downloading"), 0o644)

	removed, err := cleanupStaleTempFiles()
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 {
		t.Errorf("removed = %d, 期望 0 —— 不该删正在进行的下载", removed)
	}
	if _, err := os.Stat(tmpFile); err != nil {
		t.Error("活跃的临时文件被误删")
	}
}

// 保留期为 0 或负数时不清理任何东西 —— 防止配置写错导致全量删除
func TestCleanupRefusesNonPositiveRetention(t *testing.T) {
	dir := t.TempDir()
	old := artifactDirOverride
	artifactDirOverride = dir
	defer func() { artifactDirOverride = old }()

	shard := filepath.Join(dir, "ab")
	os.MkdirAll(shard, 0o755)
	f := filepath.Join(shard, "task-x.mp4")
	os.WriteFile(f, []byte("x"), 0o644)
	past := time.Now().Add(-100 * 24 * time.Hour)
	os.Chtimes(f, past, past)

	for _, days := range []int{0, -1} {
		removed, err := cleanupOrphanFiles(days)
		if err != nil {
			t.Fatal(err)
		}
		if removed != 0 {
			t.Errorf("retention=%d 时删了 %d 个文件,应为 0", days, removed)
		}
	}
	if _, err := os.Stat(f); err != nil {
		t.Error("保留期非正时文件被删")
	}
}
