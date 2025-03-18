package pointsub

import (
	"sync"
	"time"
)

// 传输属性，用于描述数据传输的特性
type TransportAttributes struct {
	// 数据大小 (字节)
	Size int

	// 是否需要高可靠性
	RequireReliability bool

	// 是否需要实时性
	RequireRealtime bool

	// 是否需要带宽效率（低开销）
	RequireEfficiency bool

	// 消息优先级 (0-10，10最高)
	Priority int

	// 是否可接受延迟
	CanDelay bool

	// 网络条件分级 (0-5, 0最差，5最好)
	NetworkCondition int

	// 是否启用自适应流控
	EnableFlowControl bool

	// 是否需要压缩
	EnableCompression bool

	// 传输模式 (stream, block, batch)
	Mode string

	// 自定义元数据
	Metadata map[string]interface{}
}

// 策略选择结果
type StrategySelection struct {
	// 选中的传输策略
	Strategy TransportStrategy

	// 优化后的参数
	Params TransportParams

	// 选择原因
	Reason string

	// 评分 (0-100)
	Score int

	// 备选策略列表
	Alternatives []TransportStrategy
}

// 策略评分
type StrategyScore struct {
	// 策略
	Strategy TransportStrategy

	// 评分
	Score int

	// 评分原因
	Reason string
}

// StrategySelector 负责基于传输属性选择最佳传输策略
type StrategySelector interface {
	// 选择最佳策略
	SelectBestStrategy(attrs TransportAttributes) StrategySelection

	// 评估策略适用度
	EvaluateStrategy(strategy TransportStrategy, attrs TransportAttributes) int

	// 注册策略
	RegisterStrategy(strategy TransportStrategy)

	// 注册评分器
	RegisterScorer(scorer StrategyScorer)
}

// StrategyScorer 策略评分器接口
type StrategyScorer interface {
	// 评分策略
	ScoreStrategy(strategy TransportStrategy, attrs TransportAttributes) StrategyScore

	// 获取评分器名称
	Name() string

	// 获取评分器权重
	Weight() int
}

// 默认策略选择器实现
type defaultStrategySelector struct {
	// 策略列表
	strategies []TransportStrategy

	// 评分器列表
	scorers []StrategyScorer

	// 消息大小检测器
	sizer MessageSizer

	// 缓存近期选择结果
	selectionCache map[string]StrategySelection

	// 统计信息
	stats struct {
		totalSelections  int
		cacheHits        int
		strategyUsage    map[string]int
		lastSelectionAt  time.Time
		averageScoreTime time.Duration
	}

	// 保护锁
	mu sync.RWMutex

	// 缓存过期时间
	cacheTTL time.Duration
}

// 默认参数
const (
	defaultCacheTTL            = 5 * time.Minute
	defaultCacheSize           = 100
	defaultReliabilityWeight   = 3
	defaultRealtimeWeight      = 3
	defaultEfficiencyWeight    = 2
	defaultPriorityWeight      = 2
	defaultNetworkWeight       = 4
	defaultCompressionWeight   = 1
	defaultFlowControlWeight   = 2
	defaultStrategySelectLimit = 3
)

// NewStrategySelector 创建新的策略选择器
func NewStrategySelector() StrategySelector {
	selector := &defaultStrategySelector{
		strategies:     make([]TransportStrategy, 0),
		scorers:        make([]StrategyScorer, 0),
		sizer:          NewDefaultMessageSizer(),
		selectionCache: make(map[string]StrategySelection),
		cacheTTL:       defaultCacheTTL,
	}
	selector.stats.strategyUsage = make(map[string]int)

	// 注册默认评分器
	selector.RegisterScorer(&sizeBasedScorer{weight: 5})
	selector.RegisterScorer(&reliabilityScorer{weight: defaultReliabilityWeight})
	selector.RegisterScorer(&realtimeScorer{weight: defaultRealtimeWeight})
	selector.RegisterScorer(&networkConditionScorer{weight: defaultNetworkWeight})

	return selector
}

