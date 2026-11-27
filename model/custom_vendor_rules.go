// Fork: default vendors for model names upstream's rules do not cover, so the
// price list, and every vendor filter built on it, names a vendor for them. As
// with upstream's rules, a model with a metadata row keeps the vendor set
// there; these apply only to a model that has none.
package model

import "maps"

func init() {
	maps.Copy(defaultVendorRules, map[string]string{
		"seedance":           "字节跳动",
		"seedream":           "字节跳动",
		"dola-seed":          "字节跳动",
		"seed-tts":           "字节跳动",
		"wan2.":              "阿里巴巴",
		"wan3.":              "阿里巴巴",
		"qwq":                "阿里巴巴",
		"z-image":            "阿里巴巴",
		"happyhorse":         "阿里巴巴",
		"text-embedding-v":   "阿里巴巴",
		"text-embedding-3":   "OpenAI",
		"text-embedding-ada": "OpenAI",
		"hy3":                "腾讯",
		"hy4":                "腾讯",
		"wand-vega":          "腾讯",
		"berry-1.":           "MuleRouter",
		"carrothub-":         "MuleRouter",
		"flashvsr":           "MuleRouter",
	})
}
