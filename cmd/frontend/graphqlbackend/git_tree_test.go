package graphqlbackend

import (
	"context"
	"io/fs"
	"os"
	"testing"

	"github.com/sourcegraph/sourcegraph/cmd/frontend/backend"
	"github.com/sourcegraph/sourcegraph/internal/actor"
	"github.com/sourcegraph/sourcegraph/internal/api"
	"github.com/sourcegraph/sourcegraph/internal/authz"
	"github.com/sourcegraph/sourcegraph/internal/conf"
	"github.com/sourcegraph/sourcegraph/internal/database"
	"github.com/sourcegraph/sourcegraph/internal/database/dbmock"
	"github.com/sourcegraph/sourcegraph/internal/types"
	"github.com/sourcegraph/sourcegraph/internal/vcs/git"
	"github.com/sourcegraph/sourcegraph/internal/vcs/git/gitapi"
	"github.com/sourcegraph/sourcegraph/internal/vcs/util"
	"github.com/sourcegraph/sourcegraph/schema"
)

func TestGitTree(t *testing.T) {
	db := database.NewDB(nil)
	tests := []*Test{
		{
			Schema: mustParseGraphQLSchema(t, db),
			Query: `
				{
					repository(name: "github.com/gorilla/mux") {
						commit(rev: "` + exampleCommitSHA1 + `") {
							tree(path: "foo bar") {
								directories {
									name
									path
									url
								}
								files {
									name
									path
									url
								}
							}
						}
					}
				}
			`,
			ExpectedResult: `
{
  "repository": {
    "commit": {
      "tree": {
        "directories": [
          {
            "name": "Geoffrey's random queries.32r242442bf",
            "path": "foo bar/Geoffrey's random queries.32r242442bf",
            "url": "/github.com/gorilla/mux@1234567890123456789012345678901234567890/-/tree/foo%20bar/Geoffrey%27s%20random%20queries.32r242442bf"
          },
          {
            "name": "testDirectory",
            "path": "foo bar/testDirectory",
            "url": "/github.com/gorilla/mux@1234567890123456789012345678901234567890/-/tree/foo%20bar/testDirectory"
          }
        ],
        "files": [
          {
            "name": "% token.4288249258.sql",
            "path": "foo bar/% token.4288249258.sql",
            "url": "/github.com/gorilla/mux@1234567890123456789012345678901234567890/-/blob/foo%20bar/%25%20token.4288249258.sql"
          },
          {
            "name": "testFile",
            "path": "foo bar/testFile",
            "url": "/github.com/gorilla/mux@1234567890123456789012345678901234567890/-/blob/foo%20bar/testFile"
          }
        ]
      }
    }
  }
}
			`,
		},
	}
	testGitTree(t, tests)
}

func TestGitTree_SubRepo_Deny(t *testing.T) {
	mockDB := dbmock.NewMockDBFrom(database.NewDB(nil))
	mockDB.SubRepoPermsFunc.SetDefaultHook(func() database.SubRepoPermsStore {
		srp := dbmock.NewMockSubRepoPermsStore()
		srp.RepoSupportedFunc.SetDefaultReturn(true, nil)
		srp.GetByUserFunc.SetDefaultReturn(map[api.RepoName]authz.SubRepoPermissions{
			"github.com/gorilla/mux": {
				PathIncludes: []string{"**"},
				PathExcludes: []string{"**/foo bar/testFile"},
			},
		}, nil)
		return srp
	})

	conf.Mock(&conf.Unified{
		SiteConfiguration: schema.SiteConfiguration{
			ExperimentalFeatures: &schema.ExperimentalFeatures{
				EnableSubRepoPermissions: true,
			},
		},
	})
	defer conf.Mock(nil)

	ctx := actor.WithActor(context.Background(), actor.FromUser(1))
	tests := []*Test{
		{
			Context: ctx,
			Schema:  mustParseGraphQLSchema(t, mockDB),
			Query: `
				{
					repository(name: "github.com/gorilla/mux") {
						commit(rev: "` + exampleCommitSHA1 + `") {
							tree(path: "foo bar") {
								directories {
									name
									path
									url
								}
								files {
									name
									path
									url
								}
							}
						}
					}
				}
			`,
			ExpectedResult: `
{
  "repository": {
    "commit": {
      "tree": {
        "directories": [
          {
            "name": "Geoffrey's random queries.32r242442bf",
            "path": "foo bar/Geoffrey's random queries.32r242442bf",
            "url": "/github.com/gorilla/mux@1234567890123456789012345678901234567890/-/tree/foo%20bar/Geoffrey%27s%20random%20queries.32r242442bf"
          },
          {
            "name": "testDirectory",
            "path": "foo bar/testDirectory",
            "url": "/github.com/gorilla/mux@1234567890123456789012345678901234567890/-/tree/foo%20bar/testDirectory"
          }
        ],
        "files": [
          {
            "name": "% token.4288249258.sql",
            "path": "foo bar/% token.4288249258.sql",
            "url": "/github.com/gorilla/mux@1234567890123456789012345678901234567890/-/blob/foo%20bar/%25%20token.4288249258.sql"
          }
        ]
      }
    }
  }
}
			`,
		},
	}
	testGitTree(t, tests)
}

func testGitTree(t *testing.T, tests []*Test) {
	resetMocks()
	database.Mocks.ExternalServices.List = func(opt database.ExternalServicesListOptions) ([]*types.ExternalService, error) {
		return nil, nil
	}
	database.Mocks.Repos.MockGetByName(t, "github.com/gorilla/mux", 2)
	backend.Mocks.Repos.ResolveRev = func(ctx context.Context, repo *types.Repo, rev string) (api.CommitID, error) {
		if repo.ID != 2 || rev != exampleCommitSHA1 {
			t.Error("wrong arguments to Repos.ResolveRev")
		}
		return exampleCommitSHA1, nil
	}
	backend.Mocks.Repos.MockGetCommit_Return_NoCheck(t, &gitapi.Commit{ID: exampleCommitSHA1})

	git.Mocks.Stat = func(commit api.CommitID, path string) (fs.FileInfo, error) {
		if string(commit) != exampleCommitSHA1 {
			t.Errorf("got commit %q, want %q", commit, exampleCommitSHA1)
		}
		if want := "foo bar"; path != want {
			t.Errorf("got path %q, want %q", path, want)
		}
		return &util.FileInfo{Name_: path, Mode_: os.ModeDir}, nil
	}
	git.Mocks.ReadDir = func(commit api.CommitID, name string, recurse bool) ([]fs.FileInfo, error) {
		if string(commit) != exampleCommitSHA1 {
			t.Errorf("got commit %q, want %q", commit, exampleCommitSHA1)
		}
		if want := "foo bar"; name != want {
			t.Errorf("got name %q, want %q", name, want)
		}
		if recurse {
			t.Error("got recurse == false, want true")
		}
		return []fs.FileInfo{
			&util.FileInfo{Name_: name + "/testDirectory", Mode_: os.ModeDir},
			&util.FileInfo{Name_: name + "/Geoffrey's random queries.32r242442bf", Mode_: os.ModeDir},
			&util.FileInfo{Name_: name + "/testFile", Mode_: 0},
			&util.FileInfo{Name_: name + "/% token.4288249258.sql", Mode_: 0},
		}, nil
	}
	defer git.ResetMocks()

	RunTests(t, tests)
}