// cacheKey 生成缓存键
func (s *defaultStrategySelector) cacheKey(attrs TransportAttributes) string {
	// 简单实现，实际可能需要更复杂的哈希算法
	key := ""
	key += "s:" + string(rune(attrs.Size/1024)) // 按KB分组
	if attrs.RequireReliability {
		key += "_r:1"
	} else {
		key += "_r:0"
	}
	if attrs.RequireRealtime {
		key += "_t:1"
	} else {
		key += "_t:0"
	}
	if attrs.RequireEfficiency {
		key += "_e:1"
	} else {
		key += "_e:0"
	}
	key += "_p:" + string(rune(attrs.Priority/2)) // 按优先级分组
	key += "_n:" + string(rune(attrs.NetworkCondition))
	if attrs.EnableCompression {
		key += "_c:1"
	} else {
		key += "_c:0"
	}
	if attrs.EnableFlowControl {
		key += "_f:1"
	} else {
		key += "_f:0"
	}
	key += "_m:" + attrs.Mode

	return key
}

// SelectBestStrategy 实现StrategySelector接口
func (s *defaultStrategySelector) SelectBestStrategy(attrs TransportAttributes) StrategySelection {
	s.mu.Lock()
	defer s.mu.Unlock()

	startTime := time.Now()

	// 检查缓存
	cacheKey := s.cacheKey(attrs)
	if selection, ok := s.selectionCache[cacheKey]; ok {
		// 更新统计
		s.stats.totalSelections++
		s.stats.cacheHits++
		s.stats.lastSelectionAt = time.Now()
		s.stats.strategyUsage[selection.Strategy.Name()]++

		return selection
	}

	// 计算每个策略的得分
	scores := make([]StrategyScore, 0, len(s.strategies))
	for _, strategy := range s.strategies {
		score := s.EvaluateStrategy(strategy, attrs)
		scores = append(scores, StrategyScore{
			Strategy: strategy,
			Score:    score,
			Reason:   "综合评分",
		})
	}

	// 如果策略为空，使用消息大小检测器选择一个
	if len(scores) == 0 {
		strategy := s.sizer.SelectStrategy(attrs.Size)
		params := s.sizer.OptimizeParams(attrs.Size)

		selection := StrategySelection{
			Strategy:     strategy,
			Params:       params,
			Reason:       "基于消息大小自动选择",
			Score:        75, // 默认得分
			Alternatives: []TransportStrategy{},
		}

		// 更新统计
		s.stats.totalSelections++
		s.stats.lastSelectionAt = time.Now()
		s.stats.strategyUsage[strategy.Name()]++
		s.stats.averageScoreTime = time.Since(startTime)

		// 缓存结果
		s.selectionCache[cacheKey] = selection

		return selection
	}

	// 排序得分（从高到低）
	for i := 0; i < len(scores)-1; i++ {
		for j := i + 1; j < len(scores); j++ {
			if scores[i].Score < scores[j].Score {
				scores[i], scores[j] = scores[j], scores[i]
			}
		}
	}

	// 选择最高得分的策略
	bestStrategy := scores[0].Strategy
	bestScore := scores[0].Score

	// 准备备选策略列表
	alternatives := make([]TransportStrategy, 0, defaultStrategySelectLimit-1)
	for i := 1; i < len(scores) && i < defaultStrategySelectLimit; i++ {
		alternatives = append(alternatives, scores[i].Strategy)
	}

	// 获取优化参数
	params := s.sizer.OptimizeParams(attrs.Size)

	// 根据属性进一步优化参数
	if attrs.RequireReliability {
		params.EnableChecksum = true
		params.MaxRetries = 5
	}

	if attrs.RequireRealtime {
		params.NoBlockDelay = true
		params.ReadTimeout = 10 * time.Second
		params.WriteTimeout = 10 * time.Second
	}

	if attrs.EnableCompression {
		// 额外的压缩参数设置可以在这里添加
	}

	if attrs.Mode == "stream" {
		params.UseChunking = true
	}

	// 创建结果
	selection := StrategySelection{
		Strategy:     bestStrategy,
		Params:       params,
		Reason:       "综合评分最高",
		Score:        bestScore,
		Alternatives: alternatives,
	}

	// 更新统计
	s.stats.totalSelections++
	s.stats.lastSelectionAt = time.Now()
	s.stats.strategyUsage[bestStrategy.Name()]++
	s.stats.averageScoreTime = time.Since(startTime)

	// 缓存结果
	s.selectionCache[cacheKey] = selection

	// 清理过期缓存
	if len(s.selectionCache) > defaultCacheSize {
		s.cleanupCache()
	}

	return selection
}

