package core

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-github/v57/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/traefik/piceus/internal/plugin"
)

func Test_repoKey(t *testing.T) {
	testCases := []struct {
		name     string
		expected string
	}{
		{name: "github.com/traefik/plugindemo", expected: "traefik/plugindemo"},
		{name: "github.com/Traefik/PluginDemo/v2", expected: "traefik/plugindemo"},
		{name: "github.com/traefik/plugindemo/sub/pkg", expected: "traefik/plugindemo"},
		{name: "github.com/traefik", expected: ""},
		{name: "gitlab.com/traefik/plugindemo", expected: ""},
		{name: "", expected: ""},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.expected, repoKey(test.name))
		})
	}
}

func TestScrapper_loadStates(t *testing.T) {
	pgClient := &mockPluginClient{listStates: func() ([]plugin.State, error) {
		return []plugin.State{
			{ID: "1", Name: "github.com/traefik/plugindemo"},
			{ID: "2", Name: "github.com/Owner/Renamed"},
			{ID: "3", Name: "github.com/owner/renamed/v2"},
			{ID: "4", Name: "example.com/not/github"},
		}, nil
	}}

	s := NewScrapper(nil, nil, pgClient, true, nil, nil, nil)
	s.loadStates(context.Background())

	state, ok := s.stateOf(&github.Repository{FullName: new("Traefik/PluginDemo")})
	require.True(t, ok)
	assert.Equal(t, "1", state.ID)

	// Two plugins come from the same repository: none is used.
	_, ok = s.stateOf(&github.Repository{FullName: new("owner/renamed")})
	assert.False(t, ok)

	_, ok = s.stateOf(&github.Repository{FullName: new("other/repo")})
	assert.False(t, ok)
}

func TestScrapper_loadStates_error(t *testing.T) {
	pgClient := &mockPluginClient{listStates: func() ([]plugin.State, error) {
		return nil, errors.New("boom")
	}}

	s := NewScrapper(nil, nil, pgClient, true, nil, nil, nil)
	s.loadStates(context.Background())

	// Without the states, every plugin is read again.
	_, ok := s.stateOf(&github.Repository{FullName: new("traefik/plugindemo")})
	assert.False(t, ok)
}

