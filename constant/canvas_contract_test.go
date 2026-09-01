package constant

import "testing"

func TestContractForCapabilityKnownCapabilities(t *testing.T) {
	cases := map[string]string{
		"video_gen": "relay_video_async_v1",
		"image_gen": "relay_image_async_v1",
	}
	for capability, want := range cases {
		got, ok := ContractForCapability(capability)
		if !ok {
			t.Fatalf("capability %q: expected ok=true", capability)
		}
		if got != want {
			t.Errorf("capability %q: got %q, want %q", capability, got, want)
		}
	}
}

// 未知 capability 不能瞎猜一个契约回去 —— 管理员手填,界面据此提示。
func TestContractForCapabilityUnknownReturnsFalse(t *testing.T) {
	got, ok := ContractForCapability("audio_gen")
	if ok {
		t.Fatalf("expected ok=false for unknown capability, got contract %q", got)
	}
	if got != "" {
		t.Errorf("expected empty contract on miss, got %q", got)
	}
}

func TestIsSupportedContractKnownValues(t *testing.T) {
	for _, contract := range []string{"relay_video_async_v1", "relay_image_async_v1"} {
		if !IsSupportedContract(contract) {
			t.Errorf("contract %q: expected supported", contract)
		}
	}
}

// 目录管理页的"已配置完成 vs 未配置"分流靠这个函数判断契约是否画布真能用 ——
// 一个管理员手填的、画布不认识的 contract 必须判为不支持,否则目录总览会把
// 一个"保存后请求会被画布整条跳过"的条目错误归到"已配置完成"页。
func TestIsSupportedContractUnknownValue(t *testing.T) {
	if IsSupportedContract("relay_audio_async_v1") {
		t.Fatalf("expected unsupported contract to return false")
	}
	if IsSupportedContract("") {
		t.Fatalf("expected empty contract to return false")
	}
}
