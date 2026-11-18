package plugin

import "time"

// BlacklistEntry represents a repository (owner/repo) excluded from the scraping.
type BlacklistEntry struct {
	Repository string    `json:"repository"`
	Reason     string    `json:"reason,omitempty"`
	Author     string    `json:"author,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

// Plugin The plugin information.
type Plugin struct {
	ID            string         `json:"id,omitempty"`
	Name          string         `json:"name,omitempty"`
	RepoName      string         `json:"repoName,omitempty"`
	DisplayName   string         `json:"displayName,omitempty"`
	Runtime       string         `json:"runtime,omitempty"`
	Author        string         `json:"author,omitempty"`
	Type          string         `json:"type,omitempty"`
	Import        string         `json:"import,omitempty"`
	Compatibility string         `json:"compatibility,omitempty"`
	Summary       string         `json:"summary,omitempty"`
	IconURL       string         `json:"iconUrl,omitempty"`
	BannerURL     string         `json:"bannerUrl,omitempty"`
	Readme        string         `json:"readme,omitempty"`
	LatestVersion string         `json:"latestVersion,omitempty"`
	Versions      []string       `json:"versions,omitempty"`
	Stars         int            `json:"stars,omitempty"`
	Snippet       map[string]any `json:"snippet,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
	Hidden        bool           `json:"hidden,omitempty"`
	UseUnsafe     bool           `json:"useUnsafe,omitempty"`
	// TagsETag is the ETag of the GitHub tags response the plugin was read from.
	TagsETag string `json:"tagsETag,omitempty"`
}

// State is the part of a plugin that tells if it changed since the last run.
type State struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	LatestVersion string `json:"latestVersion"`
	Stars         int    `json:"stars"`
	Hidden        bool   `json:"hidden"`
	Disabled      bool   `json:"disabled"`
	TagsETag      string `json:"tagsETag"`
}

// Patch holds the plugin fields to update alone. A nil field is not updated.
type Patch struct {
	Stars    *int    `json:"stars,omitempty"`
	Hidden   *bool   `json:"hidden,omitempty"`
	TagsETag *string `json:"tagsETag,omitempty"`
}

// IsEmpty reports whether the patch updates nothing.
func (p Patch) IsEmpty() bool {
	return p.Stars == nil && p.Hidden == nil && p.TagsETag == nil
}
