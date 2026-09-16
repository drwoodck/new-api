package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
)

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

// TestTaskDirectVideoURL 锁定直链提取规则:ResultURL 优先,但旧版写成的
// 自引用 /content 代理地址必须跳过(那会让代理请求自己,Sora 渠道上游没有
// /content 子路径,404→502 死循环);此时回退 task.Data 里的 url/video_url。
func TestTaskDirectVideoURL(t *testing.T) {
	selfURL := "https://station.example.com/v1/videos/task_x/content"
	direct := "https://filer2.fdai.xyz/a.mp4"

	// 干净的 ResultURL 直接用
	task := &model.Task{TaskID: "task_x", Data: []byte(`{}`)}
	task.PrivateData.ResultURL = direct
	if got := taskDirectVideoURL(task); got != direct {
		t.Errorf("干净 ResultURL: 得到 %q, 期望 %q", got, direct)
	}

	// 自引用地址 + Data 里有直链 → 用 Data 里的
	task = &model.Task{TaskID: "task_x", Data: []byte(`{"url":"` + direct + `"}`)}
	task.PrivateData.ResultURL = selfURL
	if got := taskDirectVideoURL(task); got != direct {
		t.Errorf("自引用回退: 得到 %q, 期望 %q", got, direct)
	}

	// 自引用地址 + Data 是 video_url 字段
	task = &model.Task{TaskID: "task_x", Data: []byte(`{"video_url":"` + direct + `"}`)}
	task.PrivateData.ResultURL = selfURL
	if got := taskDirectVideoURL(task); got != direct {
		t.Errorf("video_url 回退: 得到 %q, 期望 %q", got, direct)
	}

	// 自引用地址 + 无直链可用 → 空串(调用方走 /content 透传)
	task = &model.Task{TaskID: "task_x", Data: []byte(`{"progress":100}`)}
	task.PrivateData.ResultURL = selfURL
	if got := taskDirectVideoURL(task); got != "" {
		t.Errorf("无直链应返回空: 得到 %q", got)
	}
}

// TestSingleLineKey 锁定 Authorization 头的取 key 规则:多 key 渠道的整串
// (带换行)必须拆开,否则 net/http 以 invalid header field value 拒绝请求。
func TestSingleLineKey(t *testing.T) {
	ch := &model.Channel{Key: "key-a\nkey-b"}
	task := &model.Task{}
	if got := singleLineKey(ch, task); got != "key-a" {
		t.Errorf("多 key 兜底: 得到 %q, 期望第一行 key-a", got)
	}

	task.PrivateData.Key = "key-submit"
	if got := singleLineKey(ch, task); got != "key-submit" {
		t.Errorf("回存 key 优先: 得到 %q", got)
	}

	single := &model.Channel{Key: "sk-only"}
	task2 := &model.Task{}
	if got := singleLineKey(single, task2); got != "sk-only" {
		t.Errorf("单 key: 得到 %q", got)
	}
}
