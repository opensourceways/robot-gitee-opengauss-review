package main

import (
	"fmt"

	"github.com/opensourceways/community-robot-lib/config"
	framework "github.com/opensourceways/community-robot-lib/robot-gitee-framework"
	"github.com/opensourceways/community-robot-lib/utils"
	sdk "github.com/opensourceways/go-gitee/gitee"
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
	MergePR(owner, repo string, number int32, opt sdk.PullRequestMergePutParam) error
	UpdatePullRequest(org, repo string, number int32, param sdk.PullRequestUpdateParam) (sdk.PullRequest, error)
}

func newRobot(cli iClient, cacheCli *cache.SDK) *robot {
	return &robot{cli: cli, cacheCli: cacheCli}
}

type robot struct {
	cli      iClient
	cacheCli *cache.SDK
}

func (bot *robot) NewConfig() config.Config {
	return &configuration{}
}

func (bot *robot) getConfig(cfg config.Config, org, repo string) (*botConfig, error) {
	c, ok := cfg.(*configuration)
	if !ok {
		return nil, fmt.Errorf("can't convert to configuration")
	}

	if bc := c.configFor(org, repo); bc != nil {
		return bc, nil
	}

	return nil, fmt.Errorf("no config for this repo:%s/%s", org, repo)
}

func (bot *robot) RegisterEventHandler(p framework.HandlerRegitster) {
	p.RegisterPullRequestHandler(bot.handlePREvent)
	p.RegisterNoteEventHandler(bot.handleNoteEvent)
}

func (bot *robot) handlePREvent(e *sdk.PullRequestEvent, c config.Config, log *logrus.Entry) error {
	org, repo := e.GetOrgRepo()
	cfg, err := bot.getConfig(c, org, repo)
	if err != nil {
		return err
	}

	merr := utils.NewMultiErrors()
	if err := bot.clearLabel(e); err != nil {
		merr.AddError(err)
	}

	if err := bot.tryMerge(e, cfg); err != nil {
		merr.AddError(err)
	}

	return merr.Err()
}

func (bot *robot) handleNoteEvent(e *sdk.NoteEvent, c config.Config, log *logrus.Entry) error {
	org, repo := e.GetOrgRepo()
	cfg, err := bot.getConfig(c, org, repo)
	if err != nil {
		return err
	}

	merr := utils.NewMultiErrors()
	if err := bot.handleLGTM(e, cfg, log); err != nil {
		merr.AddError(err)
	}

	if err = bot.handleApprove(e, cfg, log); err != nil {
		merr.AddError(err)
	}

	if err = bot.handleCheckPR(e, cfg); err != nil {
		merr.AddError(err)
	}

	return merr.Err()
}
