package controller

import "testing"

// 扩展名 → content-type 的反向映射必须覆盖 artifactExtByType 的每一项,
// 否则某类产物命中本地时浏览器收不到正确的 Content-Type
func TestContentTypeForArtifactPath(t *testing.T) {
	cases := map[string]string{
		"ab/task-1.mp4":  "video/mp4",
		"ab/task-1.webm": "video/webm",
		"ab/task-1.mov":  "video/quicktime",
		"ab/task-1.png":  "image/png",
		"ab/task-1.jpg":  "image/jpeg",
		"ab/task-1.webp": "image/webp",
		"ab/task-1.gif":  "image/gif",
		"ab/task-1.mp3":  "audio/mpeg",
		"ab/task-1.wav":  "audio/wav",
		// 兜底:未知扩展名给通用二进制流,而不是空字符串 ——
		// 空 Content-Type 会让浏览器猜,可能猜成 text/html
		"ab/task-1.bin": "application/octet-stream",
		"ab/task-1":     "application/octet-stream",
	}
	for path, want := range cases {
		if got := contentTypeForArtifactPath(path); got != want {
			t.Errorf("%q: 得到 %q, 期望 %q", path, got, want)
		}
	}
}

// 判定「该不该尝试本地」的逻辑:路径为空、或落盘节点不是本节点 → 不尝试
func TestShouldTryLocalArtifact(t *testing.T) {
	cases := []struct {
		name, path, node, thisNode string
		want                       bool
	}{
		{"正常命中", "ab/t.mp4", "node-a", "node-a", true},
		{"未落盘", "", "", "node-a", false},
		{"文件在别的节点", "ab/t.mp4", "node-b", "node-a", false},
		{"落盘节点未记录 —— 单节点部署的老数据,仍尝试", "ab/t.mp4", "", "node-a", true},
		{"本节点名未知 —— 单节点部署,仍尝试", "ab/t.mp4", "", "", true},
	}
	for _, c := range cases {
		if got := shouldTryLocalArtifact(c.path, c.node, c.thisNode); got != c.want {
			t.Errorf("%s: 得到 %v, 期望 %v", c.name, got, c.want)
		}
	}
}
