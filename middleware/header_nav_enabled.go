package middleware

// HeaderNavModuleEnabled reports whether a header navigation module (pricing,
// rankings) is switched on. Fork-local: surfaces outside the web routes that
// serve the same data, such as the MCP server, ask it so they follow the same
// switch as the page.
func HeaderNavModuleEnabled(module string) bool {
	return getHeaderNavAccess(module).Enabled
}
