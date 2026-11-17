package memory

import (
	"strings"
)

// embeddingSpace is how one embedding model is used: the prefixes its inputs
// need, the value stored in memories.embedding_model, and the thresholds
// measured for it. Vectors are only ever compared within one Name, so Name
// must change whenever the way text is turned into a vector changes; rows
// stored under an older name are re-embedded in the background.
//
// The E5 numbers come from the evaluation set in testdata/recall_eval.json,
// embedded by the deployed intfloat/multilingual-e5-large (see eval.go and
// `superbot memory-eval`).
type embeddingSpace struct {
	Name          string
	QueryPrefix   string
	PassagePrefix string

	// RecallMin is the absolute similarity a memory needs to be recalled
	// when per-query calibration is off or not available yet.
	RecallMin float64
	// RecallDelta > 0 turns on per-query calibration: a memory is recalled
	// when its similarity beats the query's background (its mean similarity
	// to calibrationPassages) by RecallDelta. E5 shifts the whole score band
	// per query, so one absolute cut-off is either deaf or noisy.
	RecallDelta float64
	// RecallFloor bounds the calibrated threshold from below.
	RecallFloor float64
	// Margin drops hits that trail the best one by more than this, so a
	// weakly related memory does not ride along with a strong match.
	Margin float64

	// RelatedMin is the memory-to-memory similarity from which an existing
	// memory is handed to the conflict check as possibly outdated by a new one.
	RelatedMin float64
	// MergeMin is the similarity from which a new memory is merged into an
	// existing one when nobody (no model, no user) judged the pair.
	MergeMin float64
	// TidyMin is the similarity from which the unattended Tidy archives a copy.
	TidyMin float64

	// Ranking weights, in similarity units: they reorder hits that already
	// passed the threshold and never let an irrelevant memory in.
	PinnedBonus  float64
	RecencyBonus float64
}

const (
	localModel = "local-hash-1024"
	// e5SpaceVersion marks vectors embedded with the E5 "query: "/"passage: "
	// prefixes. Rows stored under the bare model name were embedded without
	// them and are moved over by the background re-embed.
	e5SpaceVersion = "@e5-prefix-v1"
)

// The local hash embedder is lexical: it only measures shared words and
// character bigrams. Unrelated sentences score 0.00-0.10, sentences sharing a
// real term from ~0.15 (short Chinese) to 0.3-0.6 (English); see
// TestLocalHashThreshold and TestRecallEvalLocalHash.
var localSpace = embeddingSpace{
	Name:      localModel,
	RecallMin: 0.12, Margin: 0.05,
	RelatedMin: 0.20, MergeMin: 0.92, TidyMin: 0.95,
	PinnedBonus: 0.02, RecencyBonus: 0.02,
}

// Generic remote models (OpenAI text-embedding-3 and alike) put loosely
// related text around 0.3-0.5 and on-topic text above ~0.55. No evaluation
// data exists for them here, so the merge thresholds keep their old values.
func genericRemoteSpace(model string) embeddingSpace {
	return embeddingSpace{
		Name:      "remote:" + model,
		RecallMin: 0.55, Margin: 0.05,
		RelatedMin: 0.6, MergeMin: 0.92, TidyMin: 0.95,
		PinnedBonus: 0.01, RecencyBonus: 0.01,
	}
}

