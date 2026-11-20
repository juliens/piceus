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
	s.statesByName = map[string][]plugin.State{}
	s.statesLoaded = false

	states, err := s.pg.ListStates(ctx)
	if err != nil {
		log.Ctx(ctx).Warn().Err(err).Msg("Unable to load the plugin states: every plugin is read again from GitHub, and no plugin is created")
		return
	}

	s.statesLoaded = true

	for _, state := range states {
		s.statesByName[state.Name] = append(s.statesByName[state.Name], state)

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

// isDisabled reports whether a plugin with this name is disabled in plugin-service.
// GetByName does not return a disabled plugin: without this check, store would create it again.
func (s *Scrapper) isDisabled(name string) bool {
	for _, state := range s.statesByName[name] {
		if state.Disabled {
			return true
		}
	}

	return false
}

// upToDate reports whether plugin-service already has this latest version and these stars for the plugin.
// It uses the loaded state when there is exactly one plugin with this name, and asks plugin-service otherwise.
func (s *Scrapper) upToDate(ctx context.Context, name, latestVersion string, stars int) bool {
	if states := s.statesByName[name]; len(states) == 1 {
		return states[0].LatestVersion == latestVersion && states[0].Stars == stars
	}

	prev, err := s.pg.GetByName(ctx, name)

	return err == nil && prev != nil && prev.LatestVersion == latestVersion && prev.Stars == stars
}

// recordTagsETag records the tags ETag of a known plugin, so the next run gets a 304.
func (s *Scrapper) recordTagsETag(ctx context.Context, state plugin.State, tagsETag string) {
	if tagsETag == "" || tagsETag == state.TagsETag {
		return
	}

	s.patchPlugin(ctx, state, plugin.Patch{TagsETag: &tagsETag})
}

// recordRepositoryState records the stars and the hidden flag of a known plugin when they changed.
// The plugin is not read again: its tags did not change.
func (s *Scrapper) recordRepositoryState(ctx context.Context, state plugin.State, repository *github.Repository) {
	var patch plugin.Patch

	if stars := repository.GetStargazersCount(); stars != state.Stars {
		patch.Stars = new(stars)
	}

	if hidden := slices.Contains(repository.Topics, hiddenTopic); hidden != state.Hidden {
		patch.Hidden = new(hidden)
	}

	if patch.IsEmpty() {
		return
	}

	s.patchPlugin(ctx, state, patch)
}

// patchPlugin updates only the fields of the patch for a known plugin.
// An error is only logged: the plugin itself is fine, and the next run tries again.
func (s *Scrapper) patchPlugin(ctx context.Context, state plugin.State, patch plugin.Patch) {
	logger := log.Ctx(ctx).With().Str("module_name", state.Name).Interface("patch", patch).Logger()

	if s.dryRun {
		logger.Info().Msg("Dry run, not patching the plugin")

		return
	}

	if err := s.pg.Patch(ctx, state.ID, patch); err != nil {
		logger.Error().Err(err).Msg("Failed to patch the plugin")
	}
}
