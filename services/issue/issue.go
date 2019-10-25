// Copyright 2019 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package issue

import (
	"fmt"

	"code.gitea.io/gitea/models"
	"code.gitea.io/gitea/modules/log"
	"code.gitea.io/gitea/modules/notification"
	api "code.gitea.io/gitea/modules/structs"
)

// NewIssue creates new issue with labels for repository.
func NewIssue(repo *models.Repository, issue *models.Issue, labelIDs []int64, uuids []string) error {
	if err := models.NewIssue(repo, issue, labelIDs, uuids); err != nil {
		return err
	}

	if err := models.NotifyWatchers(&models.Action{
		ActUserID: issue.Poster.ID,
		ActUser:   issue.Poster,
		OpType:    models.ActionCreateIssue,
		Content:   fmt.Sprintf("%d|%s", issue.Index, issue.Title),
		RepoID:    repo.ID,
		Repo:      repo,
		IsPrivate: repo.IsPrivate,
	}); err != nil {
		log.Error("NotifyWatchers: %v", err)
	}

	mode, _ := models.AccessLevel(issue.Poster, issue.Repo)
	if err := models.PrepareWebhooks(repo, models.HookEventIssues, &api.IssuePayload{
		Action:     api.HookIssueOpened,
		Index:      issue.Index,
		Issue:      issue.APIFormat(),
		Repository: repo.APIFormat(mode),
		Sender:     issue.Poster.APIFormat(),
	}); err != nil {
		log.Error("PrepareWebhooks: %v", err)
	} else {
		go models.HookQueue.Add(issue.RepoID)
	}

	return nil
}

// ChangeTitle changes the title of this issue, as the given user.
func ChangeTitle(issue *models.Issue, doer *models.User, title string) (err error) {
	oldTitle := issue.Title
	issue.Title = title

	if err = issue.ChangeTitle(doer, oldTitle); err != nil {
		return
	}

	mode, _ := models.AccessLevel(issue.Poster, issue.Repo)
	if issue.IsPull {
		if err = issue.LoadPullRequest(); err != nil {
			return fmt.Errorf("loadPullRequest: %v", err)
		}
		issue.PullRequest.Issue = issue
		err = models.PrepareWebhooks(issue.Repo, models.HookEventPullRequest, &api.PullRequestPayload{
			Action: api.HookIssueEdited,
			Index:  issue.Index,
			Changes: &api.ChangesPayload{
				Title: &api.ChangesFromPayload{
					From: oldTitle,
				},
			},
			PullRequest: issue.PullRequest.APIFormat(),
			Repository:  issue.Repo.APIFormat(mode),
			Sender:      doer.APIFormat(),
		})
	} else {
		err = models.PrepareWebhooks(issue.Repo, models.HookEventIssues, &api.IssuePayload{
			Action: api.HookIssueEdited,
			Index:  issue.Index,
			Changes: &api.ChangesPayload{
				Title: &api.ChangesFromPayload{
					From: oldTitle,
				},
			},
			Issue:      issue.APIFormat(),
			Repository: issue.Repo.APIFormat(mode),
			Sender:     issue.Poster.APIFormat(),
		})
	}

	if err != nil {
		log.Error("PrepareWebhooks [is_pull: %v]: %v", issue.IsPull, err)
	} else {
		go models.HookQueue.Add(issue.RepoID)
	}

	return nil
}

// UpdateAssignees is a helper function to add or delete one or multiple issue assignee(s)
// Deleting is done the GitHub way (quote from their api documentation):
// https://developer.github.com/v3/issues/#edit-an-issue
// "assignees" (array): Logins for Users to assign to this issue.
// Pass one or more user logins to replace the set of assignees on this Issue.
// Send an empty array ([]) to clear all assignees from the Issue.
func UpdateAssignees(issue *models.Issue, oneAssignee string, multipleAssignees []string, doer *models.User) (err error) {
	var allNewAssignees []*models.User

	// Keep the old assignee thingy for compatibility reasons
	if oneAssignee != "" {
		// Prevent double adding assignees
		var isDouble bool
		for _, assignee := range multipleAssignees {
			if assignee == oneAssignee {
				isDouble = true
				break
			}
		}

		if !isDouble {
			multipleAssignees = append(multipleAssignees, oneAssignee)
		}
	}

	// Loop through all assignees to add them
	for _, assigneeName := range multipleAssignees {
		assignee, err := models.GetUserByName(assigneeName)
		if err != nil {
			return err
		}

		allNewAssignees = append(allNewAssignees, assignee)
	}

	// Delete all old assignees not passed
	if err = models.DeleteNotPassedAssignee(issue, doer, allNewAssignees); err != nil {
		return err
	}

	// Add all new assignees
	// Update the assignee. The function will check if the user exists, is already
	// assigned (which he shouldn't as we deleted all assignees before) and
	// has access to the repo.
	for _, assignee := range allNewAssignees {
		// Extra method to prevent double adding (which would result in removing)
		err = AddAssigneeIfNotAssigned(issue, doer, assignee.ID)
		if err != nil {
			return err
		}
	}

	return
}

// AddAssigneeIfNotAssigned adds an assignee only if he isn't already assigned to the issue.
// Also checks for access of assigned user
func AddAssigneeIfNotAssigned(issue *models.Issue, doer *models.User, assigneeID int64) (err error) {
	assignee, err := models.GetUserByID(assigneeID)
	if err != nil {
		return err
	}

	// Check if the user is already assigned
	isAssigned, err := models.IsUserAssignedToIssue(issue, assignee)
	if err != nil {
		return err
	}
	if isAssigned {
		// nothing to to
		return nil
	}

	valid, err := models.CanBeAssigned(assignee, issue.Repo, issue.IsPull)
	if err != nil {
		return err
	}
	if !valid {
		return models.ErrUserDoesNotHaveAccessToRepo{UserID: assigneeID, RepoName: issue.Repo.Name}
	}

	removed, comment, err := issue.ToggleAssignee(doer, assigneeID)
	if err != nil {
		return err
	}

	notification.NotifyIssueChangeAssignee(doer, issue, assignee, removed, comment)

	return nil
}

// AddAssignees adds a list of assignes (from IDs) to an issue
func AddAssignees(issue *models.Issue, doer *models.User, assigneeIDs []int64) (err error) {
	for _, assigneeID := range assigneeIDs {
		if err = AddAssigneeIfNotAssigned(issue, doer, assigneeID); err != nil {
			return err
		}
	}
	return nil
}
