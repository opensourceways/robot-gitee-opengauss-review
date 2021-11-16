package main

import (
	"encoding/base64"
	"path"
	"regexp"

	"github.com/opensourceways/community-robot-lib/giteeclient"
	"github.com/opensourceways/repo-file-cache/models"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/yaml"
)

func (bot *robot) hasPermission(
	commenter string,
	info giteeclient.PRInfo,
	cfg *botConfig,
	log *logrus.Entry,
) (bool, error) {
	p, err := bot.cli.GetUserPermissionsOfRepo(info.Org, info.Repo, commenter)
	if err != nil {
		return false, err
	}

	if p.Permission == "admin" || p.Permission == "write" {
		return true, nil
	}

	// determine if the commenter is in the OWNERS file of the repository where the event occurred
	if v, err := bot.inRepoOwnersFile(commenter, info, "OWNERS", log); err != nil || v {
		return v, err
	}

	return bot.inSigDirOwnersFile(commenter, info, cfg, log)
}

func (bot *robot) inRepoOwnersFile(
	commenter string,
	info giteeclient.PRInfo,
	path string,
	log *logrus.Entry,
) (bool, error) {
	content, err := bot.cli.GetPathContent(info.Org, info.Repo, path, info.BaseRef)
	if err != nil || content.Content == "" {
		return false, err
	}

	owners := decodeOwners(content.Content, log)

	return owners.Has(commenter), nil
}

func (bot *robot) inSigDirOwnersFile(
	commenter string,
	info giteeclient.PRInfo,
	cfg *botConfig,
	log *logrus.Entry,
) (bool, error) {
	if !cfg.isSpecialRepo(info.Repo) {
		return false, nil
	}

	cFiles, err := bot.cli.GetPullRequestChanges(info.Org, info.Repo, info.Number)
	if err != nil {
		return false, err
	}

	regSigFilePattern := regexp.MustCompile("^sigs/[a-zA-Z0-9_-]+/.+")
	filesPath := sets.NewString()

	for _, file := range cFiles {
		if !regSigFilePattern.MatchString(file.Filename) {
			return false, nil
		}

		filesPath.Insert(path.Dir(file.Filename))
	}

	if len(filesPath) == 0 {
		return false, nil
	}

	param := models.Branch{
		Platform: "gitee",
		Org:      info.Org,
		Repo:     info.Repo,
		Branch:   info.BaseRef,
	}

	files, err := bot.cacheCli.GetFiles(param, "OWNERS", true)
	if err != nil || len(files.Files) == 0 {
		return false, err
	}

	for _, v := range files.Files {
		if !filesPath.Has(string(v.Path)) {
			continue
		}

		if owners := decodeOwners(v.Content, log); !owners.Has(commenter) {
			return false, nil
		}

		filesPath.Delete(string(v.Path))
		if len(filesPath) == 0 {
			break
		}
	}

	return len(filesPath) == 0, nil
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
