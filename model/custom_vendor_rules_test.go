package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A model with no metadata row gets its vendor from its name, including the
// names upstream's rules do not cover; one no rule knows stays without.
func TestDefaultVendorOfUncataloguedModels(t *testing.T) {
	names := map[string]string{
		"dola-seed-2-1-turbo-260628":     "字节跳动",
		"dola-seedream-5-0-pro-260628":   "字节跳动",
		"dreamina-seedance-2-5-260628":   "字节跳动",
		"doubao-seedance-2-0-260128":     "字节跳动",
		"wan3.0-video-prime":             "阿里巴巴",
		"text-embedding-v4":              "阿里巴巴",
		"qwen3.8-flash":                  "阿里巴巴",
		"text-embedding-3-large":         "OpenAI",
		"hy4-preview":                    "腾讯",
		"deepseek-v4-1-flash-261017":     "DeepSeek",
		"some-model-nobody-has-heard-of": "",
	}
	abilities := make([]AbilityWithChannel, 0, len(names))
	for name := range names {
		abilities = append(abilities, AbilityWithChannel{Ability: Ability{Model: name}})
	}
	metas := map[string]*Model{}
	vendors := map[int]*Vendor{}
	initDefaultVendorMapping(metas, vendors, abilities)

	for name, want := range names {
		got := ""
		if vendor := vendors[metas[name].VendorID]; vendor != nil {
			got = vendor.Name
		}
		assert.Equal(t, want, got, name)
	}
}
