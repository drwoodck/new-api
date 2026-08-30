package service

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 路径必须由 taskID 派生且不可逃逸出存储根目录。
// taskID 来自上游/用户可影响的数据,`../` 注入会写到任意位置。
func TestArtifactPathRejectsTraversal(t *testing.T) {
	for _, bad := range []string{
		"../etc/passwd", "..\\windows\\system32", "a/../../b",
		"/absolute/path", "", ".", "..",
	} {
		if _, _, err := ArtifactPathFor(bad, "video/mp4"); err == nil {
			t.Errorf("taskID %q 应被拒绝,但通过了", bad)
		}
	}
}

// 正常 taskID 落在根目录下,且带正确扩展名
func TestArtifactPathDerivesExtensionFromContentType(t *testing.T) {
	cases := map[string]string{
		"video/mp4":  ".mp4",
		"image/png":  ".png",
		"image/jpeg": ".jpg",
		"":           ".bin", // 上游没给 content-type 时的兜底
	}
	for ct, wantExt := range cases {
		rel, _, err := ArtifactPathFor("task-abc123", ct)
		if err != nil {
			t.Fatalf("content-type %q: %v", ct, err)
		}
		if !strings.HasSuffix(rel, wantExt) {
			t.Errorf("content-type %q 期望后缀 %q,得到 %q", ct, wantExt, rel)
		}
	}
}

// 分目录存放 —— 单目录堆几十万文件会让 ext4/NTFS 的目录遍历变慢
func TestArtifactPathShardsIntoSubdirectories(t *testing.T) {
	rel, _, err := ArtifactPathFor("task-abc123", "video/mp4")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rel, string(filepath.Separator)) &&
		!strings.Contains(rel, "/") {
		t.Errorf("期望分片子目录,得到扁平路径 %q", rel)
	}
}

// 写入必须是原子的:下载中途失败不能留下半个文件被代理当成完整产物读出去
func TestStoreArtifactIsAtomic(t *testing.T) {
	dir := t.TempDir()
	old := artifactDirOverride
	artifactDirOverride = dir
	defer func() { artifactDirOverride = old }()

	// 一个读到一半就报错的 reader
	r := &failingReader{data: []byte("partial data here"), failAfter: 5}
	_, _, err := StoreArtifact(context.Background(), "task-fail", "video/mp4", r)
	if err == nil {
		t.Fatal("期望报错")
	}

	// 存储根目录下不该留下任何非临时文件
	var found []string
	filepath.Walk(dir, func(p string, info os.FileInfo, e error) error {
		if e == nil && !info.IsDir() && !strings.HasSuffix(p, ".tmp") {
			found = append(found, p)
		}
		return nil
	})
	if len(found) > 0 {
		t.Errorf("失败的写入留下了文件: %v", found)
	}
}

func TestStoreAndOpenRoundTrip(t *testing.T) {
	dir := t.TempDir()
	old := artifactDirOverride
	artifactDirOverride = dir
	defer func() { artifactDirOverride = old }()

	want := []byte("fake mp4 bytes")
	rel, size, err := StoreArtifact(context.Background(), "task-ok", "video/mp4",
		bytes.NewReader(want))
	if err != nil {
		t.Fatal(err)
	}
	if size != int64(len(want)) {
		t.Errorf("size = %d, 期望 %d", size, len(want))
	}

	f, err := OpenArtifact(rel)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got := make([]byte, len(want))
	f.Read(got)
	if !bytes.Equal(got, want) {
		t.Errorf("内容不符: %q vs %q", got, want)
	}
}

// OpenArtifact 也要防逃逸 —— relPath 从 DB 读出,DB 可能被写脏
func TestOpenArtifactRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	old := artifactDirOverride
	artifactDirOverride = dir
	defer func() { artifactDirOverride = old }()

	for _, bad := range []string{"../../etc/passwd", "/etc/passwd"} {
		if f, err := OpenArtifact(bad); err == nil {
			f.Close()
			t.Errorf("relPath %q 应被拒绝", bad)
		}
	}
}

type failingReader struct {
	data      []byte
	pos       int
	failAfter int
}

func (r *failingReader) Read(p []byte) (int, error) {
	if r.pos >= r.failAfter {
		return 0, os.ErrClosed
	}
	n := copy(p, r.data[r.pos:r.failAfter])
	r.pos += n
	return n, nil
}
