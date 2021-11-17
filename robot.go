package main

import (
	"fmt"

	sdk "gitee.com/openeuler/go-gitee/gitee"
	libconfig "github.com/opensourceways/community-robot-lib/config"
	"github.com/opensourceways/community-robot-lib/giteeclient"
	libplugin "github.com/opensourceways/community-robot-lib/giteeplugin"
	cache "github.com/opensourceways/repo-file-cache/sdk"
	"github.com/sirupsen/logrus"
)

const botName = "review"

type iClient interface {
	AddPRLabel(org, repo string, number int32, label string) error
	RemovePRLabel(org, repo string, number int32, label string) error
	RemovePRLabels(org, repo string, number int32, label []string) error
	CreatePRComment(org, repo string, number int32, comment string) error
	GetUserPermissionsOfRepo(org, repo, login string) (sdk.ProjectMemberPermission, error)
	GetPathContent(org, repo, path, ref string) (sdk.Content, error)
	GetPullRequestChanges(org, repo string, number int32) ([]sdk.PullRequestFiles, error)
	CreateRepoLabel(org, repo, label, color string) error
	GetRepoLabels(owner, repo string) ([]sdk.Label, error)
}

func newRobot(cli iClient, cacheCli *cache.SDK) *robot {
	return &robot{cli: cli, cacheCli: cacheCli}
}

type robot struct {
	cli      iClient
	cacheCli *cache.SDK
}

func (bot *robot) NewPluginConfig() libconfig.PluginConfig {
	return &configuration{}
}

func (bot *robot) getConfig(cfg libconfig.PluginConfig, org, repo string) (*botConfig, error) {
	c, ok := cfg.(*configuration)
	if !ok {
		return nil, fmt.Errorf("can't convert to configuration")
	}

	if bc := c.configFor(org, repo); bc != nil {
		return bc, nil
	}

	return nil, fmt.Errorf("no config for this repo:%s/%s", org, repo)
}

func (bot *robot) RegisterEventHandler(p libplugin.HandlerRegitster) {
	p.RegisterPullRequestHandler(bot.handlePREvent)
	p.RegisterNoteEventHandler(bot.handleNoteEvent)
}

func (bot *robot) handlePREvent(e *sdk.PullRequestEvent, cfg libconfig.PluginConfig, log *logrus.Entry) error {
	org, repo := giteeclient.GetOwnerAndRepoByPREvent(e)
	if _, err := bot.getConfig(cfg, org, repo); err != nil {
		return err
	}

	return bot.clearLabel(e)
}

func (bot *robot) handleNoteEvent(e *sdk.NoteEvent, pc libconfig.PluginConfig, log *logrus.Entry) error {
	org, repo := giteeclient.GetOwnerAndRepoByNoteEvent(e)
	cfg, err := bot.getConfig(pc, org, repo)
	if err != nil {
		return err
	}

	if err := bot.handleLGTM(e, cfg, log); err != nil {
		log.WithError(err).Error("handle lgtm command")
	}

	return bot.handleApprove(e, cfg, log)
}
