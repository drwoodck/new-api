// 把画布 profiles 里的 param_schema 导入中转站的 model_metadata 表。
//
// 用法:
//
//	go run scripts/import_param_schemas.go -canvas E:\githubxiangmu\myhuabua [-dry-run]
//
// 为什么需要名字映射(而不是直接用 endpoint TOML 里的 model_id):
// 目录端点用 canvas_catalog_models.remote_id 去 model_metadata 里查 schema,
// 而画布 endpoint TOML 里写的是画布内部 id,两者不是一个名字 ——
// 例如 model_id="paipu-ac-image-2-5-flare" 对应的 remote_id 是
// "lec-ac-image-2-5-flare"。桥梁是 providers/*.toml 的 [[models]] 里的
// id / remote_id 两个字段。名字对不上时查表会静默落空(节点面板照旧显示
// 模板默认参数,没有任何报错),所以下面导入前会跟目录表交叉校验。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/QuantumNous/new-api/model"
	"github.com/pelletier/go-toml/v2"
)

// providerToml 只取画布内部 id 与中转站 remote_id 两组键。
type providerToml struct {
	Models []struct {
		ID       string `toml:"id"`
		RemoteID string `toml:"remote_id"`
	} `toml:"models"`
}

// endpointToml 只取挂载的 model_id 与 param_schema。
//
// 注意 param_schema 在 TOML 里是「只有 json 一个键的表」,值是 JSON 字符串:
//
//	[param_schema]
//	json = '{"prompt":{...},"duration":{...}}'
//
// 不能按嵌套表解析,否则会多包一层 {"json": "..."},画布拿到的就不是真 schema。
type endpointToml struct {
	Profile struct {
		ModelID string `toml:"model_id"`
	} `toml:"profile"`
	ParamSchema struct {
		JSON string `toml:"json"`
	} `toml:"param_schema"`
}

type candidate struct {
	canvasID string
	remoteID string
	schema   string
}

func main() {
	canvasDir := flag.String("canvas", "", "画布项目根目录路径")
	dryRun := flag.Bool("dry-run", false, "只报告将要写入的内容,不实际写库")
	flag.Parse()

	if *canvasDir == "" {
		fmt.Println("用法: go run scripts/import_param_schemas.go -canvas E:\\githubxiangmu\\myhuabua [-dry-run]")
		os.Exit(1)
	}

	if err := run(*canvasDir, *dryRun); err != nil {
		fmt.Fprintf(os.Stderr, "\n错误: %v\n", err)
		os.Exit(1)
	}
}

func run(canvasDir string, dryRun bool) error {
	profilesDir := filepath.Join(canvasDir, "src-tauri", "profiles")

	// 1) provider 表:画布内部 id -> 中转站 remote_id
	idToRemote, err := loadProviderIDs(filepath.Join(profilesDir, "providers"))
	if err != nil {
		return err
	}
	fmt.Printf("provider 映射: %d 组 (画布 id <-> remote_id)\n", len(idToRemote))

	// 2) endpoint 表:画布内部 id -> param_schema(JSON 字符串)
	idToSchema, invalidSchemas, err := loadEndpointSchemas(filepath.Join(profilesDir, "endpoints"))
	if err != nil {
		return err
	}
	fmt.Printf("endpoint schema: %d 个可用", len(idToSchema))
	if len(invalidSchemas) > 0 {
		fmt.Printf(", %d 个非法(已跳过): %v", len(invalidSchemas), invalidSchemas)
	}
	fmt.Println()

	// 3) 目录表:中转站实际在卖的 remote_id。交叉校验的基准。
	if err := model.InitDB(); err != nil {
		return fmt.Errorf("连接数据库失败: %w", err)
	}
	defer model.CloseDB()

	var catalogIDs []string
	if err := model.DB.Model(&model.CanvasCatalogModel{}).Pluck("remote_id", &catalogIDs).Error; err != nil {
		return fmt.Errorf("读取 canvas_catalog_models 失败: %w", err)
	}
	// 4) 关联:provider 表里 remote_id 必须在目录表,且画布有对应 schema
	plan := buildImportPlan(idToRemote, idToSchema, catalogIDs)
	toImport := plan.toImport
	fmt.Printf("目录表 canvas_catalog_models: %d 个 remote_id\n", plan.catalogCount)

	fmt.Println()
	fmt.Printf("=== 将导入 %d 个 ===\n", len(toImport))
	for _, c := range toImport {
		fmt.Printf("  %s  ->  %s  (schema %d 字节)\n", c.canvasID, c.remoteID, len(c.schema))
	}

	if len(plan.notInCatalog) > 0 {
		fmt.Printf("\n=== 画布有 provider 映射、但目录表里没有 (%d 个,跳过) ===\n", len(plan.notInCatalog))
		printLimited(plan.notInCatalog)
	}
	if len(plan.noCanvasSchema) > 0 {
		fmt.Printf("\n=== provider 表有、画布无 endpoint schema (%d 个,跳过) ===\n", len(plan.noCanvasSchema))
		printLimited(plan.noCanvasSchema)
	}
	if len(plan.catalogWithoutSchema) > 0 {
		fmt.Printf("\n=== 目录在卖、但这次没有 schema 可导入 (%d 个,将沿用模板默认参数) ===\n", len(plan.catalogWithoutSchema))
		printLimited(plan.catalogWithoutSchema)
	}

	// 一个都没匹配上,说明两边命名规则对不上 —— 多半是前缀改了。
	// 这时静默返回「导入 0 条」最危险,必须显式失败。
	if len(toImport) == 0 {
		return fmt.Errorf(
			"没有任何条目进入导入集:目录表有 %d 个 remote_id,provider 表有 %d 组映射,但两者无交集。\n"+
				"这通常意味着两边命名规则不一致(例如前缀不同)。请检查 canvas_catalog_models.remote_id "+
				"与画布 providers/*.toml 里的 remote_id 是否同一套命名",
			plan.catalogCount, len(idToRemote))
	}

	if dryRun {
		fmt.Println("\n[dry-run] 未写库。去掉 -dry-run 执行实际导入。")
		return nil
	}

	metadatas := make([]*model.ModelMetadata, 0, len(toImport))
	for _, c := range toImport {
		schema := c.schema
		metadatas = append(metadatas, &model.ModelMetadata{
			ModelName:   c.remoteID,
			ParamSchema: &schema,
		})
	}
	if err := model.BatchUpsertModelMetadata(metadatas); err != nil {
		return fmt.Errorf("写库失败: %w", err)
	}
	fmt.Printf("\n导入完成: 写入 %d 条\n", len(metadatas))
	return nil
}

