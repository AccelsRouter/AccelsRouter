package setting

// Fork-only personal BYOK (bring-your-own-key) settings.
var (
	// PersonalByokEnabled gates the personal BYOK feature globally. Off by
	// default: a deployment must opt in before individual users can inject
	// their own upstream channels into the routing pool.
	PersonalByokEnabled = false

	// ByokFeeRatio is the default group ratio applied to a newly created BYOK
	// private group (user-<id>). 0 means BYOK requests are free on the platform
	// — the user pays only their own upstream provider, matching OrcaRouter's
	// zero-markup default. An admin can override the fee per user by adjusting
	// that user's BYOK group ratio (e.g. 0.05 for a 5% routing fee like
	// OpenRouter). Applied only when the group has no ratio yet; changing this
	// does not rewrite existing groups.
	ByokFeeRatio = 0.0
)