func TestScrapper_process_states(t *testing.T) {
	// The versions come from the GitHub tags, not from the Go proxy.
	t.Setenv(PrivateModeEnv, "true")

	fixtureDir := filepath.Join("fixtures", "simple")

	manifest, err := os.ReadFile(filepath.Join(fixtureDir, manifestFile))
	require.NoError(t, err)

	goMod, err := os.ReadFile(filepath.Join(fixtureDir, "go.mod"))
	require.NoError(t, err)

	// The files are read at the commit of the tag.
	files := map[string]string{
		manifestFile + "@abc123": string(manifest) + "\ntestData: {}\n",
		"go.mod@abc123":          string(goMod),
	}

	const etag = `"abc"`

	known := plugin.State{ID: "id1", Name: "github.com/traefik/plugintestsimple", LatestVersion: "v1.0.0", Stars: 3, TagsETag: etag}

	// fullRead are the requests to read the plugin again.
	fullRead := map[string]int{
		"GET /repos/{owner}/{repo}/tags":               1,
		"GET /repos/{owner}/{repo}/contents/{path...}": 2,
		"GET /repos/{owner}/{repo}/readme":             1,
	}

	// tagsOnly is the request of a plugin whose tags did not change: a 304 is not counted in the GitHub rate limit.
	tagsOnly := map[string]int{"GET /repos/{owner}/{repo}/tags": 1}

	testCases := []struct {
		desc string
		// state is the plugin state in plugin-service, nil when the plugin is not known.
		state *plugin.State
		// prev is the plugin returned by GetByName.
		prev     *plugin.Plugin
		stars    int
		hidden   bool
		dryRun   bool
		requests map[string]int
		patch    *plugin.Patch
		stored   bool
	}{
		{
			desc:     "tags not modified",
			state:    &known,
			stars:    3,
			requests: tagsOnly,
		},
		{
			// The plugin is not read again: only its stars are updated.
			desc:     "tags not modified, stars changed",
			state:    &known,
			stars:    4,
			requests: tagsOnly,
			patch:    &plugin.Patch{Stars: new(4)},
		},
		{
			desc:     "tags not modified, hidden changed",
			state:    &known,
			stars:    3,
			hidden:   true,
			requests: tagsOnly,
			patch:    &plugin.Patch{Hidden: new(true)},
		},
		{
			desc:     "tags not modified, no longer hidden",
			state:    &plugin.State{ID: "id1", Name: "github.com/traefik/plugintestsimple", LatestVersion: "v1.0.0", Stars: 3, Hidden: true, TagsETag: etag},
			stars:    3,
			requests: tagsOnly,
			patch:    &plugin.Patch{Hidden: new(false)},
		},
		{
			desc:     "tags not modified, stars and hidden changed",
			state:    &known,
			stars:    4,
			hidden:   true,
			requests: tagsOnly,
			patch:    &plugin.Patch{Stars: new(4), Hidden: new(true)},
		},
		{
			desc:     "tags not modified, stars changed, dry run",
			state:    &known,
			stars:    4,
			dryRun:   true,
			requests: tagsOnly,
		},
		{
			desc:     "tags changed, same latest version",
			state:    &plugin.State{ID: "id1", Name: "github.com/traefik/plugintestsimple", LatestVersion: "v1.0.0", Stars: 3, TagsETag: `"old"`},
			stars:    3,
			requests: fullRead,
			stored:   true,
		},
		{
			desc:     "disabled plugin at its latest version: the ETag is recorded",
			state:    &plugin.State{ID: "id1", Name: "github.com/traefik/plugintestsimple", LatestVersion: "v1.0.0", Disabled: true},
			requests: fullRead,
			patch:    &plugin.Patch{TagsETag: new(etag)},
		},
		{
			// With the ETag of the new tags, the plugin would keep its old version once enabled again.
			desc:     "disabled plugin with a new tag: the ETag is not recorded",
			state:    &plugin.State{ID: "id1", Name: "github.com/traefik/plugintestsimple", LatestVersion: "v0.9.0", Disabled: true},
			requests: fullRead,
		},
		{
			desc:     "disabled plugin, tags changed: the ETag is not recorded",
			state:    &plugin.State{ID: "id1", Name: "github.com/traefik/plugintestsimple", LatestVersion: "v1.0.0", Disabled: true, TagsETag: `"old"`},
			requests: fullRead,
		},
		{
			desc:     "disabled plugin, tags not modified",
			state:    &plugin.State{ID: "id1", Name: "github.com/traefik/plugintestsimple", Disabled: true, TagsETag: etag},
			requests: tagsOnly,
		},
		{
			desc:     "disabled plugin, tags not modified, stars changed",
			state:    &plugin.State{ID: "id1", Name: "github.com/traefik/plugintestsimple", Disabled: true, TagsETag: etag},
			stars:    4,
			requests: tagsOnly,
			patch:    &plugin.Patch{Stars: new(4)},
		},
		{
			// The other module of the repository is disabled, not this one.
			desc:     "disabled plugin with another module name",
			state:    &plugin.State{ID: "id1", Name: "github.com/traefik/plugintestsimple/v2", Disabled: true},
			stars:    3,
			requests: fullRead,
			stored:   true,
		},
		{
			// The loaded state tells that plugin-service has this version: GetByName is not called (prev is nil).
			desc:     "first run with states: the ETag is recorded",
			state:    &plugin.State{ID: "id1", Name: "github.com/traefik/plugintestsimple", LatestVersion: "v1.0.0", Stars: 3},
			stars:    3,
			requests: fullRead,
			patch:    &plugin.Patch{TagsETag: new(etag)},
		},
		{
			desc:     "first run with states, dry run",
			state:    &plugin.State{ID: "id1", Name: "github.com/traefik/plugintestsimple", LatestVersion: "v1.0.0", Stars: 3},
			stars:    3,
			dryRun:   true,
			requests: fullRead,
		},
		{
			desc:     "new plugin",
			stars:    3,
			requests: fullRead,
			stored:   true,
		},
	}

	for _, test := range testCases {
		t.Run(test.desc, func(t *testing.T) {
			requests := map[string]int{}

			gh := fakeGitHub{
				files:    files,
				readmes:  map[string]string{"abc123": "# Plugin"},
				tagSHA:   "abc123",
				tagsETag: etag,
				requests: requests,
			}.client(t)

			var patches []plugin.Patch

			pgClient := &mockPluginClient{
				getByName: func(string) (*plugin.Plugin, error) { return test.prev, nil },
				listStates: func() ([]plugin.State, error) {
					if test.state == nil {
						return nil, nil
					}

					return []plugin.State{*test.state}, nil
				},
				patch: func(id string, patch plugin.Patch) error {
					assert.Equal(t, "id1", id)
					patches = append(patches, patch)

					return nil
				},
			}

			s := NewScrapper(gh, newFakeGoProxy(t, http.StatusOK), pgClient, test.dryRun,
				func(context.Context, string, string) (string, error) { return "abc123", nil },
				nil, nil)
			s.loadStates(t.Context())

			repository := &github.Repository{
				Name:            new("plugintestsimple"),
				FullName:        new("traefik/plugintestsimple"),
				Owner:           &github.User{Login: new("traefik")},
				StargazersCount: new(test.stars),
			}
			if test.hidden {
				repository.Topics = []string{hiddenTopic}
			}

			p, err := s.process(t.Context(), repository)
			require.NoError(t, err)

			assert.Equal(t, test.requests, requests)

			if test.patch == nil {
				assert.Empty(t, patches)
			} else {
				assert.Equal(t, []plugin.Patch{*test.patch}, patches)
			}

			if !test.stored {
				assert.Nil(t, p)
				return
			}

			require.NotNil(t, p)
			assert.Equal(t, etag, p.TagsETag)
			assert.Equal(t, test.stars, p.Stars)
			assert.Equal(t, test.hidden, p.Hidden)
		})
	}
}