// importPlan 是一次导入的关联结果。
// 抽成纯函数是为了让它可测 —— 名字对不上时这里会静默产出空集,
// 那是整个功能最容易无声失效的地方。
type importPlan struct {
	catalogCount         int // 目录表去重后的 remote_id 数
	toImport             []candidate
	notInCatalog         []string // provider 有映射、目录表没有
	noCanvasSchema       []string // provider 有映射、画布无 endpoint schema
	catalogWithoutSchema []string // 目录在卖、但没有 schema 可导入
}

// buildImportPlan 关联三份数据:
// provider 映射(画布 id → remote_id) × endpoint schema(画布 id → schema) × 目录表。
//
// 只有当 remote_id 确实出现在目录表里、且画布有对应 schema 时才进导入集 ——
// 目录表是「中转站真的在卖什么」的唯一真源,用它过滤可以避免把早已下架、
// 或从未上架的模型的 schema 塞进库。
func buildImportPlan(idToRemote, idToSchema map[string]string, catalogIDs []string) importPlan {
	catalogSet := make(map[string]struct{}, len(catalogIDs))
	for _, id := range catalogIDs {
		catalogSet[id] = struct{}{}
	}

	plan := importPlan{catalogCount: len(catalogSet)}
	covered := make(map[string]struct{})

	for canvasID, remoteID := range idToRemote {
		schema, ok := idToSchema[canvasID]
		if !ok {
			plan.noCanvasSchema = append(plan.noCanvasSchema, canvasID)
			continue
		}
		if _, ok := catalogSet[remoteID]; !ok {
			plan.notInCatalog = append(plan.notInCatalog, remoteID)
			continue
		}
		plan.toImport = append(plan.toImport, candidate{canvasID: canvasID, remoteID: remoteID, schema: schema})
		covered[remoteID] = struct{}{}
	}

	// 目录里有、但这次没有 schema 可导入的 —— 它们会继续用契约模板的默认参数,
	// 正是本次要消灭的「参数不对」现象,所以单独列出来提醒。
	for id := range catalogSet {
		if _, ok := covered[id]; !ok {
			plan.catalogWithoutSchema = append(plan.catalogWithoutSchema, id)
		}
	}

	// 排序只为输出稳定,让两次运行的报告可以直接 diff
	sort.Slice(plan.toImport, func(i, j int) bool { return plan.toImport[i].remoteID < plan.toImport[j].remoteID })
	sort.Strings(plan.notInCatalog)
	sort.Strings(plan.noCanvasSchema)
	sort.Strings(plan.catalogWithoutSchema)
	return plan
}

// loadProviderIDs 汇总 providers/ 下所有 TOML 的 [[models]] id / remote_id 映射。
func loadProviderIDs(dir string) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("读取 %s 失败: %w", dir, err)
	}

	result := make(map[string]string)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".toml" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("读取 %s 失败: %w", path, err)
		}
		var p providerToml
		if err := toml.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("解析 %s 失败: %w", path, err)
		}
		for _, m := range p.Models {
			if m.ID == "" || m.RemoteID == "" {
				continue
			}
			result[m.ID] = m.RemoteID
		}
	}
	return result, nil
}

// loadEndpointSchemas 汇总 endpoints/ 下所有 TOML 的 model_id -> param_schema。
// 返回的第二个值是 schema 不是合法 JSON 的 model_id 列表(这些会被跳过而不是写进库)。
func loadEndpointSchemas(dir string) (map[string]string, []string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("读取 %s 失败: %w", dir, err)
	}

	result := make(map[string]string)
	var invalid []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".toml" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, fmt.Errorf("读取 %s 失败: %w", path, err)
		}
		var ep endpointToml
		if err := toml.Unmarshal(raw, &ep); err != nil {
			// 画布目录里混杂着别家 provider 的 profile,解析失败不足以中断整次导入
			continue
		}
		modelID := ep.Profile.ModelID
		schema := ep.ParamSchema.JSON
		if modelID == "" || schema == "" {
			continue
		}
		if !json.Valid([]byte(schema)) {
			invalid = append(invalid, modelID)
			continue
		}
		result[modelID] = schema
	}
	return result, invalid, nil
}

func printLimited(items []string) {
	const max = 10
	for i, s := range items {
		if i >= max {
			fmt.Printf("  ... 另有 %d 个\n", len(items)-max)
			return
		}
		fmt.Printf("  %s\n", s)
	}
}
