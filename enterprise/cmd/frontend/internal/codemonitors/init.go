package codemonitors

import (
	"context"

	"github.com/sourcegraph/sourcegraph/cmd/frontend/enterprise"
	"github.com/sourcegraph/sourcegraph/enterprise/cmd/frontend/internal/codemonitors/resolvers"
	"github.com/sourcegraph/sourcegraph/internal/conf/conftypes"
	"github.com/sourcegraph/sourcegraph/internal/database/dbutil"
	"github.com/sourcegraph/sourcegraph/internal/observation"
	"github.com/sourcegraph/sourcegraph/internal/oobmigration"
)

func Init(ctx context.Context, db dbutil.DB, _ conftypes.UnifiedWatchable, outOfBandMigrationRunner *oobmigration.Runner, enterpriseServices *enterprise.Services, observationContext *observation.Context) error {
	enterpriseServices.CodeMonitorsResolver = resolvers.NewResolver(db)
	return nil
}
