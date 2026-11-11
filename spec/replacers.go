package spec

import (
	"slices"
	"strings"

	"github.com/kenjones-cisco/dapperdox/config"
)

const rewritePairSize = 2

func newReplacer(settings config.Settings) *strings.Replacer {
	keys := make([]string, 0, len(settings.SpecRewriteURL))
	for key := range settings.SpecRewriteURL {
		keys = append(keys, key)
	}

	slices.Sort(keys)

	replacements := make([]string, 0, rewritePairSize*len(keys))

	for _, key := range keys {
		value := settings.SpecRewriteURL[key]
		if value == "" {
			value = settings.SiteURL
		}

		replacements = append(replacements, key, value)
	}

	return strings.NewReplacer(replacements...)
}
