package main

import (
	"fmt"
	"regexp"
	"strings"

	sdk "github.com/opensourceways/go-gitee/gitee"
	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	msgPRConflicts        = "PR conflicts to the target branch."
	msgMissingLabels      = "PR does not have these lables: %s"
	msgInvalidLabels      = "PR should remove these labels: %s"
	msgNotEnoughLGTMLabel = "PR needs %d lgtm labels and now gets %d"
)

var regCheckPr = regexp.MustCompile(`(?mi)^/check-pr\s*$`)

func (bot *robot) handleCheckPR(e *sdk.NoteEvent, cfg *botConfig) error {
	if !e.IsPullRequest() ||
		!e.IsPROpen() ||
		!e.IsCreatingCommentEvent() ||
		!regCheckPr.MatchString(e.GetComment().GetBody()) {
		return nil
	}

	pr := e.GetPullRequest()
	org, repo := e.GetOrgRepo()

	if r := canMerge(pr.Mergeable, e.GetPRLabelSet(), cfg); len(r) > 0 {
		return bot.cli.CreatePRComment(
			org, repo, e.GetPRNumber(),
			fmt.Sprintf(
				"@%s , this pr is not mergeable and the reasons are below:\n%s",
				e.GetCommenter(), strings.Join(r, "\n"),
			),
		)
	}

	return bot.mergePR(
		pr.NeedReview || pr.NeedTest,
		org, repo, e.GetPRNumber(), string(cfg.MergeMethod),
	)
}

func (bot *robot) tryMerge(e *sdk.PullRequestEvent, cfg *botConfig) error {
	if sdk.GetPullRequestAction(e) != sdk.PRActionUpdatedLabel {
		return nil
	}

	pr := e.PullRequest

	if r := canMerge(pr.GetMergeable(), e.GetPRLabelSet(), cfg); len(r) > 0 {
		return nil
	}

	org, repo := e.GetOrgRepo()

	return bot.mergePR(
		pr.GetNeedReview() || pr.GetNeedTest(),
		org, repo, e.GetPRNumber(), string(cfg.MergeMethod),
	)
}

func (bot *robot) mergePR(needReviewOrTest bool, org, repo string, number int32, method string) error {
	if needReviewOrTest {
		v := int32(0)
		p := sdk.PullRequestUpdateParam{
			AssigneesNumber: &v,
			TestersNumber:   &v,
		}
		if _, err := bot.cli.UpdatePullRequest(org, repo, number, p); err != nil {
			return err
		}
	}

	return bot.cli.MergePR(
		org, repo, number,
		sdk.PullRequestMergePutParam{
			MergeMethod: method,
		},
	)
}

func canMerge(mergeable bool, labels sets.String, cfg *botConfig) []string {
	if !mergeable {
		return []string{msgPRConflicts}
	}

	reasons := []string{}

	needs := sets.NewString(approvedLabel)
	needs.Insert(cfg.LabelsForMerge...)

	if ln := cfg.LgtmCountsRequired; ln == 1 {
		needs.Insert(lgtmLabel)
	} else {
		v := getLGTMLabelsOnPR(labels)
		if n := uint(len(v)); n < ln {
			reasons = append(reasons, fmt.Sprintf(msgNotEnoughLGTMLabel, ln, n))
		}
	}

	if v := needs.Difference(labels); v.Len() > 0 {
		reasons = append(reasons, fmt.Sprintf(
			msgMissingLabels, strings.Join(v.UnsortedList(), ", "),
		))
	}

	if len(cfg.MissingLabelsForMerge) > 0 {
		missing := sets.NewString(cfg.MissingLabelsForMerge...)
		if v := missing.Intersection(labels); v.Len() > 0 {
			reasons = append(reasons, fmt.Sprintf(
				msgInvalidLabels, strings.Join(v.UnsortedList(), ", "),
			))
		}
	}

	return reasons
}
