package core

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/go-github/v57/github"
	"github.com/traefik/piceus/pkg/audit"
	"golang.org/x/mod/modfile"
)

// errPinnedCommit is returned when the Go module proxy cannot be read: it is not a plugin problem.
var errPinnedCommit = errors.New("failed to get the commit pinned by the Go module proxy")

// PinnedCommit returns the commit the Go module proxy pinned for module@version, or audit.ErrNotPinned.
type PinnedCommit func(ctx context.Context, module, version string) (string, error)

// yaegiRef returns the ref to read the Yaegi plugin from: the commit the Go module proxy pinned at publication,
// that plugin-service serves once the tag moved, or the tag when the proxy pinned none.
// When the ref is the tag, it also returns the go.mod read at the tag, or nil when it did not read it.
func (s *Scrapper) yaegiRef(ctx context.Context, repository *github.Repository, latest tag) (string, *modfile.File, error) {
	version := latest.Name

	// The Go module proxy cannot fetch a private repository: do not send it its name.
	if s.pinnedCommit == nil || repository.GetPrivate() {
		return version, nil, nil
	}

	mod, err := s.getModuleInfo(ctx, repository, version)
	if err != nil {
		return "", nil, err
	}

	if mod.Module == nil {
		return version, mod, nil
	}

	pinned, err := s.pinnedCommitWithRetry(ctx, mod.Module.Mod.Path, version)

	switch {
	case errors.Is(err, audit.ErrNotPinned):
		return version, mod, nil
	case err != nil:
		return "", nil, fmt.Errorf("%w: %w", errPinnedCommit, err)
	}

	// The tag did not move: the files at the tag are the pinned ones.
	if latest.SHA != "" && strings.EqualFold(pinned, latest.SHA) {
		return version, mod, nil
	}

	// plugin-service serves the tag once the pinned commit is gone from GitHub.
	gone, err := s.commitGone(ctx, repository, pinned)
	if err != nil {
		return "", nil, err
	}

	if gone {
		return version, mod, nil
	}

	return pinned, nil, nil
}

// pinnedCommitWithRetry calls pinnedCommit, and tries again twice when the Go module proxy fails.
func (s *Scrapper) pinnedCommitWithRetry(ctx context.Context, module, version string) (string, error) {
	var pinned string
	var err error

	for attempt := range 3 {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(time.Duration(attempt) * s.retryWait):
			}
		}

		pinned, err = s.pinnedCommit(ctx, module, version)
		if err == nil || errors.Is(err, audit.ErrNotPinned) {
			break
		}
	}

	return pinned, err
}

// commitGone reports whether GitHub no longer has the commit sha.
func (s *Scrapper) commitGone(ctx context.Context, repository *github.Repository, sha string) (bool, error) {
	// A conditional request answered 304 is not counted in the GitHub rate limit.
	_, resp, err := s.gh.Repositories.GetCommitSHA1(ctx, repository.GetOwner().GetLogin(), repository.GetName(), sha, sha)

	switch {
	case err == nil, resp != nil && resp.StatusCode == http.StatusNotModified:
		return false, nil
	case resp != nil && (resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnprocessableEntity):
		return true, nil
	default:
		return false, fmt.Errorf("check commit %s: %w", sha, err)
	}
}
