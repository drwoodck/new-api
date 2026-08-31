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
