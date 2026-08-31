package controller

import "testing"

// 载荷绑定与校验 —— 这部分不碰 DB,可以纯测
func TestContractReportRequestValidation(t *testing.T) {
	cases := []struct {
		name    string
		req     contractReportRequest
		wantErr bool
	}{
		{"完整载荷", contractReportRequest{
			InstallID: "inst-abc", ClientVersion: "0.1.22",
			Contracts: []string{"video.v1"}, SupportedVocab: 1,
		}, false},
		{"缺 install_id", contractReportRequest{
			ClientVersion: "0.1.22",
		}, true},
		{"缺 client_version", contractReportRequest{
			InstallID: "inst-abc",
		}, true},
		{"契约列表为空 —— 允许,老客户端可能一个都不支持", contractReportRequest{
			InstallID: "inst-abc", ClientVersion: "0.1.15", Contracts: nil,
		}, false},
		{"install_id 过长 —— 列宽 64,超了要拒绝而非静默截断", contractReportRequest{
			InstallID:     "x0123456789012345678901234567890123456789012345678901234567890123456789",
			ClientVersion: "0.1.22",
		}, true},
		{"契约数量异常多 —— 防止恶意载荷撑爆 JSON 列", contractReportRequest{
			InstallID: "inst-abc", ClientVersion: "0.1.22",
			Contracts: makeStrings(200),
		}, true},
	}
	for _, c := range cases {
		err := c.req.validate()
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err=%v, wantErr=%v", c.name, err, c.wantErr)
		}
	}
}

func makeStrings(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "contract-" + string(rune('a'+i%26))
	}
	return out
}
