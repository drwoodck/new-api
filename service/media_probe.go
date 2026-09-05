package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"math/bits"
	"net"
	"net/http"
	"net/netip"
	neturl "net/url"
	"strings"
	"sync"
	"syscall"
	"time"
)

// 探测行为常量:两段 Range 请求(头/尾各 256KB)——wav/mp3 的头在文件首,
// ISO mp4 的 moov 通常在文件尾;缓存 TTL 1h;并发上限 8。
const (
	mediaProbeChunkBytes = 256 * 1024
	// mediaProbeHeadRange/mediaProbeTailRange 与 mediaProbeChunkBytes-1 对应,
	// 固定字面量避免 strconv 依赖。
	mediaProbeHeadRange   = "bytes=0-262143"
	mediaProbeTailRange   = "bytes=-262143"
	mediaProbeMaxRedirect = 3
	mediaProbeCacheTTL    = time.Hour
	mediaProbeConcurrency = 8
	// mediaProbeMaxSeconds 是探测结果的可信上界:超过它视为解析误报(随机
	// 字节撞上 magic),不进缓存。计费侧另有 MaxTaskDurationSeconds 钳制。
	mediaProbeMaxSeconds = 24 * 60 * 60
)

var (
	// mediaProbeSem 限制同时在飞的探测请求数(含后台补探测),全程持锁。
	mediaProbeSem = make(chan struct{}, mediaProbeConcurrency)
	// mediaDurationCache key = sha256(url)(不落原始 URL),value = cachedMediaDuration。
	mediaDurationCache sync.Map
)

// cachedMediaDuration 是一条探测成果;expireAt 之前视为有效。
type cachedMediaDuration struct {
	duration float64
	expireAt time.Time
}

// errMediaProbePrivateAddr 是 Dialer Control 拒绝连接的统一错误。
var errMediaProbePrivateAddr = errors.New("media probe refused private/loopback/link-local/unspecified address")

// mediaProbeDialControl 是 SSRF 连接校验的测试接缝:生产恒为
// blockPrivateDialControl,不暴露任何配置项;测试用它临时放行 httptest 的
// 环回地址,护栏本身不可被运行时配置削弱。
var mediaProbeDialControl = blockPrivateDialControl

// blockPrivateDialControl 在 TCP connect 前校验目标地址。Dialer.Control 收到
// 的是已解析的 IP(含重定向目标),因此 DNS rebinding 与"重定向转内网"都在
// 这里被拦;校验不出 IP 时按拒绝处理(兜底拒绝)。
func blockPrivateDialControl(_ string, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip, ok := netip.AddrFromSlice(net.ParseIP(host))
	if !ok {
		return errMediaProbePrivateAddr
	}
	ip = ip.Unmap() // IPv4-mapped IPv6(::ffff:10.0.0.1)按 IPv4 判
	if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return errMediaProbePrivateAddr
	}
	// 100.64.0.0/10 CGNAT(运营商级 NAT)常被误当公网,一并拒绝
	if ip.Is4() {
		b := ip.As4()
		if b[0] == 100 && b[1] >= 64 && b[1] < 128 {
			return errMediaProbePrivateAddr
		}
	}
	return nil
}

// ProbeMediaDuration 通过头/尾各 256KB 的 Range 请求探测 URL 媒体时长(秒)。
// 解析顺序:先头段(wav/mp3/mp4/webm),头段解不出再补尾段(mp4 moov/webm
// 在尾)。SSRF 护栏:scheme 仅 http/https;Dialer Control 拒私网/环回/链路
// 本地/CGNAT/未指定地址;重定向 ≤3;io.LimitReader 与 Range 双保险;总超时
// = budget;并发信号量 8(阻塞获取;信号量忙时愿意放弃的调用方——如后台
// 补探测——应自行 try-acquire 后调 probeMediaDuration)。
//
// 全部失败路径返回 (0, false) 而非 error:探测失败 = 走系统默认估算的正常
// 路径,不是异常。成功结果写入 1h TTL 缓存,供同 URL 提交与结算修正复用。
func ProbeMediaDuration(url string, budget time.Duration) (float64, bool) {
	if url == "" || budget <= 0 {
		return 0, false
	}
	u, err := neturl.Parse(url)
	if err != nil {
		return 0, false
	}
	if scheme := strings.ToLower(u.Scheme); scheme != "http" && scheme != "https" {
		return 0, false
	}

	mediaProbeSem <- struct{}{}
	defer func() { <-mediaProbeSem }()

	return probeMediaDuration(url, budget)
}

