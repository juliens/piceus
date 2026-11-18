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

	files := map[string]string{
		manifestFile + "@v1.0.0": string(manifest) + "\ntestData: {}\n",
		"go.mod@v1.0.0":          string(goMod),
	}

	const etag = `"abc"`

	known := plugin.State{ID: "id1", Name: "github.com/traefik/plugintestsimple", LatestVersion: "v1.0.0", Stars: 3, TagsETag: etag}

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
			requests: map[string]int{"GET /repos/{owner}/{repo}/tags": 1},
		},
		{
			desc:     "tags not modified, stars and hidden changed",
			state:    &known,
			stars:    4,
			hidden:   true,
			requests: map[string]int{"GET /repos/{owner}/{repo}/tags": 1},
			patch:    &plugin.Patch{Stars: new(4), Hidden: new(true)},
		},
		{
			desc:     "tags not modified, dry run",
			state:    &known,
			stars:    4,
			dryRun:   true,
			requests: map[string]int{"GET /repos/{owner}/{repo}/tags": 1},
		},
		{
			desc:     "disabled plugin",
			state:    &plugin.State{ID: "id1", Name: "github.com/traefik/plugintestsimple", Disabled: true},
			requests: map[string]int{},
		},
		{
			desc:  "first run with states: the ETag is recorded",
			state: &plugin.State{ID: "id1", Name: "github.com/traefik/plugintestsimple", LatestVersion: "v1.0.0", Stars: 3},
			prev:  &plugin.Plugin{ID: "id1", Name: "github.com/traefik/plugintestsimple", LatestVersion: "v1.0.0", Stars: 3},
			stars: 3,
			requests: map[string]int{
				"GET /repos/{owner}/{repo}/tags":               1,
				"GET /repos/{owner}/{repo}/contents/{path...}": 2,
				"GET /repos/{owner}/{repo}/readme":             1,
			},
			patch: &plugin.Patch{TagsETag: new(etag)},
		},
		{
			desc:  "new plugin",
			stars: 3,
			requests: map[string]int{
				"GET /repos/{owner}/{repo}/tags":               1,
				"GET /repos/{owner}/{repo}/contents/{path...}": 2,
				"GET /repos/{owner}/{repo}/readme":             1,
			},
			stored: true,
		},
	}

	for _, test := range testCases {
		t.Run(test.desc, func(t *testing.T) {
			requests := map[string]int{}

			gh := fakeGitHub{
				files:    files,
				readmes:  map[string]string{"v1.0.0": "# Plugin"},
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
		})
	}
}
