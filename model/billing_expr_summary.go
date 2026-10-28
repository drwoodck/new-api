package model

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/setting/billing_setting"
)

// billingExprSlots 是表达式里会被计价的 token 维度 → 展示名,顺序即展示顺序:
// 先输入/输出,再缓存,最后多模态。与 pkg/billingexpr/types.go 的 TokenParams
// 一一对应。
//
// 注意这里只能列 **TokenParams 现有字段**:pkg/billingexpr 正在补 `img_cr`
// (缓存图片输入单独计价),那个字段还没合进来,提前引用会让本文件在 HEAD 上编不过。
// 表达式里出现未知标识符时 billingexpr 会编译失败,DescribeBillingExpr 返回
// false、退回一句「阶梯表达式计费」—— 不会崩,也不会编错价。
var billingExprSlots = []struct {
	Var   string
	Label string
}{
	{"p", "输入"},
	{"c", "输出"},
	{"cr", "缓存命中"},
	{"cc", "缓存写入"},
	{"cc1h", "缓存写入1h"},
	{"img", "图片输入"},
	{"img_o", "图片输出"},
	{"ai", "音频输入"},
	{"ao", "音频输出"},
}

// exprProbeTokens 是「每维取多少 token」来反推单价。1M 是为了让
// `p * coeff` 直接等于「每百万 token 的价格」:表达式系数本身就是 $/1M,
// 乘 1e6 个 token 再除回 1e6 就是系数本身(billingexpr 的 v1 口径)。
const exprProbeTokens = 1_000_000

// UsesTieredExprBilling 判断这个模型是否走「阶梯表达式」计费。
//
// 这类模型的价格由表达式按上下文长度/缓存/图片等维度现算,**没有单一标价**。
// 目录下发价格时必须能识别它:否则会掉进按倍率的分支,把 models.model_ratio
// 这个与计费无关的占位值(线上是 37.5)当成真倍率,画布就显示「$75/M 输入」
// —— 一个任何用户都不会被收取的价格。
func UsesTieredExprBilling(modelName string) bool {
	if billing_setting.GetBillingMode(modelName) != billing_setting.BillingModeTieredExpr {
		return false
	}
	expr, ok := billing_setting.GetBillingExpr(modelName)
	return ok && strings.TrimSpace(expr) != ""
}