// probeMediaDuration 是持锁后的探测主体:预算上下文、Range 抓取与各格式
// 解析。调用方必须已持有 mediaProbeSem(前台 ProbeMediaDuration 阻塞获取,
// 后台补探测 try-acquire)。
func probeMediaDuration(url string, budget time.Duration) (float64, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	// Proxy 显式置 nil:连接目标必须是 URL 本身的主机,Control 才能拦到真实
	// 目的地(环境代理会把探测流量指到别处,绕过校验)。
	transport := &http.Transport{
		Proxy:               nil,
		DialContext:         (&net.Dialer{Timeout: budget, Control: mediaProbeDialControl}).DialContext,
		TLSHandshakeTimeout: budget,
		DisableKeepAlives:   true,
	}
	client := &http.Client{
		Timeout:   budget,
		Transport: transport,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= mediaProbeMaxRedirect {
				return fmt.Errorf("media probe stopped after %d redirects", mediaProbeMaxRedirect)
			}
			return nil
		},
	}

	fetch := func(ranges string) []byte {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil
		}
		req.Header.Set("Range", ranges)
		req.Header.Set("User-Agent", "new-api-media-probe/1.0")
		resp, err := client.Do(req)
		if err != nil {
			return nil
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
			return nil
		}
		// LimitReader 双保险:服务器无视 Range 返回全量时也只读前 256KB。
		data, err := io.ReadAll(io.LimitReader(resp.Body, mediaProbeChunkBytes))
		if err != nil {
			return nil
		}
		return data
	}
	store := func(duration float64) (float64, bool) {
		sum := sha256.Sum256([]byte(url))
		mediaDurationCache.Store(string(sum[:]), cachedMediaDuration{
			duration: duration,
			expireAt: time.Now().Add(mediaProbeCacheTTL),
		})
		return duration, true
	}

	head := fetch(mediaProbeHeadRange)
	if len(head) > 0 {
		if d, ok := parseWavDuration(head); ok {
			return store(d)
		}
		if d, ok := parseMp3Duration(head); ok {
			return store(d)
		}
		if d, ok := parseMp4Duration(head, nil); ok {
			return store(d)
		}
		if d, ok := parseWebmDuration(head, nil); ok {
			return store(d)
		}
	}
	tail := fetch(mediaProbeTailRange)
	if len(tail) > 0 {
		if d, ok := parseMp4Duration(head, tail); ok {
			return store(d)
		}
		if d, ok := parseWebmDuration(head, tail); ok {
			return store(d)
		}
	}
	return 0, false
}

// CachedMediaDuration 返回此前探测成功且仍在 TTL 内的时长;未命中/过期返回
// (0, false)。
func CachedMediaDuration(url string) (float64, bool) {
	if url == "" {
		return 0, false
	}
	sum := sha256.Sum256([]byte(url))
	v, ok := mediaDurationCache.Load(string(sum[:]))
	if !ok {
		return 0, false
	}
	entry := v.(cachedMediaDuration)
	if time.Now().After(entry.expireAt) {
		mediaDurationCache.CompareAndDelete(string(sum[:]), entry)
		return 0, false
	}
	return entry.duration, true
}

