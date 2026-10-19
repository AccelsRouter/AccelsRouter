package autoroute

// TierModels holds the ordered model list of each tier, indexed by Tier.
// A model may appear under several tiers. The value is read-only once built.
type TierModels [4][]string

// Set stores a copy of models as the list for tier.
func (t *TierModels) Set(tier Tier, models []string) {
	if tier < TierSimple || tier > TierReasoning {
		return
	}
	t[tier] = append([]string(nil), models...)
}

// Effective returns the tier whose list serves a request classified as tier:
// tier itself when it has models, otherwise the nearest stronger tier that
// has some, otherwise the nearest weaker one. ok is false when every list is
// empty.
func (t TierModels) Effective(tier Tier) (Tier, bool) {
	if tier < TierSimple {
		tier = TierSimple
	}
	if tier > TierReasoning {
		tier = TierReasoning
	}
	for up := tier; up <= TierReasoning; up++ {
		if len(t[up]) > 0 {
			return up, true
		}
	}
	for down := tier - 1; down >= TierSimple; down-- {
		if len(t[down]) > 0 {
			return down, true
		}
	}
	return TierSimple, false
}

// FailoverPlan is the order in which models are tried for a request
// classified as tier: the effective tier's list, then every stronger tier's
// list. It never goes down to a weaker tier. Failing over to a weaker model
// than the classifier asked for trades quality for availability behind the
// user's back; failing over to a stronger one only costs more. Duplicates
// keep their first position.
func (t TierModels) FailoverPlan(tier Tier) []string {
	eff, ok := t.Effective(tier)
	if !ok {
		return nil
	}
	var plan []string
	seen := make(map[string]bool)
	for level := eff; level <= TierReasoning; level++ {
		for _, m := range t[level] {
			if !seen[m] {
				seen[m] = true
				plan = append(plan, m)
			}
		}
	}
	return plan
}

// PlanFrom is FailoverPlan with first moved to the front. It is the plan for
// a request that keeps a model it already used (channel affinity) and falls
// back to the usual order only when that model fails.
func (t TierModels) PlanFrom(first string, tier Tier) []string {
	plan := []string{first}
	for _, m := range t.FailoverPlan(tier) {
		if m != first {
			plan = append(plan, m)
		}
	}
	return plan
}

// All lists every model once, weakest tier first.
func (t TierModels) All() []string {
	var all []string
	seen := make(map[string]bool)
	for level := TierSimple; level <= TierReasoning; level++ {
		for _, m := range t[level] {
			if !seen[m] {
				seen[m] = true
				all = append(all, m)
			}
		}
	}
	return all
}

// TierOf returns the weakest tier whose list contains model.
func (t TierModels) TierOf(model string) (Tier, bool) {
	for level := TierSimple; level <= TierReasoning; level++ {
		for _, m := range t[level] {
			if m == model {
				return level, true
			}
		}
	}
	return TierSimple, false
}
