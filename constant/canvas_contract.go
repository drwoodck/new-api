package constant

// ContractForCapability 把画布目录条目的 capability 映射到画布客户端的契约模板 id。
//
// 画布侧总共只有两个契约(src-tauri/src/relay/contracts.rs 的 SUPPORTED_CONTRACTS:
// relay_video_async_v1、relay_image_async_v1),与 capability 一一对应,所以显式表就够 ——
// 不走 supported_endpoint_types 推导。那个值是 common.GetEndpointTypesByChannelType
// 按渠道类型算出来再对启用的 abilities 聚合的结果,是「这个渠道能力上支持什么协议」,
// 不是「这个模型该用哪个画布契约」的声明,两者不是一一对应关系。
//
// 新增契约时这里要同步加一行,且必须先确认画布客户端已发版支持 ——
// 上线前用 GET /api/canvas_catalog/contract-stats 看在线客户端的支持率,
// 支持率为 0 就先别上架,那等于挂了个谁都调不动的模型。
var capabilityToContract = map[string]string{
	"video_gen": "relay_video_async_v1",
	"image_gen": "relay_image_async_v1",
}

// ContractForCapability 返回 capability 对应的默认契约。
// 第二个返回值为 false 时表示映射表里没有这个 capability —— 调用方不应该猜,
// 应留空并提示管理员手填 contract。
func ContractForCapability(capability string) (string, bool) {
	contract, ok := capabilityToContract[capability]
	return contract, ok
}

// IsSupportedContract 判断某个 contract 字符串是否在画布客户端支持的清单内 ——
// 即 capabilityToContract 的值集合,不是键集合。用于目录条目"画布是否真的能用
// 这一条"的判定(未知 contract 画布会把整条目录条目跳过,见 sync/catalog.rs 的
// UnknownContract 分支),不要为这个判断另建一份契约常量,两份清单迟早会漏同步。
func IsSupportedContract(contract string) bool {
	for _, c := range capabilityToContract {
		if c == contract {
			return true
		}
	}
	return false
}
