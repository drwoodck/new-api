package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeInputMaterialPriceList(t *testing.T) {
	tests := []struct {
		name    string
		in      InputMaterialPriceList
		want    InputMaterialPriceList
		wantErr bool
	}{
		{
			name: "空表返回nil",
			in:   nil,
			want: nil,
		},
		{
			name: "图片按张_视频按秒_合法",
			in: InputMaterialPriceList{
				{MaterialType: "image", PricePerUnit: 0.01},
				{MaterialType: "video", PricePerSecond: 0.1, DefaultSeconds: 20},
				{MaterialType: "audio", PricePerSecond: 0.005},
			},
			want: InputMaterialPriceList{
				{MaterialType: "image", PricePerUnit: 0.01},
				{MaterialType: "video", PricePerSecond: 0.1, DefaultSeconds: 20},
				{MaterialType: "audio", PricePerSecond: 0.005},
			},
		},
		{
			name:    "material_type非法",
			in:      InputMaterialPriceList{{MaterialType: "doc", PricePerUnit: 0.1}},
			wantErr: true,
		},
		{
			name: "同类型重复",
			in: InputMaterialPriceList{
				{MaterialType: "image", PricePerUnit: 0.01},
				{MaterialType: "image", PricePerUnit: 0.02},
			},
			wantErr: true,
		},
		{
			name:    "负价拒绝",
			in:      InputMaterialPriceList{{MaterialType: "video", PricePerSecond: -0.1}},
			wantErr: true,
		},
		{
			name:    "价格超上限拒绝",
			in:      InputMaterialPriceList{{MaterialType: "image", PricePerUnit: MaxTierPrice * 2}},
			wantErr: true,
		},
		{
			// 归一化语义是钳制不是拒绝（与解析侧"钳制后使用"同向），
			// 极端值 9999 同样收敛到上限 —— brief 原表此行写的是拒绝，
			// 与实现注释及相邻钳制用例矛盾，按实现语义修正。
			name: "DefaultSeconds极端值钳制到上限",
			in:   InputMaterialPriceList{{MaterialType: "audio", PricePerSecond: 0.001, DefaultSeconds: 9999}},
			want: InputMaterialPriceList{{MaterialType: "audio", PricePerSecond: 0.001, DefaultSeconds: 3600}},
		},
		{
			name:    "DefaultSeconds为负拒绝",
			in:      InputMaterialPriceList{{MaterialType: "audio", PricePerSecond: 0.001, DefaultSeconds: -1}},
			wantErr: true,
		},
		{
			name: "DefaultSeconds钳制到上限",
			in:   InputMaterialPriceList{{MaterialType: "video", PricePerSecond: 0.1, DefaultSeconds: 5000}},
			want: InputMaterialPriceList{{MaterialType: "video", PricePerSecond: 0.1, DefaultSeconds: 3600}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeInputMaterialPriceList(tt.in, MaxTaskDurationSeconds)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestInputMaterialPriceListValuerScanner(t *testing.T) {
	list := InputMaterialPriceList{{MaterialType: "video", PricePerSecond: 0.1}}
	v, err := list.Value()
	require.NoError(t, err)
	var back InputMaterialPriceList
	require.NoError(t, back.Scan(v))
	assert.Equal(t, list, back)

	// NULL 语义
	var nilList InputMaterialPriceList
	nilV, err := nilList.Value()
	require.NoError(t, err)
	assert.Nil(t, nilV)
	var fromNil InputMaterialPriceList
	require.NoError(t, fromNil.Scan(nil))
	assert.Nil(t, fromNil)
}
