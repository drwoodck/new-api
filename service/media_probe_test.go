package service

import (
	"encoding/binary"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildMinimalWav 构造 44 字节头 + data 的合法 wav,时长 = dataSize/byteRate。
func buildMinimalWav(dataSeconds int) []byte {
	sampleRate := 8000
	byteRate := sampleRate * 2 // 16bit mono
	dataSize := byteRate * dataSeconds
	buf := make([]byte, 0, 44+dataSize)
	buf = append(buf, []byte("RIFF")...)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(36+dataSize))
	buf = append(buf, []byte("WAVEfmt ")...)
	buf = binary.LittleEndian.AppendUint32(buf, 16)
	buf = binary.LittleEndian.AppendUint16(buf, 1) // PCM
	buf = binary.LittleEndian.AppendUint16(buf, 1) // mono
	buf = binary.LittleEndian.AppendUint32(buf, uint32(sampleRate))
	buf = binary.LittleEndian.AppendUint32(buf, uint32(byteRate))
	buf = binary.LittleEndian.AppendUint16(buf, 2) // block align
	buf = binary.LittleEndian.AppendUint16(buf, 16)
	buf = append(buf, []byte("data")...)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(dataSize))
	buf = append(buf, make([]byte, dataSize)...)
	return buf
}

// buildMinimalMp4 构造 ftyp + moov(mvhd v0) 的最小 mp4,
// 时长 = durationUnits/timescale。
func buildMinimalMp4(timescale, durationUnits uint32) []byte {
	mvhdPayload := make([]byte, 0, 100)
	mvhdPayload = append(mvhdPayload, 0, 0, 0, 0)               // version=0 + flags
	mvhdPayload = binary.BigEndian.AppendUint32(mvhdPayload, 0) // creation
	mvhdPayload = binary.BigEndian.AppendUint32(mvhdPayload, 0) // modification
	mvhdPayload = binary.BigEndian.AppendUint32(mvhdPayload, timescale)
	mvhdPayload = binary.BigEndian.AppendUint32(mvhdPayload, durationUnits)
	mvhdPayload = binary.BigEndian.AppendUint32(mvhdPayload, 0x00010000) // rate
	mvhdPayload = binary.BigEndian.AppendUint16(mvhdPayload, 0x0100)     // volume
	mvhdPayload = append(mvhdPayload, make([]byte, 2)...)                // reserved
	mvhdPayload = append(mvhdPayload, make([]byte, 8)...)                // pre_defined[2]
	mvhdPayload = append(mvhdPayload, make([]byte, 36)...)               // matrix[9]
	mvhdPayload = append(mvhdPayload, make([]byte, 24)...)               // pre_defined[6]
	mvhdPayload = binary.BigEndian.AppendUint32(mvhdPayload, 3)          // next_track_ID
	mvhd := append([]byte("mvhd"), mvhdPayload...)                       // full box:type + 100B payload
	moov := append(u32be(len(mvhd)+4), mvhd...)
	ftyp := append(u32be(20), []byte("ftypisom\x00\x00\x02\x00isomiso2")...)
	return append(ftyp, moov...)
}

func u32be(v int) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, uint32(v))
	return b
}

// buildXingMp3 构造带 Xing tag 的最小 mp3:MPEG1 Layer3 44100Hz mono 128kbps,
// Xing 声明总帧数 frames,时长 = frames × 1152 / 44100。
func buildXingMp3(frames uint32) []byte {
	header := []byte{0xFF, 0xFB, 0x90, 0xC0} // sync|MPEG1 L3 no-crc|128k 44.1k|mono
	sideInfo := make([]byte, 17)             // MPEG1 L3 mono 侧信息
	xing := append([]byte("Xing"), 0, 0, 0, 1)
	xing = binary.BigEndian.AppendUint32(xing, frames)
	return append(append(header, sideInfo...), xing...)
}

// webmElement 按 EBML 编码一个元素(载荷 < 128B 的 fixture 简化 size vint)。
func webmElement(id, payload []byte) []byte {
	out := append([]byte{}, id...)
	out = append(out, byte(0x80|len(payload)))
	return append(out, payload...)
}

// buildMinimalWebm 构造 EBML 头 + Segment/Info(TimecodeScale=1e6,Duration=
// float64) 的最小 webm,时长 = durationScaled × 1e6 / 1e9 秒。
func buildMinimalWebm(durationScaled float64) []byte {
	var dur [8]byte
	binary.BigEndian.PutUint64(dur[:], math.Float64bits(durationScaled))
	info := webmElement([]byte{0x2A, 0xD7, 0xB1}, []byte{0x0F, 0x42, 0x40}) // TimecodeScale = 1000000
	info = append(info, webmElement([]byte{0x44, 0x89}, dur[:])...)
	segment := webmElement([]byte{0x15, 0x49, 0xA9, 0x66}, info)
	ebml := webmElement([]byte{0x1A, 0x45, 0xDF, 0xA3}, webmElement([]byte{0x42, 0x86}, []byte("webm")))
	return append(ebml, segment...)
}