// saneProbeSeconds 校验解析出的时长落在可信域 (0, 24h]:时长大得离谱只可能
// 是解析误报(随机字节撞上 magic),不能当真实时长缓存。
func saneProbeSeconds(seconds float64) (float64, bool) {
	if seconds <= 0 || seconds > mediaProbeMaxSeconds || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return 0, false
	}
	return seconds, true
}

// parseMp4Duration 在头/尾两段内找 moov→mvhd box 并解出 duration/timescale。
// mvhd 可能整体落在头段(快速启动文件)或尾段(常规封装),故两段都扫。
func parseMp4Duration(head, tail []byte) (float64, bool) {
	if d, ok := mvhdDuration(head); ok {
		return d, true
	}
	return mvhdDuration(tail)
}

var mp4MvhdMagic = []byte("mvhd")

// mvhdDuration 在数据里扫描 mvhd 签名并按 ISO BMFF 解析:
//
//	'mvhd'(4) version(1) flags(3) creation(4/8) modification(4/8)
//	timescale(4) duration(4|8)
//
// version 0 用 uint32,version 1 用 uint64;duration=0 或 timescale=0 由
// saneProbeSeconds 以 0/+Inf/NaN 兜底拒绝。
func mvhdDuration(data []byte) (float64, bool) {
	for pos := 0; pos < len(data); {
		idx := bytes.Index(data[pos:], mp4MvhdMagic)
		if idx < 0 {
			return 0, false
		}
		pos += idx
		if pos+8 <= len(data) {
			switch version := data[pos+4]; version {
			case 0:
				if pos+24 <= len(data) {
					timescale := binary.BigEndian.Uint32(data[pos+16 : pos+20])
					duration := binary.BigEndian.Uint32(data[pos+20 : pos+24])
					if seconds, ok := saneProbeSeconds(float64(duration) / float64(timescale)); ok {
						return seconds, true
					}
				}
			case 1:
				if pos+36 <= len(data) {
					timescale := binary.BigEndian.Uint32(data[pos+24 : pos+28])
					duration := binary.BigEndian.Uint64(data[pos+28 : pos+36])
					if seconds, ok := saneProbeSeconds(float64(duration) / float64(timescale)); ok {
						return seconds, true
					}
				}
			}
		}
		pos += 4
	}
	return 0, false
}

// parseWavDuration 解析 RIFF 头:fmt chunk 的 byteRate 与 data chunk 的
// size。chunk size 从头里读,data 本体超过头段长度不影响计算(头段 256KB
// 足够容纳真实文件的 fmt+data 声明)。
func parseWavDuration(head []byte) (float64, bool) {
	if len(head) < 12 || string(head[0:4]) != "RIFF" || string(head[8:12]) != "WAVE" {
		return 0, false
	}
	var byteRate, dataSize uint64
	haveFmt, haveData := false, false
	for pos := 12; pos+8 <= len(head); {
		id := string(head[pos : pos+4])
		size := uint64(binary.LittleEndian.Uint32(head[pos+4 : pos+8]))
		switch id {
		case "fmt ":
			// fmt chunk: audioFormat(2) channels(2) sampleRate(4) byteRate(4) ...
			if pos+8+16 <= len(head) {
				byteRate = uint64(binary.LittleEndian.Uint32(head[pos+16 : pos+20]))
				haveFmt = true
			}
		case "data":
			dataSize = size
			haveData = true
		}
		if haveFmt && haveData {
			break
		}
		// RIFF chunk 按 2 字节对齐;推进用 int64 计算并在 size 越界(超过剩余
		// 长度)时停止——32 位平台上 int(uint32) 会回绕成负数,直接推进会造成
		// 负下标/死循环。
		next := int64(pos) + 8 + int64(size) + int64(size&1)
		if next > int64(len(head)) {
			break
		}
		pos = int(next)
	}
	if !haveFmt || !haveData {
		return 0, false
	}
	return saneProbeSeconds(float64(dataSize) / float64(byteRate))
}

