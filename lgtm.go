package main

import (
	"fmt"
	"regexp"
	"strings"

	sdk "gitee.com/openeuler/go-gitee/gitee"
	libconfig "github.com/opensourceways/community-robot-lib/config"
	"github.com/opensourceways/community-robot-lib/giteeclient"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	lgtmLabel               = "lgtm"
	lgtmAddedMessage        = `***lgtm*** is added in this pull request by: ***%s***. :wave:`
	lgtmSelfOwnMessage      = `***lgtm*** can not be added in your self-own pull request. :astonished: `
	lgtmNoPermissionMessage = `***@%s*** has no permission to %s ***lgtm*** in this pull request. :astonished:
	please contact to the collaborators in this repository.`
	// the gitee platform limits the length of labels to a maximum of 20.
	labelLenLimit = 20
)

var (
	regAddLgtm    = regexp.MustCompile(`(?mi)^/lgtm\s*$`)
	regRemoveLgtm = regexp.MustCompile(`(?mi)^/lgtm cancel\s*$`)
)

func (bot *robot) handleLGTM(e giteeclient.PRNoteEvent, pc libconfig.PluginConfig, log *logrus.Entry) error {
	org, repo := e.GetOrgRep()

	cfg, err := bot.getConfig(pc, org, repo)
	if err != nil {
		return err
	}

	if regAddLgtm.MatchString(e.GetComment()) {
		return bot.addLGTM(cfg, e, log)
	}

	if regRemoveLgtm.MatchString(e.GetComment()) {
		return bot.removeLGTM(cfg, e, log)
	}

	return nil
}

func (bot *robot) addLGTM(cfg *botConfig, e giteeclient.PRNoteEvent, log *logrus.Entry) error {
	prInfo := e.GetPRInfo()
	log.Infof("start do add lgtm label for %s/%s/pull:%d", prInfo.Org, prInfo.Repo, prInfo.Number)

	commenter := e.GetCommenter()
	if prInfo.Author == commenter {
		return bot.cli.CreatePRComment(prInfo.Org, prInfo.Repo, prInfo.Number, lgtmSelfOwnMessage)
	}

	v, err := bot.hasPermission(commenter, prInfo, cfg, log)
	if err != nil {
		return err
	}

	if !v {
		comment := fmt.Sprintf(lgtmNoPermissionMessage, commenter, "add")

		return bot.cli.CreatePRComment(prInfo.Org, prInfo.Repo, prInfo.Number, comment)
	}

	label := lgtmLabelContent(commenter, cfg.LgtmCountsRequired)
	if label != lgtmLabel {
		if err := bot.complexLgtmPrepare(label, prInfo); err != nil {
			log.Error(err)
		}
	}

	if err := bot.cli.AddMultiPRLabel(prInfo.Org, prInfo.Repo, prInfo.Number, []string{label}); err != nil {
		return err
	}

	comment := fmt.Sprintf(lgtmAddedMessage, commenter)

	return bot.cli.CreatePRComment(prInfo.Org, prInfo.Repo, prInfo.Number, comment)
}

func (bot *robot) removeLGTM(cfg *botConfig, e giteeclient.PRNoteEvent, log *logrus.Entry) error {
	prInfo := e.GetPRInfo()
	log.Infof("start do add lgtm label for %s/%s/pull:%d", prInfo.Org, prInfo.Repo, prInfo.Number)

	commenter := e.GetCommenter()
	if prInfo.Author != commenter {
		v, err := bot.hasPermission(commenter, prInfo, cfg, log)
		if err != nil {
			return err
		}

		if !v {
			comment := fmt.Sprintf(lgtmNoPermissionMessage, commenter, "remove")

			return bot.cli.CreatePRComment(prInfo.Org, prInfo.Repo, prInfo.Number, comment)
		}

		label := lgtmLabelContent(commenter, cfg.LgtmCountsRequired)

		return bot.cli.RemovePRLabel(prInfo.Org, prInfo.Repo, prInfo.Number, label)
	}

	// the commenter can remove all of lgtm[-login name] kind labels that who is the pr author
	v := getLGTMLabelsOnPR(prInfo.Labels)
	if len(v) == 0 {
		return nil
	}

	return bot.cli.RemovePRLabel(prInfo.Org, prInfo.Repo, prInfo.Number, strings.Join(v, ","))
}

func (bot *robot) clearLGTM(e *sdk.PullRequestEvent) error {
	prInfo := giteeclient.GetPRInfoByPREvent(e)

	v := getLGTMLabelsOnPR(prInfo.Labels)
	if len(v) == 0 {
		return nil
	}

	return bot.cli.RemovePRLabel(prInfo.Org, prInfo.Repo, prInfo.Number, strings.Join(v, ","))
}

func (bot *robot) complexLgtmPrepare(label string, info giteeclient.PRInfo) error {
	repoLabels, err := bot.cli.GetRepoLabels(info.Org, info.Repo)
	if err != nil {
		return err
	}

	has := false
	for _, v := range repoLabels {
		if v.Name == label {
			has = false
		}
	}
	if !has {
		return bot.cli.CreateRepoLabel(info.Org, info.Repo, label, "")
	}

	return nil
}

func lgtmLabelContent(commenter string, lgtmCount uint8) string {
	if lgtmCount <= 1 {
		return lgtmLabel
	}

	labelLGTM := fmt.Sprintf("%s-%s", lgtmLabel, strings.ToLower(commenter))

	if len(labelLGTM) > labelLenLimit {
		return labelLGTM[:labelLenLimit]
	}

	return labelLGTM
}

func getLGTMLabelsOnPR(labels sets.String) []string {
	var lgtmLabels []string

	for l := range labels {
		if strings.HasPrefix(l, lgtmLabel) {
			lgtmLabels = append(lgtmLabels, l)
		}
	}

	return lgtmLabels
}