// EvaluateStrategy 实现StrategySelector接口
func (s *defaultStrategySelector) EvaluateStrategy(strategy TransportStrategy, attrs TransportAttributes) int {
	if strategy == nil {
		return 0
	}

	totalScore := 0
	totalWeight := 0

	// 使用所有评分器评分
	for _, scorer := range s.scorers {
		score := scorer.ScoreStrategy(strategy, attrs)
		totalScore += score.Score * scorer.Weight()
		totalWeight += scorer.Weight()
	}

	// 如果没有评分器，使用简单评分
	if totalWeight == 0 {
		min, max := strategy.SizeRange()
		if attrs.Size >= min && (max == 0 || attrs.Size < max) {
			return 80 // 在大小范围内，给予较高分数
		}
		return 30 // 不在大小范围内，给予较低分数
	}

	// 计算加权平均分
	return totalScore / totalWeight
}

// RegisterStrategy 实现StrategySelector接口
func (s *defaultStrategySelector) RegisterStrategy(strategy TransportStrategy) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 检查是否已存在相同策略
	for i, existingStrategy := range s.strategies {
		if existingStrategy.Name() == strategy.Name() {
			s.strategies[i] = strategy
			return
		}
	}

	s.strategies = append(s.strategies, strategy)
}

// RegisterScorer 实现StrategySelector接口
func (s *defaultStrategySelector) RegisterScorer(scorer StrategyScorer) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 检查是否已存在相同评分器
	for i, existingScorer := range s.scorers {
		if existingScorer.Name() == scorer.Name() {
			s.scorers[i] = scorer
			return
		}
	}

	s.scorers = append(s.scorers, scorer)
}

// cleanupCache 清理过期缓存
func (s *defaultStrategySelector) cleanupCache() {
	now := time.Now()
	for key := range s.selectionCache {
		if now.Sub(s.stats.lastSelectionAt) > s.cacheTTL {
			delete(s.selectionCache, key)
		}
	}
}

// ===========================================================================
// 基于大小的评分器
// ===========================================================================
type sizeBasedScorer struct {
	weight int
}

func (s *sizeBasedScorer) Name() string {
	return "SizeBasedScorer"
}

func (s *sizeBasedScorer) Weight() int {
	return s.weight
}

func (s *sizeBasedScorer) ScoreStrategy(strategy TransportStrategy, attrs TransportAttributes) StrategyScore {
	min, max := strategy.SizeRange()

	// 检查大小是否在策略范围内
	if attrs.Size >= min && (max == 0 || attrs.Size < max) {
		// 在范围内，基于范围中的位置计算得分
		var positionScore int
		if max == 0 {
			// 无上限的策略
			if attrs.Size >= min*2 {
				positionScore = 100 // 远超下限，满分
			} else {
				// 接近下限，给中等分数
				positionScore = 80 + (attrs.Size-min)*20/(min)
			}
		} else {
			// 有上下限的策略
			range_size := max - min
			if range_size == 0 {
				positionScore = 100 // 避免除零
			} else {
				// 大小在范围中间时得分最高
				middle := min + range_size/2
				distance := absInt(attrs.Size - middle)
				positionScore = 100 - (distance * 40 / range_size)
				if positionScore < 60 {
					positionScore = 60 // 最低不低于60分
				}
			}
		}

		return StrategyScore{
			Strategy: strategy,
			Score:    positionScore,
			Reason:   "大小在策略范围内",
		}
	}

	// 大小不在范围内
	var distance int
	if attrs.Size < min {
		distance = min - attrs.Size
	} else {
		distance = attrs.Size - max
	}

	// 根据距离计算得分，最高40分
	distanceScore := 40 - (distance * 40 / (max - min + 1))
	if distanceScore < 0 {
		distanceScore = 0
	}

	return StrategyScore{
		Strategy: strategy,
		Score:    distanceScore,
		Reason:   "大小超出策略范围",
	}
}

// ===========================================================================
// 可靠性评分器
// ===========================================================================
type reliabilityScorer struct {
	weight int
}

func (s *reliabilityScorer) Name() string {
	return "ReliabilityScorer"
}

func (s *reliabilityScorer) Weight() int {
	return s.weight
}

