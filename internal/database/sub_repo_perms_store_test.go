package database

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcegraph/sourcegraph/internal/api"
	"github.com/sourcegraph/sourcegraph/internal/authz"
	"github.com/sourcegraph/sourcegraph/internal/database/dbtest"
	"github.com/sourcegraph/sourcegraph/internal/database/dbutil"
)

func TestSubRepoPermsInsert(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	t.Parallel()

	db := dbtest.NewDB(t)

	ctx := context.Background()
	prepareSubRepoTestData(ctx, t, db)
	s := SubRepoPerms(db)

	userID := int32(1)
	repoID := api.RepoID(1)
	perms := authz.SubRepoPermissions{
		PathIncludes: []string{"/src/foo/*"},
		PathExcludes: []string{"/src/bar/*"},
	}
	if err := s.Upsert(ctx, userID, repoID, perms); err != nil {
		t.Fatal(err)
	}

	have, err := s.Get(ctx, userID, repoID)
	if err != nil {
		t.Fatal(err)
	}

	if diff := cmp.Diff(&perms, have); diff != "" {
		t.Fatal(diff)
	}
}

func TestSubRepoPermsUpsert(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	t.Parallel()

	db := dbtest.NewDB(t)

	ctx := context.Background()
	prepareSubRepoTestData(ctx, t, db)
	s := SubRepoPerms(db)

	userID := int32(1)
	repoID := api.RepoID(1)
	perms := authz.SubRepoPermissions{
		PathIncludes: []string{"/src/foo/*"},
		PathExcludes: []string{"/src/bar/*"},
	}
	// Insert initial data
	if err := s.Upsert(ctx, userID, repoID, perms); err != nil {
		t.Fatal(err)
	}

	// Upsert to change perms
	perms = authz.SubRepoPermissions{
		PathIncludes: []string{"/src/foo_upsert/*"},
		PathExcludes: []string{"/src/bar_upsert/*"},
	}
	if err := s.Upsert(ctx, userID, repoID, perms); err != nil {
		t.Fatal(err)
	}

	have, err := s.Get(ctx, userID, repoID)
	if err != nil {
		t.Fatal(err)
	}

	if diff := cmp.Diff(&perms, have); diff != "" {
		t.Fatal(diff)
	}
}

func TestSubRepoPermsUpsertWithSpec(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	t.Parallel()

	db := dbtest.NewDB(t)

	ctx := context.Background()
	prepareSubRepoTestData(ctx, t, db)
	s := SubRepoPerms(db)

	userID := int32(1)
	repoID := api.RepoID(1)
	perms := authz.SubRepoPermissions{
		PathIncludes: []string{"/src/foo/*"},
		PathExcludes: []string{"/src/bar/*"},
	}
	spec := api.ExternalRepoSpec{
		ID:          "MDEwOlJlcG9zaXRvcnk0MTI4ODcwOA==",
		ServiceType: "github",
		ServiceID:   "https://github.com/",
	}
	// Insert initial data
	if err := s.UpsertWithSpec(ctx, userID, spec, perms); err != nil {
		t.Fatal(err)
	}

	// Upsert to change perms
	perms = authz.SubRepoPermissions{
		PathIncludes: []string{"/src/foo_upsert/*"},
		PathExcludes: []string{"/src/bar_upsert/*"},
	}
	if err := s.UpsertWithSpec(ctx, userID, spec, perms); err != nil {
		t.Fatal(err)
	}

	have, err := s.Get(ctx, userID, repoID)
	if err != nil {
		t.Fatal(err)
	}

	if diff := cmp.Diff(&perms, have); diff != "" {
		t.Fatal(diff)
	}
}

