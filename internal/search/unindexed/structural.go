package unindexed

import (
	"context"

	"github.com/cockroachdb/errors"
	"github.com/inconshreveable/log15"
	"github.com/sourcegraph/sourcegraph/internal/search"
	"github.com/sourcegraph/sourcegraph/internal/search/result"
	"github.com/sourcegraph/sourcegraph/internal/search/streaming"
	zoektutil "github.com/sourcegraph/sourcegraph/internal/search/zoekt"
	"golang.org/x/sync/errgroup"
)

// repoData represents an object of repository revisions to search.
type repoData interface {
	AsList() []*search.RepositoryRevisions
	IsIndexed() bool
}

type IndexedMap map[string]*search.RepositoryRevisions

func (m IndexedMap) AsList() []*search.RepositoryRevisions {
	reposList := make([]*search.RepositoryRevisions, 0, len(m))
	for _, repo := range m {
		reposList = append(reposList, repo)
	}
	return reposList
}

func (IndexedMap) IsIndexed() bool {
	return true
}

type UnindexedList []*search.RepositoryRevisions

func (ul UnindexedList) AsList() []*search.RepositoryRevisions {
	return ul
}

func (UnindexedList) IsIndexed() bool {
	return false
}

// searchRepos represent the arguments to a search called over repositories.
type searchRepos struct {
	args    *search.SearcherParameters
	repoSet repoData
	stream  streaming.Sender
}

// getJob returns a function parameterized by ctx to search over repos.
func (s *searchRepos) getJob(ctx context.Context) func() error {
	return func() error {
		return callSearcherOverRepos(ctx, s.args, s.stream, s.repoSet.AsList(), s.repoSet.IsIndexed())
	}
}

func runJobs(ctx context.Context, jobs []*searchRepos) error {
	g, ctx := errgroup.WithContext(ctx)
	for _, j := range jobs {
		g.Go(j.getJob(ctx))
	}
	return g.Wait()
}

// repoSets returns the set of repositories to search (whether indexed or unindexed) based on search mode.
func repoSets(request zoektutil.IndexedSearchRequest, mode search.GlobalSearchMode) []repoData {
	repoSets := []repoData{UnindexedList(request.UnindexedRepos())} // unindexed included by default
	if mode != search.SearcherOnly {
		repoSets = append(repoSets, IndexedMap(request.IndexedRepos()))
	}
	return repoSets
}

// getting request depends on ctx which is parameterized by file match limit. also stream, but we probably don't need WithLimit before that for stream. we need it for ctx.
// then, args has a file match value inside that determines request. but this doesn't matter either. so
// all we _really_ need is to parameterize TextSearchRequest with a context on construction -> to do with limit.
// so we need TextSearchRequest with limit.

// TextSearchRequest(ctx context.Context, args *search.TextParameters, onMissing zoektutil.OnMissingRepoRevs) (zoektutil.IndexedSearchRequest, error)

// streamStructuralSearch runs structural search jobs and streams the results.
func streamStructuralSearch(ctx context.Context, args *search.TextParameters, fileMatchLimit int32, stream streaming.Sender) (err error) {
	g := func(ctx context.Context) ([]repoData, error) {
		request, err := TextSearchRequest(ctx, args, zoektutil.MissingRepoRevStatus(stream))
		if err != nil {
			return nil, err
		}
		return repoSets(request, args.Mode), nil
	}

	f := func() error {
		ctx, stream, cleanup := streaming.WithLimit(ctx, stream, int(fileMatchLimit))
		defer cleanup()

		repos, err := g(ctx)
		if err != nil {
			return err
		}

		jobs := []*searchRepos{}
		for _, repoSet := range repos {
			searcherArgs := &search.SearcherParameters{
				SearcherURLs:    args.SearcherURLs,
				PatternInfo:     args.PatternInfo,
				UseFullDeadline: args.UseFullDeadline,
			}

			jobs = append(jobs, &searchRepos{args: searcherArgs, stream: stream, repoSet: repoSet})
		}
		return runJobs(ctx, jobs)
	}
	return f()
}

// retryStructuralSearch runs a structural search with an updated file match limit so
// that Zoekt resolves more potential file matches.
func retryStructuralSearch(ctx context.Context, args *search.TextParameters, fileMatchLimit int32, stream streaming.Sender) error {
	patternCopy := *(args.PatternInfo)
	patternCopy.FileMatchLimit = fileMatchLimit
	argsCopy := *args
	argsCopy.PatternInfo = &patternCopy
	args = &argsCopy
	return streamStructuralSearch(ctx, args, fileMatchLimit, stream)
}

func StructuralSearch(ctx context.Context, args *search.TextParameters, fileMatchLimit int32, stream streaming.Sender) error {
	if fileMatchLimit != search.DefaultMaxSearchResults {
		// streamStructuralSearch performs a streaming search when the user sets a value
		// for `count`. The first return parameter indicates whether the request was
		// serviced with streaming.
		return streamStructuralSearch(ctx, args, fileMatchLimit, stream)
	}

	// For structural search with default limits we retry if we get no results.
	fileMatches, stats, err := streaming.CollectStream(func(stream streaming.Sender) error {
		return streamStructuralSearch(ctx, args, fileMatchLimit, stream)
	})

	if len(fileMatches) == 0 && err == nil {
		// retry structural search with a higher limit.
		fileMatches, stats, err = streaming.CollectStream(func(stream streaming.Sender) error {
			return retryStructuralSearch(ctx, args, 1000, stream)
		})
		if err != nil {
			return err
		}

		if len(fileMatches) == 0 {
			// Still no results? Give up.
			log15.Warn("Structural search gives up after more exhaustive attempt. Results may have been missed.")
			stats.IsLimitHit = false // Ensure we don't display "Show more".
		}
	}

	matches := make([]result.Match, 0, len(fileMatches))
	for _, fm := range fileMatches {
		if _, ok := fm.(*result.FileMatch); !ok {
			return errors.Errorf("StructuralSearch failed to convert results")
		}
		matches = append(matches, fm)
	}

	stream.Send(streaming.SearchEvent{
		Results: matches,
		Stats:   stats,
	})
	return err
}
