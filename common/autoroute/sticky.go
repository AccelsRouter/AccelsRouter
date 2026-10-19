package autoroute

import "hash/fnv"

// The model an auto request settled on is remembered next to the channel
// affinity entry, so that the next request of the same conversation can go
// straight back to it. The cache holds integers, so the record packs the
// tier (3 bits) and a 28-bit hash of the model name into 31 bits, which fits
// an int on every platform. A hash is enough because it is only ever matched
// against the few models of one auto model; a record that matches none of
// them (the admin changed the list) is ignored.
const (
	stickyHashBits = 28
	stickyHashMask = 1<<stickyHashBits - 1
)

// ModelHash is the 28-bit hash stored in a sticky record.
func ModelHash(model string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(model))
	return h.Sum32() & stickyHashMask
}

// EncodeSticky packs a tier and a model into a cache value. The value is
// never zero, so a missing entry cannot be mistaken for a record.
func EncodeSticky(tier Tier, model string) int {
	if tier < TierSimple || tier > TierReasoning {
		tier = TierSimple
	}
	return (int(tier)+1)<<stickyHashBits | int(ModelHash(model))
}

// DecodeSticky unpacks a cache value. ok is false when v is not a record.
func DecodeSticky(v int) (tier Tier, hash uint32, ok bool) {
	level := v >> stickyHashBits
	if v <= 0 || level < 1 || level > int(TierReasoning)+1 {
		return TierSimple, 0, false
	}
	return Tier(level - 1), uint32(v) & stickyHashMask, true
}

// FindByHash returns the model in models that hashes to h.
func FindByHash(models []string, h uint32) (string, bool) {
	for _, m := range models {
		if ModelHash(m) == h {
			return m, true
		}
	}
	return "", false
}