func (s *reliabilityScorer) ScoreStrategy(strategy TransportStrategy, attrs TransportAttributes) StrategyScore {
	// 基于策略名称的简单可靠性评分
	var reliabilityScore int

	switch strategy.Name() {
	case "TinyMessageStrategy":
		reliabilityScore = 60 // 微小消息策略可靠性较低
	case "SmallMessageStrategy":
		reliabilityScore = 70 // 小消息策略可靠性中等
	case "MediumMessageStrategy":
		reliabilityScore = 85 // 中等消息策略可靠性较高
	case "LargeMessageStrategy":
		reliabilityScore = 90 // 大消息策略可靠性高
	case "HugeMessageStrategy":
		reliabilityScore = 95 // 超大消息策略可靠性最高
	default:
		reliabilityScore = 75 // 默认中等可靠性
	}

	// 如果需要高可靠性，根据需求调整分数
	if attrs.RequireReliability {
		// 对不可靠策略降分，对可靠策略加分
		if reliabilityScore < 80 {
			reliabilityScore -= 20 // 显著降低不可靠策略的得分
		} else {
			reliabilityScore += 10 // 略微提高可靠策略的得分
			if reliabilityScore > 100 {
				reliabilityScore = 100
			}
		}
	}

	return StrategyScore{
		Strategy: strategy,
		Score:    reliabilityScore,
		Reason:   "可靠性评分",
	}
}

// ===========================================================================
// 实时性评分器
// ===========================================================================
type realtimeScorer struct {
	weight int
}

func (s *realtimeScorer) Name() string {
	return "RealtimeScorer"
}

func (s *realtimeScorer) Weight() int {
	return s.weight
}

func (s *realtimeScorer) ScoreStrategy(strategy TransportStrategy, attrs TransportAttributes) StrategyScore {
	// 基于策略名称的简单实时性评分
	var realtimeScore int

	switch strategy.Name() {
	case "TinyMessageStrategy":
		realtimeScore = 95 // 微小消息策略实时性最高
	case "SmallMessageStrategy":
		realtimeScore = 90 // 小消息策略实时性高
	case "MediumMessageStrategy":
		realtimeScore = 80 // 中等消息策略实时性中等
	case "LargeMessageStrategy":
		realtimeScore = 70 // 大消息策略实时性低
	case "HugeMessageStrategy":
		realtimeScore = 50 // 超大消息策略实时性最低
	default:
		realtimeScore = 75 // 默认中等实时性
	}

	// 如果需要高实时性，根据需求调整分数
	if attrs.RequireRealtime {
		// 对低实时性策略降分，对高实时性策略加分
		if realtimeScore < 80 {
			realtimeScore -= 20 // 显著降低低实时性策略的得分
		} else {
			realtimeScore += 10 // 略微提高高实时性策略的得分
			if realtimeScore > 100 {
				realtimeScore = 100
			}
		}
	}

	// 如果可以接受延迟，提高所有策略分数
	if attrs.CanDelay {
		realtimeScore += 10
		if realtimeScore > 100 {
			realtimeScore = 100
		}
	}

	return StrategyScore{
		Strategy: strategy,
		Score:    realtimeScore,
		Reason:   "实时性评分",
	}
}

// ===========================================================================
// 网络条件评分器
// ===========================================================================
type networkConditionScorer struct {
	weight int
}

func (s *networkConditionScorer) Name() string {
	return "NetworkConditionScorer"
}

func (s *networkConditionScorer) Weight() int {
	return s.weight
}

func (s *networkConditionScorer) ScoreStrategy(strategy TransportStrategy, attrs TransportAttributes) StrategyScore {
	networkScore := 75 // 默认中等评分

	// 基于网络条件和策略类型的评分
	switch attrs.NetworkCondition {
	case 0, 1: // 非常差的网络
		// 在非常差的网络中，小数据包策略更好
		switch strategy.Name() {
		case "TinyMessageStrategy", "SmallMessageStrategy":
			networkScore = 90
		case "MediumMessageStrategy":
			networkScore = 70
		case "LargeMessageStrategy", "HugeMessageStrategy":
			networkScore = 40
		}
	case 2, 3: // 一般的网络
		// 在一般的网络中，中等大小策略更平衡
		switch strategy.Name() {
		case "TinyMessageStrategy":
			networkScore = 70
		case "SmallMessageStrategy", "MediumMessageStrategy":
			networkScore = 85
		case "LargeMessageStrategy":
			networkScore = 75
		case "HugeMessageStrategy":
			networkScore = 60
		}
	case 4, 5: // 很好的网络
		// 在良好的网络中，大数据包更有效率
		switch strategy.Name() {
		case "TinyMessageStrategy":
			networkScore = 60
		case "SmallMessageStrategy":
			networkScore = 70
		case "MediumMessageStrategy":
			networkScore = 85
		case "LargeMessageStrategy", "HugeMessageStrategy":
			networkScore = 95
		}
	}

	return StrategyScore{
		Strategy: strategy,
		Score:    networkScore,
		Reason:   "网络条件评分",
	}
}

// 辅助函数：整数绝对值
func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
