package graphqlbackend

import (
	"context"
	"regexp"

	"github.com/sourcegraph/sourcegraph/cmd/frontend/backend"
	"github.com/sourcegraph/sourcegraph/internal/database"
)

// SearchFilterSuggestions provides search filter and default value suggestions.
func (r *schemaResolver) SearchFilterSuggestions(ctx context.Context) (*searchFilterSuggestions, error) {
	settings, err := decodedViewerFinalSettings(ctx, r.db)
	if err != nil {
		return nil, err
	}

	// List at most 10 repositories as default suggestions.
	repos, err := backend.Repos.List(ctx, database.ReposListOptions{
		OrderBy: database.RepoListOrderBy{
			{
				Field:      database.RepoListStars,
				Descending: true,
				Nulls:      "LAST",
			},
		},
		LimitOffset: &database.LimitOffset{
			Limit: 10,
		},
	})
	if err != nil {
		return nil, err
	}
	repoNames := make([]string, len(repos))

	if getBoolPtr(settings.SearchGlobbing, false) {
		for i := range repos {
			repoNames[i] = string(repos[i].Name)
		}
	} else {
		for i := range repos {
			repoNames[i] = "^" + regexp.QuoteMeta(string(repos[i].Name)) + "$"
		}
	}

	return &searchFilterSuggestions{
		repos: repoNames,
	}, nil
}

// searchFilterSuggestions holds suggestions of search filters and their default values.
type searchFilterSuggestions struct {
	repos []string
}

// Repo returns a list of repositories as the default value for suggestion.
func (s *searchFilterSuggestions) Repo() []string {
	return s.repos
}