// E5 models need "query: " / "passage: " in front of their inputs and
// compress every score into a narrow high band. Measured on the evaluation set
// with multilingual-e5-large (query vs passage, calibrated):
//   - recall: relevant pairs beat the background by 0.04-0.17, unrelated
//     ones by at most 0.08; delta 0.075 with margin 0.04 gives precision 0.94
//     and recall 0.70 (the old bare-input 0.88 cut-off: 0.96 / 0.55).
//   - memory pairs (passage vs passage): restatements 0.963-0.993, updates of
//     a fact 0.894-0.967, different facts of the same shape ("不吃香菜" vs
//     "不吃葱") up to 0.975, unrelated up to 0.877. No similarity separates
//     a restatement from a different fact, so the unattended merge only acts
//     above every measured different fact and real decisions go to the
//     conflict check, which starts at 0.89.
func e5Space(model string) embeddingSpace {
	return embeddingSpace{
		Name:        "remote:" + model + e5SpaceVersion,
		QueryPrefix: "query: ", PassagePrefix: "passage: ",
		RecallMin: 0.865, RecallDelta: 0.075, RecallFloor: 0.70, Margin: 0.04,
		RelatedMin: 0.89, MergeMin: 0.985, TidyMin: 0.985,
		PinnedBonus: 0.01, RecencyBonus: 0.01,
	}
}

// Quality floors on the bundled evaluation set (see eval.go), a little below
// what the spaces measured so a regression, not noise, trips them.
var (
	e5EvalFloor    = EvalFloor{Precision: 0.90, Recall: 0.68, NoisyNegatives: 1, MinUpdatesReached: 1}
	localEvalFloor = EvalFloor{Precision: 0.40, Recall: 0.33, NoisyNegatives: 8, MinUpdatesReached: 0.5}
)

// isE5Model recognizes the intfloat E5 family by name
// (intfloat/multilingual-e5-large, e5-large-v2, ...).
func isE5Model(model string) bool {
	lower := strings.ToLower(model)
	return strings.Contains(lower, "e5-") || strings.HasSuffix(lower, "-e5") || strings.Contains(lower, "/e5")
}

func spaceFor(configured bool, model string) embeddingSpace {
	switch {
	case !configured:
		return localSpace
	case isE5Model(model):
		return e5Space(model)
	default:
		return genericRemoteSpace(model)
	}
}

// calibrationPassages are neutral, memory-shaped sentences on topics a user's
// memories rarely touch. A query's mean similarity to them is its background:
// how much the model scores anything against this query.
var calibrationPassages = []string{
	"会议纪要需要在会后两小时内发到群里",
	"办公室的打印机在三楼茶水间旁边",
	"季度预算由财务部在每季度第一周审批",
	"新员工入职第一周需要完成安全培训",
	"仓库的库存盘点每月进行一次",
	"客服工单需要在 24 小时内首次响应",
	"设计稿统一存放在共享网盘的设计目录",
	"供应商合同到期前一个月需要提醒法务",
	"周末值班表每月初在群公告里更新",
	"门禁卡丢失后要到前台登记补办",
	"小区的快递柜在 2 号楼楼下",
	"家里的路由器放在客厅电视柜里",
	"阳台上种了几盆多肉和一棵柠檬树",
	"冰箱里的牛奶每周补货一次",
	"楼下的面包店早上八点开门",
	"这个城市的地铁末班车大约是晚上十一点",
	"接口文档使用 OpenAPI 规范编写",
	"日志保留 30 天后自动归档到对象存储",
	"单元测试覆盖率要求不低于 80%",
	"缓存的过期时间默认设置为 10 分钟",
	"移动端应用每两周发布一个版本",
	"图片上传大小限制为 10 MB",
	"The marketing team meets every Monday morning",
	"Invoices are sent to customers on the first business day of the month",
	"The office closes early on public holidays",
	"Customer data is stored in the EU region",
	"The design system uses an 8 pixel spacing grid",
	"Support tickets are triaged by priority every morning",
	"The library downstairs lends books for three weeks",
	"The gym in the building opens at six in the morning",
	"The project wiki lists every service owner",
	"Expense reports need a receipt photo attached",
}

// background is the mean similarity of a query vector to the calibration
// vectors; both are unit length.
func background(query []float32, refs [][]float32) float64 {
	if len(refs) == 0 {
		return 0
	}
	var total float64
	for _, ref := range refs {
		total += dot(query, ref)
	}
	return total / float64(len(refs))
}

func dot(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var sum float64
	for i := range a {
		sum += float64(a[i]) * float64(b[i])
	}
	return sum
}
