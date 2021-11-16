package main

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"regexp"

	"github.com/opensourceways/community-robot-lib/giteeclient"
	"github.com/opensourceways/repo-file-cache/models"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/yaml"
)

const ownerFile = "OWNERS"

var reSigsPath = regexp.MustCompile(`^sigs/[-\w]+/`)

func (bot *robot) hasPermission(
	commenter string,
	pr giteeclient.PRInfo,
	cfg *botConfig,
	log *logrus.Entry,
) (bool, error) {
	p, err := bot.cli.GetUserPermissionsOfRepo(pr.Org, pr.Repo, commenter)
	if err != nil {
		return false, err
	}

	if p.Permission == "admin" || p.Permission == "write" {
		return true, nil
	}

	if v, err := bot.getRepoOwners(pr, log); err != nil || !v.Has(commenter) {
		return false, err
	}

	if len(cfg.ReposOfSig) > 0 {
		v := sets.NewString(cfg.ReposOfSig...)
		if !v.Has(fmt.Sprintf("%s/%s", pr.Org, pr.Repo)) {
			return false, nil
		}
	}

	return bot.isOwnerOfSig(commenter, pr, cfg, log)
}

func (bot *robot) getRepoOwners(pr giteeclient.PRInfo, log *logrus.Entry) (sets.String, error) {
	v, err := bot.cli.GetPathContent(pr.Org, pr.Repo, ownerFile, pr.BaseRef)
	if err != nil || v.Content == "" {
		return nil, err
	}

	return decodeOwnerFile(v.Content, log), nil
}

func (bot *robot) isOwnerOfSig(
	commenter string,
	pr giteeclient.PRInfo,
	cfg *botConfig,
	log *logrus.Entry,
) (bool, error) {
	changes, err := bot.cli.GetPullRequestChanges(pr.Org, pr.Repo, pr.Number)
	if err != nil {
		return false, err
	}

	pathes := sets.NewString()
	for _, file := range changes {
		if !reSigsPath.MatchString(file.Filename) {
			return false, nil
		}
		pathes.Insert(filepath.Dir(file.Filename))
	}

	if pathes.Len() == 0 {
		return false, nil
	}

	param := models.Branch{
		Platform: "gitee",
		Org:      pr.Org,
		Repo:     pr.Repo,
		Branch:   pr.BaseRef,
	}

	files, err := bot.cacheCli.GetFiles(param, ownerFile, true)
	if err != nil {
		return false, err
	}
	if len(files.Files) == 0 {
		log.WithFields(
			logrus.Fields{
				"org":    pr.Org,
				"repo":   pr.Repo,
				"branch": pr.BaseRef,
			},
		).Info("there is not files stored in cache.")

		return false, nil
	}

	for _, v := range files.Files {
		p := v.Path.Dir()
		if !pathes.Has(p) {
			continue
		}

		if o := decodeOwnerFile(v.Content, log); !o.Has(commenter) {
			return false, nil
		}

		pathes.Delete(p)

		if len(pathes) == 0 {
			return true, nil
		}
	}

	return false, nil
}

func decodeOwnerFile(content string, log *logrus.Entry) sets.String {
	owners := sets.NewString()

	c, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		log.WithError(err).Error("decode file")
		return owners
	}

	var m struct {
		Maintainers []string `yaml:"maintainers"`
	}

	if err = yaml.Unmarshal(c, &m); err != nil {
		log.WithError(err).Error("code yaml file")
		return owners
	}

	if len(m.Maintainers) > 0 {
		owners.Insert(m.Maintainers...)
	}
	return owners
}
