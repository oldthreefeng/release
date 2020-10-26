/*
Copyright 2020 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package release

import (
	"fmt"
	"strings"

	"github.com/blang/semver"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"k8s.io/release/pkg/git"
	"k8s.io/release/pkg/util"
)

// GitObjectPusher is an object that pushes things to a gitrepo
type GitObjectPusher struct {
	repo git.Repo
	opts *GitObjectPusherOptions
}

var dryRunLabel = map[bool]string{true: " --dry-run", false: ""}

// GitObjectPusherOptions struct to hold the pusher options
type GitObjectPusherOptions struct {
	// Flago simulate pushes, passes --dry-run to git
	DryRun bool

	// Number of times to retry pushes
	MaxRetries int

	// Path to the repository
	RepoPath string
}

// NewGitPusher returns a new git object pusher
func NewGitPusher(opts *GitObjectPusherOptions) (*GitObjectPusher, error) {
	repo, err := git.OpenRepo(opts.RepoPath)
	if err != nil {
		return nil, errors.Wrap(err, "while opening repository")
	}

	logrus.Infof("Checkout %s branch to push objects", git.DefaultBranch)
	if err := repo.Checkout(git.DefaultBranch); err != nil {
		return nil, errors.Wrapf(err, "checking out %s branch", git.DefaultBranch)
	}

	// Pass the dry-run flag to the repo
	if opts.DryRun {
		logrus.Debug("Setting dry run flag to repository, pushing will be simuluated")
		repo.SetDry()
	}

	// Set the number of retries for the git operations:
	repo.SetMaxRetries(opts.MaxRetries)

	return &GitObjectPusher{
		repo: *repo,
		opts: opts,
	}, nil
}

// PushBranch pushes a branch to the repository
//  this function is idempotent.
func (gp *GitObjectPusher) PushBranch(branchName string) error {
	// Check if the branch name is correct
	if err := gp.checkBranchName(branchName); err != nil {
		return errors.Wrap(err, "checking branch name")
	}

	// To be able to push a branch the ref has to exist in the local repo:
	branchExists, err := gp.repo.HasBranch(branchName)
	if err != nil {
		return errors.Wrap(err, "checking if branch already exists locally")
	}
	if !branchExists {
		return errors.New(fmt.Sprintf("Unable to push branch %s, it does not exist in the local repo", branchName))
	}

	// Check if the remote branch exists already:
	branchExists, err = gp.repo.HasRemoteBranch(branchName)
	if err != nil {
		return errors.Wrapf(err, "checking if branch %s exists in remote repository", branchName)
	}

	// If the branch already exists in the remote repo, we do not do anything
	if branchExists {
		logrus.Infof("Branch %s already exists in the default remote. Noop.", branchName)
		return nil
	}

	logrus.Infof("Pushing%s %s branch:", dryRunLabel[gp.opts.DryRun], branchName)
	if err := gp.repo.Push(branchName); err != nil {
		return errors.Wrapf(err, "pushing branch %s", branchName)
	}
	logrus.Infof("Branch %s pushed successfully", branchName)
	return nil
}

// PushTag pushes a tag to the master repo
func (gp *GitObjectPusher) PushTag(newTag string) (err error) {
	// Verify that the tag is a valid tag
	if err := gp.checkTagName(newTag); err != nil {
		return errors.Wrap(err, "parsing version tag")
	}

	// Check if tag already exists
	currentTags, err := gp.repo.TagsForBranch(git.DefaultBranch)
	if err != nil {
		return errors.Wrap(err, "checking if tag exists")
	}

	// verify that the tag exists locally before trying to push
	tagExists := false
	for _, tag := range currentTags {
		if tag == newTag {
			tagExists = true
			break
		}
	}
	if !tagExists {
		return errors.Errorf("unable to push tag %s, it does not exist in the repo yet", newTag)
	}

	// CHeck if tag already exists in the remote repo
	tagExists, err = gp.repo.HasRemoteTag(newTag)
	if err != nil {
		return errors.Wrapf(err, "checking of tag %s exists", newTag)
	}

	// If the tag already exists in the remote, we return success
	if tagExists {
		logrus.Infof("Tag %s already exists in remote. Noop.", newTag)
		return nil
	}

	logrus.Infof("Pushing%s tag for version %s", dryRunLabel[gp.opts.DryRun], newTag)

	// Push the new tag, retrying up to opts.MaxRetries times
	if err := gp.repo.Push(newTag); err != nil {
		return errors.Wrapf(err, "pushing tag %s", newTag)
	}

	logrus.Infof("Successfully pushed tag %s", newTag)
	return nil
}

// checkTagName verifies that the specified tag name is valid
func (gp *GitObjectPusher) checkTagName(tagName string) error {
	_, err := util.TagStringToSemver(tagName)
	if err != nil {
		return errors.Wrap(err, "tranforming tag into semver")
	}
	return nil
}

// checkBranchName verifies that the branch name is valid
func (gp *GitObjectPusher) checkBranchName(branchName string) error {
	if !strings.HasPrefix(branchName, "release-") {
		return errors.New("Branch name has to start with release-")
	}
	versionTag := strings.TrimPrefix(branchName, "release-")
	// Add .0 and check is we get a valid semver
	_, err := semver.Parse(versionTag + ".0")
	if err != nil {
		return errors.Wrap(err, "parsing semantic version in branchname")
	}
	return nil
}