func TestSubRepoPermsGetByUser(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	t.Parallel()

	db := dbtest.NewDB(t)

	ctx := context.Background()
	s := SubRepoPerms(db)
	prepareSubRepoTestData(ctx, t, db)

	userID := int32(1)
	perms := authz.SubRepoPermissions{
		PathIncludes: []string{"/src/foo/*"},
		PathExcludes: []string{"/src/bar/*"},
	}
	if err := s.Upsert(ctx, userID, api.RepoID(1), perms); err != nil {
		t.Fatal(err)
	}

	userID = int32(1)
	perms = authz.SubRepoPermissions{
		PathIncludes: []string{"/src/foo2/*"},
		PathExcludes: []string{"/src/bar2/*"},
	}
	if err := s.Upsert(ctx, userID, api.RepoID(2), perms); err != nil {
		t.Fatal(err)
	}

	have, err := s.GetByUser(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}

	want := map[api.RepoName]authz.SubRepoPermissions{
		"github.com/foo/bar": {
			PathIncludes: []string{"/src/foo/*"},
			PathExcludes: []string{"/src/bar/*"},
		},
		"github.com/foo/baz": {
			PathIncludes: []string{"/src/foo2/*"},
			PathExcludes: []string{"/src/bar2/*"},
		},
	}

	if diff := cmp.Diff(want, have); diff != "" {
		t.Fatal(diff)
	}
}

func TestSubRepoPermsRepoSupported(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	t.Parallel()

	db := dbtest.NewDB(t)

	ctx := context.Background()
	s := SubRepoPerms(db)
	prepareSubRepoTestData(ctx, t, db)

	for _, tc := range []struct {
		repo      api.RepoName
		supported bool
	}{
		{
			repo:      "github.com/foo/bar",
			supported: false,
		},
		{
			repo:      "perforce1",
			supported: true,
		},
		{
			repo:      "unknown",
			supported: false,
		},
	} {
		t.Run(string(tc.repo), func(t *testing.T) {
			supported, err := s.RepoSupported(ctx, tc.repo)
			if err != nil {
				t.Fatal(err)
			}
			if supported != tc.supported {
				t.Fatalf("Want %v, got %v", tc.supported, supported)
			}
		})
	}
}

func TestSubRepoPermsAllSupportedRepos(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	t.Parallel()

	db := dbtest.NewDB(t)

	ctx := context.Background()
	s := SubRepoPerms(db)
	prepareSubRepoTestData(ctx, t, db)

	have, err := s.AllSupportedRepos(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []api.RepoName{"perforce1"}

	if diff := cmp.Diff(want, have); diff != "" {
		t.Fatal(diff)
	}
}

func prepareSubRepoTestData(ctx context.Context, t *testing.T, db dbutil.DB) {
	t.Helper()

	// Prepare data
	qs := []string{
		`INSERT INTO users(username) VALUES ('alice')`,

		`INSERT INTO external_services(id, display_name, kind, config, namespace_user_id, last_sync_at) VALUES(1, 'GitHub #1', 'GITHUB', '{}', 1, NOW() + INTERVAL '10min')`,
		`INSERT INTO external_services(id, display_name, kind, config, namespace_user_id, last_sync_at) VALUES(2, 'Perforce #1', 'PERFORCE', '{}', 1, NOW() + INTERVAL '10min')`,

		`INSERT INTO repo(id, name, external_id, external_service_type, external_service_id) VALUES(1, 'github.com/foo/bar', 'MDEwOlJlcG9zaXRvcnk0MTI4ODcwOA==', 'github', 'https://github.com/')`,
		`INSERT INTO repo(id, name, external_id, external_service_type, external_service_id) VALUES(2, 'github.com/foo/baz', 'MDEwOlJlcG9zaXRvcnk0MTI4ODcwOB==', 'github', 'https://github.com/')`,
		`INSERT INTO repo(id, name, external_id, external_service_type, external_service_id) VALUES(3, 'perforce1', 'MDEwOlJlcG9zaXRvcnk0MTI4ODcwOB==', 'perforce', 'https://perforce.com/')`,

		`INSERT INTO external_service_repos(repo_id, external_service_id, clone_url) VALUES(1, 1, 'cloneURL')`,
		`INSERT INTO external_service_repos(repo_id, external_service_id, clone_url) VALUES(2, 1, 'cloneURL')`,
		`INSERT INTO external_service_repos(repo_id, external_service_id, clone_url) VALUES(3, 2, 'cloneURL')`,
	}
	for _, q := range qs {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
}
