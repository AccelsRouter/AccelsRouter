package setting

// Fork-only reseller upstream routing settings.
var (
	// ResellerRoutingEnabled is the global kill switch for per-reseller upstream
	// routing: a reseller's customers are routed only through the channels the
	// platform admin bound to that reseller, with per-model priority and sticky
	// (cache-preserving) upstream/key affinity. Off by default: deploying the
	// code changes nothing until an admin opts in, and turning it off returns
	// every reseller customer to the platform default pool on the next request.
	ResellerRoutingEnabled = false
)
