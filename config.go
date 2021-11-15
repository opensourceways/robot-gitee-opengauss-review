package main

import (
	libconfig "github.com/opensourceways/community-robot-lib/config"
	"k8s.io/apimachinery/pkg/util/sets"
)

type configuration struct {
	ConfigItems []botConfig `json:"config_items,omitempty"`
}

func (c *configuration) configFor(org, repo string) *botConfig {
	if c == nil {
		return nil
	}

	items := c.ConfigItems

	v := make([]libconfig.IPluginForRepo, len(items))
	for i := range items {
		v[i] = &items[i]
	}

	if i := libconfig.FindConfig(org, repo, v); i >= 0 {
		return &items[i]
	}

	return nil
}

func (c *configuration) Validate() error {
	if c == nil {
		return nil
	}

	items := c.ConfigItems
	for i := range items {
		if err := items[i].validate(); err != nil {
			return err
		}
	}

	return nil
}

func (c *configuration) SetDefault() {
	if c == nil {
		return
	}

	Items := c.ConfigItems
	for i := range Items {
		Items[i].setDefault()
	}
}

type botConfig struct {
	libconfig.PluginForRepo
	// LgtmCountsRequired greater than 1 means that the lgtm label is composed of lgtm-login,
	// and as the basis for judging the conditions of PR merge.the default value is 1.
	LgtmCountsRequired uint8 `json:"lgtm_counts_required,omitempty"`
	// SpecialRepo indicates it should check the devepler's permission besed on the owners file
	// in sig directory when the developer comment /lgtm or /approve command for these repos.
	SpecialRepo []string `json:"special_repo,omitempty"`
}

func (c *botConfig) setDefault() {
	if c.LgtmCountsRequired == 0 {
		c.LgtmCountsRequired = 1
	}
}

func (c *botConfig) validate() error {
	return c.PluginForRepo.Validate()
}

func (c *botConfig) isSpecialRepo(repo string) bool {
	if len(c.SpecialRepo) == 0 {
		return false
	}

	sps := sets.NewString(c.SpecialRepo...)

	return sps.Has(repo)
}
