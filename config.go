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
	// MultipleLGTMLabel indicates whether the PR can add lgtm-[login name] kind labels.
	MultipleLGTMLabel bool `json:"multiple_lgtm_label"`
	// CloseStoreSha indicates whether the sha of the current PR is saved when adding on the lgtm label
	CloseStoreSha bool `json:"close_store_sha"`
	// SpecialRepo indicates exec /lgtm or /approve command need check sig dir change
	SpecialRepo []string `json:"special_repo"`
}

func (c *botConfig) setDefault() {
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