func serveBytes(t *testing.T, data []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// allowLoopbackProbe 临时放开 SSRF 连接校验让 httptest(环回)可被探测;
// 仅测试接缝,生产 mediaProbeDialControl 恒为 blockPrivateDialControl。
func allowLoopbackProbe(t *testing.T) {
	t.Helper()
	saved := mediaProbeDialControl
	mediaProbeDialControl = nil
	t.Cleanup(func() { mediaProbeDialControl = saved })
}

func TestProbeMediaDurationWavAndMp4(t *testing.T) {
	allowLoopbackProbe(t)

	wavSrv := serveBytes(t, buildMinimalWav(3))
	d, ok := ProbeMediaDuration(wavSrv.URL, 3*time.Second)
	require.True(t, ok)
	assert.InDelta(t, 3.0, d, 0.01)

	mp4Srv := serveBytes(t, buildMinimalMp4(1000, 5000)) // 5s
	d, ok = ProbeMediaDuration(mp4Srv.URL, 3*time.Second)
	require.True(t, ok)
	assert.InDelta(t, 5.0, d, 0.01)
}

func TestProbeMediaDurationFailure(t *testing.T) {
	allowLoopbackProbe(t)

	// 非 http(s) scheme 直接拒绝
	_, ok := ProbeMediaDuration("file:///etc/passwd", time.Second)
	assert.False(t, ok)

	// 空 URL / 非正预算直接拒绝
	_, ok = ProbeMediaDuration("", time.Second)
	assert.False(t, ok)
	_, ok = ProbeMediaDuration("http://example.invalid/a.mp4", 0)
	assert.False(t, ok)

	// 探测不到时长返回 false
	srv := serveBytes(t, []byte("not a media file"))
	_, ok = ProbeMediaDuration(srv.URL, time.Second)
	assert.False(t, ok)
}

// TestProbeMediaDurationBlocksLoopback 钉住 SSRF 护栏:生产 Control 下连环回
// (httptest 即环回)必须在 connect 阶段被拒,返回 (0,false)。
func TestProbeMediaDurationBlocksLoopback(t *testing.T) {
	srv := serveBytes(t, buildMinimalWav(3))
	d, ok := ProbeMediaDuration(srv.URL, 2*time.Second)
	assert.False(t, ok)
	assert.Equal(t, 0.0, d)
}

func TestProbeCacheRoundTrip(t *testing.T) {
	allowLoopbackProbe(t)

	srv := serveBytes(t, buildMinimalWav(2))
	_, ok := ProbeMediaDuration(srv.URL, 3*time.Second)
	require.True(t, ok)
	d, ok := CachedMediaDuration(srv.URL)
	require.True(t, ok)
	assert.InDelta(t, 2.0, d, 0.01)

	// 未探测过的 URL 与空 URL 不命中
	_, ok = CachedMediaDuration(srv.URL + "/never")
	assert.False(t, ok)
	_, ok = CachedMediaDuration("")
	assert.False(t, ok)
}

// TestProbeMediaDurationMp4InTail 钉住尾段路径:moov 在文件尾(mp4 常规封装)
// 时头段解不出,靠 Range 尾段拿 mvhd。服务器固定回 200 全量,这里直接喂解析
// 函数验证头/尾分工。
func TestProbeMediaDurationMp4InTail(t *testing.T) {
	fixture := buildMinimalMp4(1000, 5000)
	require.Greater(t, len(fixture), 8)
	head, tail := fixture[:8], fixture[8:]
	_, ok := parseMp4Duration(head, nil)
	assert.False(t, ok)
	d, ok := parseMp4Duration(head, tail)
	require.True(t, ok)
	assert.InDelta(t, 5.0, d, 0.01)
}

// TestProbeParseMp3XingDuration 钉住 mp3 Xing 路径:100 帧 × 1152 样本 /
// 44100Hz ≈ 2.612s;无 Xing 的帧头流不可靠(头段无文件总长),必须返回 false。
func TestProbeParseMp3XingDuration(t *testing.T) {
	d, ok := parseMp3Duration(buildXingMp3(100))
	require.True(t, ok)
	assert.InDelta(t, 2.612, d, 0.01)

	// 同样 4 字节帧头但没有 Xing/Info tag → false(不做 CBR 乱估)
	head := buildXingMp3(100)[:21]
	_, ok = parseMp3Duration(append(head, make([]byte, 32)...))
	assert.False(t, ok)

	_, ok = parseMp3Duration([]byte("definitely not mp3"))
	assert.False(t, ok)
}

// TestProbeParseWebmDuration 钉住 webm EBML 路径:Duration=7000 刻度 ×
// TimecodeScale 1e6ns = 7s;Duration 在尾段时同样可解。
func TestProbeParseWebmDuration(t *testing.T) {
	fixture := buildMinimalWebm(7000)
	d, ok := parseWebmDuration(fixture, nil)
	require.True(t, ok)
	assert.InDelta(t, 7.0, d, 0.01)

	d, ok = parseWebmDuration(nil, fixture)
	require.True(t, ok)
	assert.InDelta(t, 7.0, d, 0.01)

	_, ok = parseWebmDuration([]byte{0x1A, 0x45, 0xDF, 0xA3, 0x84, 0, 0, 0}, nil)
	assert.False(t, ok)
}

// TestParseWavDataSizeBeyondHead 钉住 wav 契约:data chunk 声明的 size 超出
// 头段长度也照算(size 在头里,不需要 data 本体)。
func TestParseWavDataSizeBeyondHead(t *testing.T) {
	fixture := buildMinimalWav(3)
	require.Greater(t, len(fixture), 44)
	head := fixture[:44] // 截到 data chunk 头,丢弃全部 data 本体
	d, ok := parseWavDuration(head)
	require.True(t, ok)
	assert.InDelta(t, 3.0, d, 0.01)
}
