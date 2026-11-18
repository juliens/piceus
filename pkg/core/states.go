package core

import (
	"context"
	"slices"
	"strings"

	"github.com/google/go-github/v57/github"
	"github.com/rs/zerolog/log"
	"github.com/traefik/piceus/internal/plugin"
)

// loadStates loads the state of all plugins from plugin-service.
// Without the states, every plugin is read again from GitHub, as before.
func (s *Scrapper) loadStates(ctx context.Context) {
	s.states = map[string][]plugin.State{}

	states, err := s.pg.ListStates(ctx)
	if err != nil {
		log.Ctx(ctx).Warn().Err(err).Msg("Unable to load the plugin states: every plugin is read again from GitHub")
		return
	}

	for _, state := range states {
		key := repoKey(state.Name)
		if key == "" {
			continue
		}

		s.states[key] = append(s.states[key], state)
	}
}

// repoKey returns "owner/repo" in lower case for a plugin name.
// The name of a plugin starts with github.com/owner/repo: checkRepoName checks it for a Yaegi plugin,
// and it is the name of a WASM plugin.
func repoKey(name string) string {
	rest, ok := strings.CutPrefix(name, "github.com/")
	if !ok {
		return ""
	}

	owner, rest, _ := strings.Cut(rest, "/")
	repo, _, _ := strings.Cut(rest, "/")

	if owner == "" || repo == "" {
		return ""
	}

	return strings.ToLower(owner + "/" + repo)
}

// stateOf returns the state of the plugin of the repository.
// It returns false when no plugin, or more than one, comes from the repository (for example after a change of the module name).
func (s *Scrapper) stateOf(repository *github.Repository) (plugin.State, bool) {
	states := s.states[strings.ToLower(repository.GetFullName())]
	if len(states) != 1 {
		return plugin.State{}, false
	}

	return states[0], true
}

// patchState updates the stars, the hidden flag and the tags ETag of a known plugin, only when they changed.
// An error is only logged: the plugin itself is fine.
func (s *Scrapper) patchState(ctx context.Context, state plugin.State, repository *github.Repository, tagsETag string) {
	var patch plugin.Patch

	if stars := repository.GetStargazersCount(); stars != state.Stars {
		patch.Stars = &stars
	}

	if hidden := slices.Contains(repository.Topics, hiddenTopic); hidden != state.Hidden {
		patch.Hidden = &hidden
	}

	if tagsETag != "" && tagsETag != state.TagsETag {
		patch.TagsETag = &tagsETag
	}

	if patch.IsEmpty() {
		return
	}

	logger := log.Ctx(ctx).With().Str("module_name", state.Name).Logger()

	if s.dryRun {
		logger.Info().Msg("Dry run, not updating the plugin state")
		logger.Debug().Interface("patch", patch).Send()

		return
	}

	if err := s.pg.Patch(ctx, state.ID, patch); err != nil {
		logger.Error().Err(err).Msg("Failed to update the plugin state")
	}
}
