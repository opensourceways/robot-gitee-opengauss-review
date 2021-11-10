package main

import (
	"encoding/base64"
	"fmt"
	"path"
	"regexp"

	sdk "gitee.com/openeuler/go-gitee/gitee"
	libconfig "github.com/opensourceways/community-robot-lib/config"
	"github.com/opensourceways/community-robot-lib/giteeclient"
	libplugin "github.com/opensourceways/community-robot-lib/giteeplugin"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/yaml"
)

const botName = "review"

type iClient interface {
	AddMultiPRLabel(org, repo string, number int32, label []string) error
	RemovePRLabel(org, repo string, number int32, label string) error
	CreatePRComment(org, repo string, number int32, comment string) error
	IsCollaborator(owner, repo, login string) (bool, error)
	ListPRComments(org, repo string, number int32) ([]sdk.PullRequestComments, error)
	GetPRCommit(org, repo, SHA string) (sdk.RepoCommit, error)
	GetPathContent(org, repo, path, ref string) (sdk.Content, error)
	GetPullRequestChanges(org, repo string, number int32) ([]sdk.PullRequestFiles, error)
}

func newRobot(cli iClient) *robot {
	return &robot{cli: cli}
}

type ownersFile struct {
	Maintainers []string `yaml:"maintainers"`
	Committers  []string `yaml:"committers"`
}

type robot struct {
	cli iClient
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
	return nil, fmt.Errorf("no %s robot config for this repo:%s/%s", botName, org, repo)
}

func (bot *robot) RegisterEventHandler(p libplugin.HandlerRegitster) {
	p.RegisterPullRequestHandler(bot.handlePREvent)
	p.RegisterNoteEventHandler(bot.handleNoteEvent)
}

func (bot *robot) handlePREvent(e *sdk.PullRequestEvent, cfg libconfig.PluginConfig, log *logrus.Entry) error {
	if giteeclient.GetPullRequestAction(e) != giteeclient.PRActionChangedSourceBranch {
		return nil
	}

	if err := bot.clearLGTM(cfg, e, log); err != nil {
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
	if prNe.PullRequest == nil || prNe.PullRequest.State != "open" {
		return nil
	}

	if err := bot.handleLGTM(prNe, cfg, log); err != nil {
		return err
	}

	return nil
}

func (bot *robot) hasPermission(commenter string, info giteeclient.PRInfo, cfg *botConfig, log *logrus.Entry) (bool, error) {
	// TODO: change judge commenter is collaborator as judge commenter's permission in gitee repository settings
	v, err := bot.cli.IsCollaborator(info.Org, info.Repo, commenter)
	if err != nil {
		return false, err
	}

	if v {
		return true, nil
	}

	// determine if the commenter is in the OWNERS file of the repository where the event occurred
	v, err = bot.inRepoOwnersFile(commenter, info, "OWNERS", log)
	if err != nil {
		return false, err
	}

	if v {
		return true, nil
	}

	return bot.inSigDirOwnersFile(commenter, info, cfg, log)
}

func (bot *robot) getLgtmLastCommentSha(info giteeclient.PRInfo) string {
	comments, err := bot.cli.ListPRComments(info.Org, info.Repo, info.Number)
	if err != nil {
		return ""
	}

	lc := len(comments)
	if lc == 0 {
		return ""
	}

	lastLgtmSha := ""
	regBotAddLgtmSha := regexp.MustCompile(fmt.Sprintf(labelHiddenValue, "(.*)"))
	for i := lc - 1; i >= 0; i-- {
		comment := comments[i]
		m := regBotAddLgtmSha.FindStringSubmatch(comment.Body)
		if m != nil && comment.UpdatedAt == comment.CreatedAt {
			lastLgtmSha = m[1]
			break
		}
	}

	return lastLgtmSha
}

func (bot *robot) inRepoOwnersFile(commenter string, info giteeclient.PRInfo, path string, log *logrus.Entry) (bool, error) {
	content, err := bot.cli.GetPathContent(info.Org, info.Repo, path, info.BaseRef)
	if err != nil {
		return false, err
	}

	owners := decodeOwners(content.Content, log)
	return owners.Has(commenter), nil
}

func (bot *robot) inSigDirOwnersFile(commenter string, info giteeclient.PRInfo, cfg *botConfig, log *logrus.Entry) (bool, error) {
	if !cfg.isSpecialRepo(info.Repo) {
		return false, nil
	}

	cFiles, err := bot.cli.GetPullRequestChanges(info.Org, info.Repo, info.Number)
	if err != nil {
		return false, err
	}

	regSigFilePattern := regexp.MustCompile("^sig/[a-zA-Z0-9_-]+/.+")
	filesPath := sets.NewString()
	for _, file := range cFiles {
		if !regSigFilePattern.MatchString(file.Filename) {
			return false, nil
		}
		filesPath.Insert(path.Dir(file.Filename))
	}
	for p := range filesPath {
		fp := path.Join(p, "OWNERS")
		yes, err := bot.inRepoOwnersFile(commenter, info, fp, log)
		if err != nil || !yes {
			return false, err
		}
	}

	return true, nil
}

func decodeOwners(content string, log *logrus.Entry) sets.String {
	owners := sets.NewString()

	decodeBytes, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		log.Error(err)
		return owners
	}

	var oFile ownersFile
	err = yaml.Unmarshal(decodeBytes, &oFile)
	if err != nil {
		log.Error(err)
		return owners
	}

	if len(oFile.Maintainers) > 0 {
		owners.Insert(oFile.Maintainers...)
	}

	if len(oFile.Committers) > 0 {
		owners.Insert(oFile.Committers...)
	}

	return owners
}
