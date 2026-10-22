package setting

// WonderGate (acquiring / card + local payment) configuration.
// Docs: https://document.wondergate.io/zh/reference/introduction.html
//
// Gateway (API) hosts are fixed per environment; the admin only toggles
// Sandbox and fills in credentials. Package-level
// vars for fast access, mutated via the option API in model.UpdateOption.
var (
	WonderGateEnabled bool
	WonderGateSandbox bool = true

	// Production credentials (Basic Auth merchantId:secretKey + appId).
	WonderGateMerchantId string
	WonderGateSecretKey  string
	WonderGateAppId      string

	// Sandbox credentials.
	WonderGateSandboxMerchantId string
	WonderGateSandboxSecretKey  string
	WonderGateSandboxAppId      string

	WonderGateUnitPrice      float64 = 1.0  // price per quota unit, in WonderGateCurrency
	WonderGateMinTopUp       int     = 1    // minimum top-up amount (quota units)
	WonderGateBillingCountry string  = "US" // default billingAddress.country (checkout requires it)
	// WonderGateCurrency is the ISO currency the WonderGate merchant is configured
	// to accept. It must match the merchant's supported currency, otherwise
	// checkout creation fails (e.g. code=101 "USD is invalid"). Set it together
	// with WonderGateUnitPrice so the charged amount is denominated correctly
	// (e.g. currency=CNY with a CNY-based unit price).
	WonderGateCurrency string = "USD"
	// WonderGateTransactionMethod pre-selects a WonderGate payment method
	// (checkout field transactionMethod, e.g. "AlipayHK"), so the payer lands
	// on that method directly instead of the cashier page. Empty = cashier
	// (WonderGate's "Default"). Pair it with a matching billing country.
	WonderGateTransactionMethod string
)

const (
	wonderGateSandboxGatewayURL = "https://sandbox-securegtw.wondergate.io"
	wonderGateProdGatewayURL    = "https://securegtw-hk.wondergate.io"
)

// WonderGateGatewayBaseURL returns the API host for the active environment.
func WonderGateGatewayBaseURL() string {
	if WonderGateSandbox {
		return wonderGateSandboxGatewayURL
	}
	return wonderGateProdGatewayURL
}

// WonderGateActiveCredentials returns the merchantId / secretKey / appId for the
// active environment (sandbox vs production).
func WonderGateActiveCredentials() (merchantId, secretKey, appId string) {
	if WonderGateSandbox {
		return WonderGateSandboxMerchantId, WonderGateSandboxSecretKey, WonderGateSandboxAppId
	}
	return WonderGateMerchantId, WonderGateSecretKey, WonderGateAppId
}
