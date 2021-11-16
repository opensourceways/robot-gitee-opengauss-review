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
	AddMultiPRLabel(org, repo string, number int32, label []string) error
	RemovePRLabel(org, repo string, number int32, label string) error
	CreatePRComment(org, repo string, number int32, comment string) error
	GetUserPermissionsOfRepo(org, repo, login string) (sdk.ProjectMemberPermission, error)
	ListPRComments(org, repo string, number int32) ([]sdk.PullRequestComments, error)
	GetPRCommit(org, repo, SHA string) (sdk.RepoCommit, error)
	GetPathContent(org, repo, path, ref string) (sdk.Content, error)
	GetPullRequestChanges(org, repo string, number int32) ([]sdk.PullRequestFiles, error)
	CreateRepoLabel(org, repo, label, color string) error
	GetRepoLabels(owner, repo string) ([]sdk.Label, error)
}

func newRobot(cli iClient, cacheCli *cache.SDK) *robot {
	return &robot{cli: cli, cacheCli: cacheCli}
}

type ownersFile struct {
	Maintainers []string `yaml:"maintainers"`
	Committers  []string `yaml:"committers"`
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
	if giteeclient.GetPullRequestAction(e) != giteeclient.PRActionChangedSourceBranch {
		return nil
	}

	if err := bot.clearLGTM(e); err != nil {
		log.Error(err)
	}

	return nil
}

func (bot *robot) handleNoteEvent(e *sdk.NoteEvent, cfg libconfig.PluginConfig, log *logrus.Entry) error {
	ne := giteeclient.NewNoteEventWrapper(e)
	if !ne.IsPullRequest() || !ne.IsCreatingCommentEvent() {
		return nil
	}

	prNe := giteeclient.NewPRNoteEvent(e)
	if prNe.IsPROpen() {
		return nil
	}

	if err := bot.handleLGTM(prNe, cfg, log); err != nil {
		log.Error(err)
	}

	return nil
}
