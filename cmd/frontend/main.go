package main

import (
	"github.com/sourcegraph/sourcegraph/cmd/frontend/enterprise"
	"github.com/sourcegraph/sourcegraph/cmd/frontend/shared"
	"github.com/sourcegraph/sourcegraph/internal/authz"
	"github.com/sourcegraph/sourcegraph/internal/conf/conftypes"
	"github.com/sourcegraph/sourcegraph/internal/database"
	"github.com/sourcegraph/sourcegraph/internal/oobmigration"
)

// Note: All frontend code should be added to shared.Main, not here. See that
// function for details.

func main() {
	// Set dummy authz provider to unblock channel for checking permissions in GraphQL APIs.
	// See https://github.com/sourcegraph/sourcegraph/issues/3847 for details.
	authz.SetProviders(true, []authz.Provider{})

	shared.Main(func(db database.DB, c conftypes.UnifiedWatchable, outOfBandMigrationRunner *oobmigration.Runner) enterprise.Services {
		return enterprise.DefaultServices()
	})
}
