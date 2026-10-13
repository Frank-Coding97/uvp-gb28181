package metrics

// Compute 计算接入健康度(plan §6.1 / D-4)
//
// 公式:
//
//	health = 100 * (w_reg*R_reg + w_kpa*R_kpa + w_inv*R_inv + w_cat*R_cat) - P_异常
//
// R_X = 今日 X 事务成功率(0~1),无样本时该项不参与加权,权重重新归一
// P_异常 = min(异常事务数 * 0.1, 20)
//
// 默认权重:
//
//	w_reg = 0.30   注册
//	w_kpa = 0.35   心跳(命脉)
//	w_inv = 0.25   点播
//	w_cat = 0.10   目录
//
// 边界:
//   - 4 类核心事务全部无样本 → 返回 HealthEmpty (前端显示 "--")
//   - 异常扣分封顶 20
//   - 健康度下界 0
type weighted struct {
	kind   TxKind
	weight float64
}

var defaultWeights = []weighted{
	{TxRegister, 0.30},
	{TxKeepalive, 0.35},
	{TxInvite, 0.25},
	{TxCatalog, 0.10},
}

// healthInputs 健康度计算需要的输入(便于测试)
type healthInputs struct {
	count   map[TxKind]int64 // 8 类计数
	success map[TxKind]int64 // 8 类成功计数
	abnorm  int64
}

// computeHealth 给定输入算健康度,纯函数无副作用
func computeHealth(in healthInputs) float64 {
	var weightSum, weightedRate float64
	hasSample := false
	for _, w := range defaultWeights {
		c := in.count[w.kind]
		if c == 0 {
			continue
		}
		hasSample = true
		rate := float64(in.success[w.kind]) / float64(c)
		weightedRate += w.weight * rate
		weightSum += w.weight
	}
	if !hasSample {
		return HealthEmpty
	}
	// 归一(避免某类无样本时该权重被吞)
	avg := weightedRate / weightSum
	score := 100.0 * avg

	penalty := float64(in.abnorm) * 0.1
	if penalty > 20 {
		penalty = 20
	}
	score -= penalty
	if score < 0 {
		score = 0
	}
	return score
}
