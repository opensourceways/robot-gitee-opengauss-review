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
	labelHiddenValue        = "<input type=hidden value=%s />"
	lgtmAddedMessage        = `***lgtm*** is added in this pull request by: ***%s***. :wave:`
	lgtmSelfOwnMessage      = `***lgtm*** can not be added in your self-own pull request. :astonished: `
	lgtmNoPermissionMessage = `***@%s*** has no permission to %s ***lgtm*** in this pull request. :astonished:
please contact to the collaborators in this repository.`
)

var (
	regAddLgtm    = regexp.MustCompile(`(?mi)^/lgtm\s*$`)
	regRemoveLgtm = regexp.MustCompile(`(?mi)^/lgtm cancel\s*$`)
)

func (bot *robot) handleLGTM(e giteeclient.PRNoteEvent, cfg libconfig.PluginConfig, log *logrus.Entry) error {
	org, repo := e.GetOrgRep()
	bCfg, err := bot.getConfig(cfg, org, repo)
	if err != nil {
		return err
	}

	toAdd, toRemove := doWhat(e.GetComment())
	if toAdd {
		return bot.addLGTM(bCfg, e, log)
	}
	if toRemove {
		return bot.removeLGTM(bCfg, e, log)
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

	label := lgtmLabelContent(commenter, cfg.MultipleLGTMLabel)
	if err := bot.cli.AddMultiPRLabel(prInfo.Org, prInfo.Repo, prInfo.Number, []string{label}); err != nil {
		return err
	}

	comment := fmt.Sprintf(lgtmAddedMessage, commenter)
	if !cfg.CloseStoreSha {
		comment += fmt.Sprintf(labelHiddenValue, prInfo.HeadSHA)
	}
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

		label := lgtmLabelContent(commenter, cfg.MultipleLGTMLabel)
		return bot.cli.RemovePRLabel(prInfo.Org, prInfo.Repo, prInfo.Number, label)
	}

	// the commenter can remove all of lgtm[-login name] kind labels that who is the pr author
	rmLabels := prCurrentLGTMLabels(prInfo.Labels)
	if len(rmLabels) == 0 {
		return nil
	}

	removeLabels := strings.Join(rmLabels, ",")
	return bot.cli.RemovePRLabel(prInfo.Org, prInfo.Repo, prInfo.Number, removeLabels)
}

func (bot *robot) clearLGTM(cfg libconfig.PluginConfig, e *sdk.PullRequestEvent, log *logrus.Entry) error {
	prInfo := giteeclient.GetPRInfoByPREvent(e)

	bConfig, err := bot.getConfig(cfg, prInfo.Org, prInfo.Repo)
	if err != nil {
		return err
	}

	curLgtmLabels := prCurrentLGTMLabels(prInfo.Labels)
	if len(curLgtmLabels) == 0 {
		return nil
	}

	if !bConfig.CloseStoreSha {
		lastSha := bot.getLgtmLastCommentSha(prInfo)
		if lastSha != "" {
			commit, err := bot.cli.GetPRCommit(prInfo.Org, prInfo.Repo, prInfo.HeadSHA)
			if err != nil {
				log.WithField("sha", prInfo.HeadSHA).WithError(err).Error("Failed to get commit.")
			}

			if commit.Commit != nil && commit.Commit.Tree != nil && commit.Commit.Tree.Sha == lastSha {
				// Don't remove the label, PR code hasn't changed
				log.Infof("Keeping LGTM label as the tree-hash remained the same: %s", commit.Commit.Tree.Sha)
				return nil
			}
		}
	}

	removeLabels := strings.Join(curLgtmLabels, ",")
	return bot.cli.RemovePRLabel(prInfo.Org, prInfo.Repo, prInfo.Number, removeLabels)
}

func doWhat(comment string) (bool, bool) {
	// If we create an "/lgtm" comment, add lgtm if necessary.
	if regAddLgtm.MatchString(comment) {
		return true, false
	}

	// If we create a "/lgtm cancel" comment, remove lgtm if necessary.
	if regRemoveLgtm.MatchString(comment) {
		return false, true
	}

	return false, false
}

func lgtmLabelContent(commenter string, multipleLGTM bool) string {
	if !multipleLGTM {
		return lgtmLabel
	}

	labelLGTM := fmt.Sprintf("%s-%s", lgtmLabel, strings.ToLower(commenter))
	// the gitee platform limits the length of labels to a maximum of 20
	if len(labelLGTM) > 20 {
		return labelLGTM[:20]
	}
	return labelLGTM
}

func prCurrentLGTMLabels(labels sets.String) []string {
	var lgtmLabels []string
	for l := range labels {
		if strings.HasPrefix(l, lgtmLabel) {
			lgtmLabels = append(lgtmLabels, l)
		}
	}
	return lgtmLabels
}
