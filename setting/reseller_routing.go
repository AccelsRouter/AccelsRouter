package setting

// Fork-only reseller upstream routing settings.
var (
	// ResellerRoutingEnabled is the emergency kill switch for per-reseller
	// upstream routing: a reseller's customers are routed only through the
	// channels the platform admin bound to that reseller, with per-model
	// priority and sticky (cache-preserving) upstream/key affinity.
	//
	// On by default: binding channels to a reseller IS the admin's intent, so
	// routing applies as soon as a reseller has bound channels — resellers with
	// no bindings, and all non-reseller traffic, are untouched either way.
	// Turning it off (system option ResellerRoutingEnabled) returns every
	// reseller customer to the platform default pool on the next request; the
	// admin routing tab shows a warning with a re-enable action while it is off.
	ResellerRoutingEnabled = true
)