// DescribeBillingExpr 把阶梯表达式渲染成用户能看懂的价格摘要,例如:
//
//	上下文≤272K（输入$0.9625/M 输出$4.8125/M）；其余（输入$1.54/M 输出$5.775/M）
//
// 第二个返回值为 false 表示这条表达式**没有可展示的固定价格**(版本不认识、
// 条件不是上下文阈值、表达式结构不认识、或同一档内价格随请求头/时间变化)——
// 调用方应退回一句「阶梯表达式计费」,而不是编一个数。
//
// 为什么不用正则从表达式里抠系数:系数会随 `tier()`、条件、请求倍率变化,
// 同一份文本里可能出现 max/min/条件嵌套。这里用 AST 只取「结构」(有哪几档、
// 每档的条件是什么),价格一律交给 billingexpr 自己算 —— 展示与计费共用同一个
// 求值器,不会出现「宣传价 ≠ 实收价」。
func DescribeBillingExpr(modelName string) (string, bool) {
	if !UsesTieredExprBilling(modelName) {
		return "", false
	}
	raw, _ := billing_setting.GetBillingExpr(modelName)
	version, body := billingexpr.ParseExprVersion(raw)
	// v1 是当前唯一口径(系数 = $/1M,quota = cost/1e6)。将来加新版本时,
	// 这里必须跟着加换算,否则会把新版系数当 v1 报出去。
	if version != 1 {
		return "", false
	}
	// 请求级倍率追加在 `|||` 之后(前端编辑器拼的乘法),展示的是基础价,
	// 这一段不参与。
	if idx := strings.Index(body, "|||"); idx >= 0 {
		body = body[:idx]
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return "", false
	}

	tree, err := parser.Parse(body)
	if err != nil {
		return "", false
	}
	branches, ok := billingExprBranches(tree.Node)
	if !ok || len(branches) == 0 {
		return "", false
	}

	parts := make([]string, 0, len(branches))
	for _, branch := range branches {
		prices, ok := billingExprBranchPrices(body, branch)
		if !ok || len(prices) == 0 {
			return "", false
		}
		// 多档(上下文阶梯)只列输入/输出:阶梯说明本来就已经很长,再挂上
		// 缓存读/缓存写会让画布那一行彻底看不清,而用户比价只看输入/输出两列。
		// 单档表达式(图片/多模态)保留全部维度 —— 那里缓存与图片就是价格主体。
		if len(branches) > 1 {
			prices = billingExprPrimaryPrices(prices)
			if len(prices) == 0 {
				return "", false
			}
		}
		if branch.Label == "" {
			parts = append(parts, strings.Join(prices, " "))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s（%s）", branch.Label, strings.Join(prices, " ")))
	}
	summary := strings.Join(parts, "；")
	if billingExprUsesRequestProbe(body) {
		// 表达式里挂了 `header(...)/param(...)/hour(...)` 这类请求级倍率(例如
		// fast 模式 ×6):上面列的是基础价。加一句提示,免得用户按基础价估算后
		// 被账单吓一跳。
		summary += "（部分请求另有倍率）"
	}
	return summary, true
}

// billingExprPrimaryPrices 只留「输入」「输出」两项(阶梯说明的紧凑形态)。
// 一个维度都没有时返回空切片,由调用方回退到完整列表。
func billingExprPrimaryPrices(prices []string) []string {
	kept := make([]string, 0, 2)
	for _, price := range prices {
		if strings.HasPrefix(price, "输入$") || strings.HasPrefix(price, "输出$") {
			kept = append(kept, price)
		}
	}
	return kept
}

// billingExprUsesRequestProbe 判断表达式是否引用了请求级探测函数。
func billingExprUsesRequestProbe(body string) bool {
	vars := billingexpr.UsedVars(body)
	for _, name := range []string{"header", "param", "hour", "minute", "weekday", "month", "day"} {
		if vars[name] {
			return true
		}
	}
	return false
}

// billingExprBranch 是表达式里的一档:人话条件 + 用来探测该档价格的上下文长度。
type billingExprBranch struct {
	Label string
	// ProbeLen 是「落进这一档」的上下文长度。表达式里可能有多档、
	// 每档条件不同,所以每档各自用一个满足自己条件的 len 去求值。
	ProbeLen float64
	// TierName 是表达式里 tier("…") 的标签,用来验证 ProbeLen 真的选中了这一档。
	TierName string
}

// billingExprBranches 把「条件 ? A : 条件 ? B : C」拆成有序的档。
//
// 只认 `len` 与常量的比较(这正是「上下文阶梯」的表达方式)。别的条件形态
// (按请求头/按小时变价)没有单一价可展示 → 返回 false。
func billingExprBranches(node ast.Node) ([]billingExprBranch, bool) {
	var branches []billingExprBranch
	lower, upper := math.Inf(-1), math.Inf(1)
	cur := node
	for {
		cond, isCond := cur.(*ast.ConditionalNode)
		if !isCond || !cond.Ternary {
			break
		}
		op, threshold, ok := billingExprLenCond(cond.Cond)
		if !ok {
			return nil, false
		}
		branches = append(branches, billingExprBranch{
			Label:    billingExprCondLabel(op, threshold),
			ProbeLen: math.Min(math.Max(probeForOp(op, threshold), lower), upper),
			TierName: billingExprTierName(cond.Exp1),
		})
		switch op {
		case "<=", "<":
			upper = math.Min(upper, threshold)
		case ">=", ">":
			lower = math.Max(lower, threshold)
		}
		cur = cond.Exp2
	}

	if len(branches) == 0 {
		// 没有条件:整份固定价,单档。
		return []billingExprBranch{{TierName: billingExprTierName(cur)}}, true
	}
	// 最后的 else 分支:落在所有条件之外 —— 取已见阈值中「最小可行」的位置。
	probe := upper
	if math.IsInf(probe, 0) {
		probe = lower
	} else {
		probe = math.Max(probe-1, lower+1)
	}
	if math.IsInf(probe, 0) {
		probe = 0
	}
	branches = append(branches, billingExprBranch{
		Label:    "其余",
		ProbeLen: probe,
		TierName: billingExprTierName(cur),
	})
	return branches, true
}

// billingExprLenCond 取 `len <= N` 形态的条件。`N >= len` 这种反写也认。
func billingExprLenCond(node ast.Node) (string, float64, bool) {
	bin, ok := node.(*ast.BinaryNode)
	if !ok {
		return "", 0, false
	}
	switch bin.Operator {
	case "<=", "<", ">=", ">":
	default:
		return "", 0, false
	}
	if id, isID := bin.Left.(*ast.IdentifierNode); isID && id.Value == "len" {
		value, valueOK := billingExprNumber(bin.Right)
		if !valueOK {
			return "", 0, false
		}
		return bin.Operator, value, true
	}
	if id, isID := bin.Right.(*ast.IdentifierNode); isID && id.Value == "len" {
		value, valueOK := billingExprNumber(bin.Left)
		if !valueOK {
			return "", 0, false
		}
		return flipComparison(bin.Operator), value, true
	}
	return "", 0, false
}

func flipComparison(op string) string {
	switch op {
	case "<=":
		return ">="
	case "<":
		return ">"
	case ">=":
		return "<="
	case ">":
		return "<"
	}
	return op
}

func billingExprNumber(node ast.Node) (float64, bool) {
	switch value := node.(type) {
	case *ast.IntegerNode:
		return float64(value.Value), true
	case *ast.FloatNode:
		return value.Value, true
	}
	return 0, false
}

// probeForOp 给出「刚好满足该条件」的一个上下文长度。
func probeForOp(op string, threshold float64) float64 {
	switch op {
	case "<":
		return threshold - 1
	case ">":
		return threshold + 1
	default:
		return threshold
	}
}

func billingExprCondLabel(op string, threshold float64) string {
	symbol := op
	switch op {
	case "<=":
		symbol = "≤"
	case ">=":
		symbol = "≥"
	}
	return "上下文" + symbol + billingExprTokenCount(threshold)
}

// billingExprTokenCount 把 token 数写成人话:272000 → "272K",1500000 → "1.5M"。
func billingExprTokenCount(tokens float64) string {
	switch {
	case tokens >= 1_000_000:
		return trimTrailingZeros(tokens/1_000_000) + "M"
	case tokens >= 1_000:
		return trimTrailingZeros(tokens/1_000) + "K"
	default:
		return trimTrailingZeros(tokens)
	}
}

func trimTrailingZeros(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// billingExprTierName 取 `tier("name", …)` 里的 name;不是 tier 调用则返回空串。
func billingExprTierName(node ast.Node) string {
	call, ok := node.(*ast.CallNode)
	if !ok || len(call.Arguments) == 0 {
		return ""
	}
	callee, ok := call.Callee.(*ast.IdentifierNode)
	if !ok || callee.Value != "tier" {
		return ""
	}
	name, ok := call.Arguments[0].(*ast.StringNode)
	if !ok {
		return ""
	}
	return name.Value
}

// billingExprBranchPrices 用 billingexpr 自己求出该档每个维度的「每百万 token
// 单价」,输出成 "输入$0.9625/M" 这样的片段(只列非零维度)。
//
// ProbeLen 用候选值逐个试,并用 trace 的 MatchedTier 校验真的落进了目标档 ——
// 条件可能是 <= / >= 混用,单靠算阈值容易挑错档。
func billingExprBranchPrices(expr string, branch billingExprBranch) ([]string, bool) {
	probeLens := []float64{branch.ProbeLen, 0, 1}
	if branch.ProbeLen > 0 {
		probeLens = append(probeLens, branch.ProbeLen+1, branch.ProbeLen*2, branch.ProbeLen/2)
	}

	probeLen := math.NaN()
	for _, candidate := range probeLens {
		if candidate < 0 {
			continue
		}
		_, trace, err := billingexpr.RunExpr(expr, billingexpr.TokenParams{Len: candidate})
		if err != nil {
			return nil, false
		}
		if branch.TierName == "" || trace.MatchedTier == branch.TierName {
			probeLen = candidate
			break
		}
	}
	if math.IsNaN(probeLen) {
		return nil, false
	}

	out := make([]string, 0, len(billingExprSlots))
	for _, slot := range billingExprSlots {
		params := billingexpr.TokenParams{Len: probeLen}
		switch slot.Var {
		case "p":
			params.P = exprProbeTokens
		case "c":
			params.C = exprProbeTokens
		case "cr":
			params.CR = exprProbeTokens
		case "cc":
			params.CC = exprProbeTokens
		case "cc1h":
			params.CC1h = exprProbeTokens
		case "img":
			params.Img = exprProbeTokens
		case "img_o":
			params.ImgO = exprProbeTokens
		case "ai":
			params.AI = exprProbeTokens
		case "ao":
			params.AO = exprProbeTokens
		}
		cost, _, err := billingexpr.RunExpr(expr, params)
		if err != nil {
			return nil, false
		}
		perMillion := cost / exprProbeTokens
		// 阈值取 1e-9:一个维度没被计价时 cost 恒为 0,而真实的极小单价
		// (比如 $0.000001/M)也不该被四舍五入掉。
		if math.Abs(perMillion) < 1e-9 {
			continue
		}
		out = append(out, fmt.Sprintf("%s$%s/M", slot.Label, formatExprPrice(perMillion)))
	}
	return out, true
}

// formatExprPrice 把 $/M 单价写成紧凑数字:0.9625 → "0.9625",1.54 → "1.54",
// 4.8125 → "4.8125"。用 6 位有效数字,足够表达 provider 的报价精度,又能把
// 0.00017999999999999998 这种浮点尾巴收敛成 0.00018。
func formatExprPrice(price float64) string {
	return strconv.FormatFloat(price, 'g', 6, 64)
}