// mp3SampleRates[versionRow][index]:MPEG1/MPEG2/MPEG2.5 三行。
var mp3SampleRates = [3][3]int{
	{44100, 48000, 32000}, // MPEG 1
	{22050, 24000, 16000}, // MPEG 2
	{11025, 12000, 8000},  // MPEG 2.5
}

// parseMp3Duration 跳过 ID3v2 后找首个帧头,取 Xing/Info tag 的总帧数
// (frames × samplesPerFrame / sampleRate)。无 tag(VBR 无头/CBR)时头段
// 只有前 256KB、不知道文件总长,推算不可靠,直接返回 false 走估算路径。
func parseMp3Duration(head []byte) (float64, bool) {
	pos := 0
	if len(head) >= 10 && string(head[0:3]) == "ID3" {
		id3Size := uint32(head[6]&0x7F)<<21 | uint32(head[7]&0x7F)<<14 |
			uint32(head[8]&0x7F)<<7 | uint32(head[9]&0x7F)
		pos = 10 + int(id3Size)
		if pos < 0 { // id3Size 溢出 int 的防御
			return 0, false
		}
	}
	for ; pos+4 <= len(head); pos++ {
		if head[pos] != 0xFF || head[pos+1]&0xE0 != 0xE0 {
			continue
		}
		if d, ok := mp3FrameDuration(head, pos); ok {
			return d, true
		}
	}
	return 0, false
}

// mp3FrameDuration 从 data[pos] 起的 4 字节帧头解出采样率与每帧样本数,并在
// 侧信息之后找 Xing/Info tag 读总帧数。position 不是合法帧头时返回 false,
// 由调用方继续扫描。
func mp3FrameDuration(data []byte, pos int) (float64, bool) {
	if pos+4 > len(data) {
		return 0, false
	}
	version := int(data[pos+1]>>3) & 0x3 // 3=MPEG1 2=MPEG2 0=MPEG2.5 1=保留
	layer := int(data[pos+1]>>1) & 0x3   // 3=Layer I 2=Layer II 1=Layer III 0=保留
	bitrateIdx := int(data[pos+2]) >> 4
	sampleIdx := int(data[pos+2]>>2) & 0x3
	if version == 1 || layer == 0 || bitrateIdx == 0 || bitrateIdx == 15 || sampleIdx == 3 {
		return 0, false
	}
	sampleRow := 0
	switch version {
	case 2:
		sampleRow = 1
	case 0:
		sampleRow = 2
	}
	sampleRate := mp3SampleRates[sampleRow][sampleIdx]
	var samplesPerFrame int
	switch {
	case layer == 3:
		samplesPerFrame = 384 // Layer I
	case layer == 2:
		samplesPerFrame = 1152 // Layer II
	case version == 3:
		samplesPerFrame = 1152 // Layer III MPEG1
	default:
		samplesPerFrame = 576 // Layer III MPEG2/2.5
	}

	// Xing/Info 紧跟 MPEG1 L3 的 17/32 字节、MPEG2/2.5 L3 的 9/17 字节侧信息。
	sideInfo := 0
	if layer == 1 {
		channels := int(data[pos+3]>>6) & 0x3 // 3 = mono
		switch {
		case version == 3 && channels == 3:
			sideInfo = 17
		case version == 3:
			sideInfo = 32
		case channels == 3:
			sideInfo = 9
		default:
			sideInfo = 17
		}
	}
	x := pos + 4 + sideInfo
	if x+12 > len(data) {
		return 0, false
	}
	tag := string(data[x : x+4])
	if tag != "Xing" && tag != "Info" {
		return 0, false
	}
	flags := binary.BigEndian.Uint32(data[x+4 : x+8])
	if flags&0x1 == 0 { // frames 字段不存在
		return 0, false
	}
	frames := binary.BigEndian.Uint32(data[x+8 : x+12])
	if frames == 0 {
		return 0, false
	}
	return saneProbeSeconds(float64(frames) * float64(samplesPerFrame) / float64(sampleRate))
}

var (
	webmEBMLMagic       = []byte{0x1A, 0x45, 0xDF, 0xA3}
	webmDurationID      = []byte{0x44, 0x89}
	webmTimecodeScaleID = []byte{0x2A, 0xD7, 0xB1}
)

// parseWebmDuration 解析 Matroska/WebM 的 Segment Info:Duration(float,单位
// 为 TimecodeScale 刻度)× TimecodeScale(默认 1e6 ns)。Duration 可能在头段
// (小文件)或尾段(muxer 常在收尾时写入 Info/Cues),两段都扫。
func parseWebmDuration(head, tail []byte) (float64, bool) {
	// EBML 头恒为 Matroska 文件的首元素:头段不以 EBML magic 开头就不是
	// webm,不进 ID 扫描(2/3 字节 ID 在任意非媒体数据里都会随机命中)。
	if !bytes.HasPrefix(head, webmEBMLMagic) {
		return 0, false
	}
	scale := 1e6
	if s, ok := webmUint(head, webmTimecodeScaleID); ok {
		scale = float64(s)
	} else if s, ok := webmUint(tail, webmTimecodeScaleID); ok {
		scale = float64(s)
	}
	raw, ok := webmFloat(head, webmDurationID)
	if !ok {
		raw, ok = webmFloat(tail, webmDurationID)
	}
	if !ok {
		return 0, false
	}
	return saneProbeSeconds(raw * scale / 1e9)
}

// webmFloat 在 data 里扫描指定 EBML ID,解出 float32/float64 载荷。
func webmFloat(data, id []byte) (float64, bool) {
	for pos := 0; pos < len(data); {
		idx := bytes.Index(data[pos:], id)
		if idx < 0 {
			return 0, false
		}
		pos += idx
		if size, next, ok := ebmlVintSize(data, pos+len(id)); ok && (size == 4 || size == 8) && next+size <= len(data) {
			var f float64
			if size == 4 {
				f = float64(math.Float32frombits(binary.BigEndian.Uint32(data[next : next+size])))
			} else {
				f = math.Float64frombits(binary.BigEndian.Uint64(data[next : next+size]))
			}
			if !math.IsNaN(f) && !math.IsInf(f, 0) && f > 0 && f <= 1e15 {
				return f, true
			}
		}
		pos++
	}
	return 0, false
}

// webmUint 在 data 里扫描指定 EBML ID,解出 uint 载荷(1..8 字节)。
func webmUint(data, id []byte) (uint64, bool) {
	for pos := 0; pos < len(data); {
		idx := bytes.Index(data[pos:], id)
		if idx < 0 {
			return 0, false
		}
		pos += idx
		if size, next, ok := ebmlVintSize(data, pos+len(id)); ok && size >= 1 && size <= 8 && next+size <= len(data) {
			var v uint64
			for _, b := range data[next : next+size] {
				v = v<<8 | uint64(b)
			}
			if v > 0 && v <= 1e12 { // TimecodeScale 实际取值远小于此上界
				return v, true
			}
		}
		pos++
	}
	return 0, false
}

// ebmlVintSize 解析 EBML vint(元素 ID 之后),返回其编码的长度值与载荷起始
// 偏移。首字节为 0 不是合法 vint 起始(长度标记位被吞),按失败处理。
func ebmlVintSize(data []byte, pos int) (int, int, bool) {
	if pos >= len(data) || data[pos] == 0 {
		return 0, 0, false
	}
	length := bits.LeadingZeros8(data[pos]) + 1
	if length > 8 || pos+length > len(data) {
		return 0, 0, false
	}
	value := int(data[pos] & (0xFF >> uint(length)))
	for i := 1; i < length; i++ {
		value = value<<8 | int(data[pos+i])
	}
	return value, pos + length, true
}
