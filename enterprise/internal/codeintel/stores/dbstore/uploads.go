package dbstore

import (
	"context"
	"database/sql"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/inconshreveable/log15"
	"github.com/keegancsmith/sqlf"
	"github.com/lib/pq"
	"github.com/opentracing/opentracing-go/log"

	"github.com/sourcegraph/sourcegraph/internal/database"
	"github.com/sourcegraph/sourcegraph/internal/database/basestore"
	"github.com/sourcegraph/sourcegraph/internal/observation"
	"github.com/sourcegraph/sourcegraph/internal/timeutil"
	"github.com/sourcegraph/sourcegraph/internal/workerutil"
)

// Upload is a subset of the lsif_uploads table and stores both processed and unprocessed
// records.
type Upload struct {
	ID                int        `json:"id"`
	Commit            string     `json:"commit"`
	Root              string     `json:"root"`
	VisibleAtTip      bool       `json:"visibleAtTip"`
	UploadedAt        time.Time  `json:"uploadedAt"`
	State             string     `json:"state"`
	FailureMessage    *string    `json:"failureMessage"`
	StartedAt         *time.Time `json:"startedAt"`
	FinishedAt        *time.Time `json:"finishedAt"`
	ProcessAfter      *time.Time `json:"processAfter"`
	NumResets         int        `json:"numResets"`
	NumFailures       int        `json:"numFailures"`
	RepositoryID      int        `json:"repositoryId"`
	RepositoryName    string     `json:"repositoryName"`
	Indexer           string     `json:"indexer"`
	NumParts          int        `json:"numParts"`
	UploadedParts     []int      `json:"uploadedParts"`
	UploadSize        *int64     `json:"uploadSize"`
	Rank              *int       `json:"placeInQueue"`
	AssociatedIndexID *int       `json:"associatedIndex"`
}

func (u Upload) RecordID() int {
	return u.ID
}

// scanUploads scans a slice of uploads from the return value of `*Store.query`.
func scanUploads(rows *sql.Rows, queryErr error) (_ []Upload, err error) {
	if queryErr != nil {
		return nil, queryErr
	}
	defer func() { err = basestore.CloseRows(rows, err) }()

	var uploads []Upload
	for rows.Next() {
		var upload Upload
		var rawUploadedParts []sql.NullInt32
		if err := rows.Scan(
			&upload.ID,
			&upload.Commit,
			&upload.Root,
			&upload.VisibleAtTip,
			&upload.UploadedAt,
			&upload.State,
			&upload.FailureMessage,
			&upload.StartedAt,
			&upload.FinishedAt,
			&upload.ProcessAfter,
			&upload.NumResets,
			&upload.NumFailures,
			&upload.RepositoryID,
			&upload.RepositoryName,
			&upload.Indexer,
			&upload.NumParts,
			pq.Array(&rawUploadedParts),
			&upload.UploadSize,
			&upload.AssociatedIndexID,
			&upload.Rank,
		); err != nil {
			return nil, err
		}

		uploadedParts := make([]int, 0, len(rawUploadedParts))
		for _, uploadedPart := range rawUploadedParts {
			uploadedParts = append(uploadedParts, int(uploadedPart.Int32))
		}
		upload.UploadedParts = uploadedParts

		uploads = append(uploads, upload)
	}

	return uploads, nil
}

// scanFirstUpload scans a slice of uploads from the return value of `*Store.query` and returns the first.
func scanFirstUpload(rows *sql.Rows, err error) (Upload, bool, error) {
	uploads, err := scanUploads(rows, err)
	if err != nil || len(uploads) == 0 {
		return Upload{}, false, err
	}
	return uploads[0], true, nil
}

// scanFirstUploadRecord scans a slice of uploads from the return value of `*Store.query` and returns the first.
func scanFirstUploadRecord(rows *sql.Rows, err error) (workerutil.Record, bool, error) {
	return scanFirstUpload(rows, err)
}

// scanCounts scans pairs of id/counts from the return value of `*Store.query`.
func scanCounts(rows *sql.Rows, queryErr error) (_ map[int]int, err error) {
	if queryErr != nil {
		return nil, queryErr
	}
	defer func() { err = basestore.CloseRows(rows, err) }()

	visibilities := map[int]int{}
	for rows.Next() {
		var id int
		var count int
		if err := rows.Scan(&id, &count); err != nil {
			return nil, err
		}

		visibilities[id] = count
	}

	return visibilities, nil
}

// GetUploadByID returns an upload by its identifier and boolean flag indicating its existence.
func (s *Store) GetUploadByID(ctx context.Context, id int) (_ Upload, _ bool, err error) {
	ctx, endObservation := s.operations.getUploadByID.With(ctx, &err, observation.Args{LogFields: []log.Field{
		log.Int("id", id),
	}})
	defer endObservation(1, observation.Args{})

	authzConds, err := database.AuthzQueryConds(ctx, s.Store.Handle().DB())
	if err != nil {
		return Upload{}, false, err
	}

	return scanFirstUpload(s.Store.Query(ctx, sqlf.Sprintf(getUploadByIDQuery, id, authzConds)))
}

const uploadRankQueryFragment = `
SELECT
	r.id,
	ROW_NUMBER() OVER (ORDER BY COALESCE(r.process_after, r.uploaded_at), r.id) as rank
FROM lsif_uploads_with_repository_name r
WHERE r.state = 'queued'
`

const getUploadByIDQuery = `
-- source: enterprise/internal/codeintel/stores/dbstore/uploads.go:GetUploadByID
SELECT
	u.id,
	u.commit,
	u.root,
	EXISTS (` + visibleAtTipSubselectQuery + `) AS visible_at_tip,
	u.uploaded_at,
	u.state,
	u.failure_message,
	u.started_at,
	u.finished_at,
	u.process_after,
	u.num_resets,
	u.num_failures,
	u.repository_id,
	u.repository_name,
	u.indexer,
	u.num_parts,
	u.uploaded_parts,
	u.upload_size,
	u.associated_index_id,
	s.rank
FROM lsif_uploads_with_repository_name u
LEFT JOIN (` + uploadRankQueryFragment + `) s
ON u.id = s.id
JOIN repo ON repo.id = u.repository_id
WHERE u.state != 'deleted' AND u.id = %s AND %s
`

const visibleAtTipSubselectQuery = `SELECT 1 FROM lsif_uploads_visible_at_tip uvt WHERE uvt.repository_id = u.repository_id AND uvt.upload_id = u.id`

// GetUploadsByIDs returns an upload for each of the given identifiers. Not all given ids will necessarily
// have a corresponding element in the returned list.
func (s *Store) GetUploadsByIDs(ctx context.Context, ids ...int) (_ []Upload, err error) {
	ctx, endObservation := s.operations.getUploadsByIDs.With(ctx, &err, observation.Args{LogFields: []log.Field{
		log.String("ids", intsToString(ids)),
	}})
	defer endObservation(1, observation.Args{})

	if len(ids) == 0 {
		return nil, nil
	}

	authzConds, err := database.AuthzQueryConds(ctx, s.Store.Handle().DB())
	if err != nil {
		return nil, err
	}

	queries := make([]*sqlf.Query, 0, len(ids))
	for _, id := range ids {
		queries = append(queries, sqlf.Sprintf("%d", id))
	}

	return scanUploads(s.Store.Query(ctx, sqlf.Sprintf(getUploadsByIDsQuery, sqlf.Join(queries, ", "), authzConds)))
}

const getUploadsByIDsQuery = `
-- source: enterprise/internal/codeintel/stores/dbstore/uploads.go:GetUploadsByIDs
SELECT
	u.id,
	u.commit,
	u.root,
	EXISTS (` + visibleAtTipSubselectQuery + `) AS visible_at_tip,
	u.uploaded_at,
	u.state,
	u.failure_message,
	u.started_at,
	u.finished_at,
	u.process_after,
	u.num_resets,
	u.num_failures,
	u.repository_id,
	u.repository_name,
	u.indexer,
	u.num_parts,
	u.uploaded_parts,
	u.upload_size,
	u.associated_index_id,
	s.rank
FROM lsif_uploads_with_repository_name u
LEFT JOIN (` + uploadRankQueryFragment + `) s
ON u.id = s.id
JOIN repo ON repo.id = u.repository_id
WHERE u.state != 'deleted' AND u.id IN (%s) AND %s
`

// DeleteUploadsStuckUploading soft deletes any upload record that has been uploading since the given time.
func (s *Store) DeleteUploadsStuckUploading(ctx context.Context, uploadedBefore time.Time) (_ int, err error) {
	ctx, traceLog, endObservation := s.operations.deleteUploadsStuckUploading.WithAndLogger(ctx, &err, observation.Args{LogFields: []log.Field{
		log.String("uploadedBefore", uploadedBefore.Format(time.RFC3339)), // TODO - should be a duration
	}})
	defer endObservation(1, observation.Args{})

	count, _, err := basestore.ScanFirstInt(s.Store.Query(ctx, sqlf.Sprintf(deleteUploadsStuckUploadingQuery, uploadedBefore)))
	if err != nil {
		return 0, err
	}
	traceLog(log.Int("count", count))

	return count, nil
}

const deleteUploadsStuckUploadingQuery = `
-- source: enterprise/internal/codeintel/stores/dbstore/uploads.go:DeleteUploadsStuckUploading
WITH
candidates AS (
	SELECT u.id
	FROM lsif_uploads u
	WHERE u.state = 'uploading' AND u.uploaded_at < %s

	-- Lock these rows in a deterministic order so that we don't
	-- deadlock with other processes updating the lsif_uploads table.
	ORDER BY u.id FOR UPDATE
),
deleted AS (
	UPDATE lsif_uploads u
	SET state = 'deleted'
	WHERE id IN (SELECT id FROM candidates)
	RETURNING u.repository_id
)
SELECT count(*) FROM deleted
`

type GetUploadsOptions struct {
	RepositoryID            int
	State                   string
	Term                    string
	VisibleAtTip            bool
	DependencyOf            int
	DependentOf             int
	UploadedBefore          *time.Time
	UploadedAfter           *time.Time
	LastRetentionScanBefore *time.Time
	AllowExpired            bool
	OldestFirst             bool
	Limit                   int
	Offset                  int
}

// GetUploads returns a list of uploads and the total count of records matching the given conditions.
func (s *Store) GetUploads(ctx context.Context, opts GetUploadsOptions) (_ []Upload, _ int, err error) {
	ctx, traceLog, endObservation := s.operations.getUploads.WithAndLogger(ctx, &err, observation.Args{LogFields: []log.Field{
		log.Int("repositoryID", opts.RepositoryID),
		log.String("state", opts.State),
		log.String("term", opts.Term),
		log.Bool("visibleAtTip", opts.VisibleAtTip),
		log.Int("dependencyOf", opts.DependencyOf),
		log.Int("dependentOf", opts.DependentOf),
		log.String("uploadedBefore", nilTimeToString(opts.UploadedBefore)),
		log.String("uploadedAfter", nilTimeToString(opts.UploadedAfter)),
		log.String("lastRetentionScanBefore", nilTimeToString(opts.LastRetentionScanBefore)),
		log.Bool("allowExpired", opts.AllowExpired),
		log.Bool("oldestFirst", opts.OldestFirst),
		log.Int("limit", opts.Limit),
		log.Int("offset", opts.Offset),
	}})
	defer endObservation(1, observation.Args{})

	tx, err := s.transact(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer func() { err = tx.Done(err) }()

	conds := make([]*sqlf.Query, 0, 11)
	cteDefinitions := make([]cteDefinition, 0, 2)

	if opts.RepositoryID != 0 {
		conds = append(conds, sqlf.Sprintf("u.repository_id = %s", opts.RepositoryID))
	}
	if opts.Term != "" {
		conds = append(conds, makeSearchCondition(opts.Term))
	}
	if opts.State != "" {
		conds = append(conds, makeStateCondition(opts.State))
	} else {
		conds = append(conds, sqlf.Sprintf("u.state != 'deleted'"))
	}
	if opts.VisibleAtTip {
		conds = append(conds, sqlf.Sprintf("EXISTS ("+visibleAtTipSubselectQuery+")"))
	}
	if opts.DependencyOf != 0 {
		cteDefinitions = append(cteDefinitions, cteDefinition{
			name:       "ranked_dependencies",
			definition: sqlf.Sprintf(rankedDependencyCandidateCTEQuery, sqlf.Sprintf("r.dump_id = %s", opts.DependencyOf)),
		})

		// Limit results to the set of uploads canonically providing packages referenced by the given upload identifier
		// (opts.DependencyOf). We do this by selecting the top ranked values in the CTE defined above, which are the
		// referenced package providers grouped by package name, version, repository, and root.
		conds = append(conds, sqlf.Sprintf(`u.id IN (SELECT rd.pkg_id FROM ranked_dependencies rd WHERE rd.rank = 1)`))
	}
	if opts.DependentOf != 0 {
		cteCondition := sqlf.Sprintf(`(p.scheme, p.name, p.version) IN (
			SELECT p.scheme, p.name, p.version
			FROM lsif_packages p
			WHERE p.dump_id = %s
		)`, opts.DependentOf)

		cteDefinitions = append(cteDefinitions, cteDefinition{
			name:       "ranked_dependents",
			definition: sqlf.Sprintf(rankedDependencyCandidateCTEQuery, cteCondition),
		})

		// Limit results to the set of uploads that reference the target upload if it canonically provides the
		// matching package. If the target upload does not canonically provide a package, the results will contain
		// no dependent uploads.
		conds = append(conds, sqlf.Sprintf(`u.id IN (SELECT rd.ref_id FROM ranked_dependents rd WHERE rd.pkg_id = %s AND rd.rank = 1)`, opts.DependentOf))
	}
	if opts.UploadedBefore != nil {
		conds = append(conds, sqlf.Sprintf("u.uploaded_at < %s", *opts.UploadedBefore))
	}
	if opts.UploadedAfter != nil {
		conds = append(conds, sqlf.Sprintf("u.uploaded_at > %s", *opts.UploadedAfter))
	}
	if opts.LastRetentionScanBefore != nil {
		conds = append(conds, sqlf.Sprintf("(u.last_retention_scan_at IS NULL OR u.last_retention_scan_at < %s)", *opts.LastRetentionScanBefore))
	}
	if !opts.AllowExpired {
		conds = append(conds, sqlf.Sprintf("NOT u.expired"))
	}

	authzConds, err := database.AuthzQueryConds(ctx, tx.Store.Handle().DB())
	if err != nil {
		return nil, 0, err
	}
	conds = append(conds, authzConds)

	totalCount, _, err := basestore.ScanFirstInt(tx.Store.Query(
		ctx,
		sqlf.Sprintf(
			getUploadsCountQuery,
			buildCTEPrefix(cteDefinitions),
			sqlf.Join(conds, " AND "),
		),
	))
	if err != nil {
		return nil, 0, err
	}

	var orderExpression *sqlf.Query
	if opts.OldestFirst {
		orderExpression = sqlf.Sprintf("uploaded_at")
	} else {
		orderExpression = sqlf.Sprintf("uploaded_at DESC")
	}

	uploads, err := scanUploads(tx.Store.Query(ctx, sqlf.Sprintf(
		getUploadsQuery,
		buildCTEPrefix(cteDefinitions),
		sqlf.Join(conds, " AND "),
		orderExpression,
		opts.Limit,
		opts.Offset,
	)))
	if err != nil {
		return nil, 0, err
	}
	traceLog(
		log.Int("totalCount", totalCount),
		log.Int("numUploads", len(uploads)),
	)

	return uploads, totalCount, nil
}

type cteDefinition struct {
	name       string
	definition *sqlf.Query
}

func buildCTEPrefix(cteDefinitions []cteDefinition) *sqlf.Query {
	if len(cteDefinitions) == 0 {
		return sqlf.Sprintf("")
	}

	cteQueries := make([]*sqlf.Query, 0, len(cteDefinitions))
	for _, cte := range cteDefinitions {
		cteQueries = append(cteQueries, sqlf.Sprintf("%s AS (%s)", sqlf.Sprintf(cte.name), cte.definition))
	}

	return sqlf.Sprintf("WITH\n%s", sqlf.Join(cteQueries, ",\n"))
}

const getUploadsCountQuery = `
-- source: enterprise/internal/codeintel/stores/dbstore/uploads.go:GetUploads
%s -- Dynamic CTE definitions for use in the WHERE clause
SELECT COUNT(*) FROM lsif_uploads_with_repository_name u
JOIN repo ON repo.id = u.repository_id
WHERE %s
`

const getUploadsQuery = `
-- source: enterprise/internal/codeintel/stores/dbstore/uploads.go:GetUploads
%s -- Dynamic CTE definitions for use in the WHERE clause
SELECT
	u.id,
	u.commit,
	u.root,
	EXISTS (` + visibleAtTipSubselectQuery + `) AS visible_at_tip,
	u.uploaded_at,
	u.state,
	u.failure_message,
	u.started_at,
	u.finished_at,
	u.process_after,
	u.num_resets,
	u.num_failures,
	u.repository_id,
	u.repository_name,
	u.indexer,
	u.num_parts,
	u.uploaded_parts,
	u.upload_size,
	u.associated_index_id,
	s.rank
FROM lsif_uploads_with_repository_name u
LEFT JOIN (` + uploadRankQueryFragment + `) s
ON u.id = s.id
JOIN repo ON repo.id = u.repository_id
WHERE %s ORDER BY %s LIMIT %d OFFSET %d
`

var rankedDependencyCandidateCTEQuery = `
SELECT
	p.dump_id as pkg_id,
	r.dump_id as ref_id,
	-- Rank each upload providing the same package from the same directory
	-- within a repository by commit date. We'll choose the oldest commit
	-- date as the canonical choice and ignore the uploads for younger
	-- commits providing the same package.
	` + packageRankingQueryFragment + ` AS rank
FROM lsif_uploads u
JOIN lsif_packages p ON p.dump_id = u.id
JOIN lsif_references r ON
	r.scheme = p.scheme AND
	r.name = p.name AND
	r.version = p.version AND
	r.dump_id != p.dump_id
WHERE
	-- Don't match deleted uploads
	u.state = 'completed' AND
	%s
`

// makeSearchCondition returns a disjunction of LIKE clauses against all searchable columns of an upload.
func makeSearchCondition(term string) *sqlf.Query {
	searchableColumns := []string{
		"u.commit",
		"u.root",
		"(u.state)::text",
		"u.failure_message",
		`u.repository_name`,
		"u.indexer",
	}

	var termConds []*sqlf.Query
	for _, column := range searchableColumns {
		termConds = append(termConds, sqlf.Sprintf(column+" ILIKE %s", "%"+term+"%"))
	}

	return sqlf.Sprintf("(%s)", sqlf.Join(termConds, " OR "))
}

// makeStateCondition returns a disjunction of clauses comparing the upload against the target state.
func makeStateCondition(state string) *sqlf.Query {
	states := make([]string, 0, 2)
	if state == "errored" || state == "failed" {
		// Treat errored and failed states as equivalent
		states = append(states, "errored", "failed")
	} else {
		states = append(states, state)
	}

	queries := make([]*sqlf.Query, 0, len(states))
	for _, state := range states {
		queries = append(queries, sqlf.Sprintf("u.state = %s", state))
	}

	return sqlf.Sprintf("(%s)", sqlf.Join(queries, " OR "))
}

// InsertUpload inserts a new upload and returns its identifier.
func (s *Store) InsertUpload(ctx context.Context, upload Upload) (id int, err error) {
	ctx, endObservation := s.operations.insertUpload.With(ctx, &err, observation.Args{})
	defer func() {
		endObservation(1, observation.Args{LogFields: []log.Field{
			log.Int("id", id),
		}})
	}()

	if upload.UploadedParts == nil {
		upload.UploadedParts = []int{}
	}

	id, _, err = basestore.ScanFirstInt(s.Store.Query(
		ctx,
		sqlf.Sprintf(
			insertUploadQuery,
			upload.Commit,
			upload.Root,
			upload.RepositoryID,
			upload.Indexer,
			upload.State,
			upload.NumParts,
			pq.Array(upload.UploadedParts),
			upload.UploadSize,
			upload.AssociatedIndexID,
		),
	))

	return id, err
}

const insertUploadQuery = `
-- source: enterprise/internal/codeintel/stores/dbstore/uploads.go:InsertUpload
INSERT INTO lsif_uploads (
	commit,
	root,
	repository_id,
	indexer,
	state,
	num_parts,
	uploaded_parts,
	upload_size,
	associated_index_id
) VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s)
RETURNING id
`

// AddUploadPart adds the part index to the given upload's uploaded parts array. This method is idempotent
// (the resulting array is deduplicated on update).
func (s *Store) AddUploadPart(ctx context.Context, uploadID, partIndex int) (err error) {
	ctx, endObservation := s.operations.addUploadPart.With(ctx, &err, observation.Args{LogFields: []log.Field{
		log.Int("uploadID", uploadID),
		log.Int("partIndex", partIndex),
	}})
	defer endObservation(1, observation.Args{})

	return s.Store.Exec(ctx, sqlf.Sprintf(addUploadPartQuery, partIndex, uploadID))
}

const addUploadPartQuery = `
-- source: enterprise/internal/codeintel/stores/dbstore/uploads.go:AddUploadPart
UPDATE lsif_uploads SET uploaded_parts = array(SELECT DISTINCT * FROM unnest(array_append(uploaded_parts, %s))) WHERE id = %s
`

// MarkQueued updates the state of the upload to queued and updates the upload size.
func (s *Store) MarkQueued(ctx context.Context, id int, uploadSize *int64) (err error) {
	ctx, endObservation := s.operations.markQueued.With(ctx, &err, observation.Args{LogFields: []log.Field{
		log.Int("id", id),
	}})
	defer endObservation(1, observation.Args{})

	return s.Store.Exec(ctx, sqlf.Sprintf(markQueuedQuery, uploadSize, id))
}

const markQueuedQuery = `
-- source: enterprise/internal/codeintel/stores/dbstore/uploads.go:MarkQueued
UPDATE lsif_uploads SET state = 'queued', upload_size = %s WHERE id = %s
`

// MarkFailed updates the state of the upload to failed, increments the num_failures column and sets the finished_at time
func (s *Store) MarkFailed(ctx context.Context, id int, reason string) (err error) {
	ctx, endObservation := s.operations.markFailed.With(ctx, &err, observation.Args{LogFields: []log.Field{
		log.Int("id", id),
	}})
	defer endObservation(1, observation.Args{})

	return s.Store.Exec(ctx, sqlf.Sprintf(markFailedQuery, reason, id))
}

const markFailedQuery = `
-- source: enterprise/internal/codeintel/stores/dbstore/uploads.go:MarkFailed
UPDATE
	lsif_uploads
SET
	state = 'failed',
	finished_at = clock_timestamp(),
	failure_message = %s,
	num_failures = num_failures + 1
WHERE
	id = %s
`

var uploadColumnsWithNullRank = []*sqlf.Query{
	sqlf.Sprintf("u.id"),
	sqlf.Sprintf("u.commit"),
	sqlf.Sprintf("u.root"),
	sqlf.Sprintf("EXISTS (" + visibleAtTipSubselectQuery + ") AS visible_at_tip"),
	sqlf.Sprintf("u.uploaded_at"),
	sqlf.Sprintf("u.state"),
	sqlf.Sprintf("u.failure_message"),
	sqlf.Sprintf("u.started_at"),
	sqlf.Sprintf("u.finished_at"),
	sqlf.Sprintf("u.process_after"),
	sqlf.Sprintf("u.num_resets"),
	sqlf.Sprintf("u.num_failures"),
	sqlf.Sprintf("u.repository_id"),
	sqlf.Sprintf(`u.repository_name`),
	sqlf.Sprintf("u.indexer"),
	sqlf.Sprintf("u.num_parts"),
	sqlf.Sprintf("u.uploaded_parts"),
	sqlf.Sprintf("u.upload_size"),
	sqlf.Sprintf("u.associated_index_id"),
	sqlf.Sprintf("NULL"),
}

// DeleteUploadByID deletes an upload by its identifier. This method returns a true-valued flag if a record
// was deleted. The associated repository will be marked as dirty so that its commit graph will be updated in
// the background.
func (s *Store) DeleteUploadByID(ctx context.Context, id int) (_ bool, err error) {
	ctx, endObservation := s.operations.deleteUploadByID.With(ctx, &err, observation.Args{LogFields: []log.Field{
		log.Int("id", id),
	}})
	defer endObservation(1, observation.Args{})

	tx, err := s.transact(ctx)
	if err != nil {
		return false, err
	}
	defer func() { err = tx.Done(err) }()

	repositoryID, deleted, err := basestore.ScanFirstInt(tx.Store.Query(ctx, sqlf.Sprintf(deleteUploadByIDQuery, id)))
	if err != nil {
		return false, err
	}
	if !deleted {
		return false, nil
	}

	if err := tx.MarkRepositoryAsDirty(ctx, repositoryID); err != nil {
		return false, err
	}

	return true, nil
}

const deleteUploadByIDQuery = `
-- source: enterprise/internal/codeintel/stores/dbstore/uploads.go:DeleteUploadByID
UPDATE lsif_uploads u SET state = CASE WHEN u.state = 'completed' THEN 'deleting' ELSE 'deleted' END WHERE id = %s RETURNING repository_id
`

// DeletedRepositoryGracePeriod is the minimum allowable duration between a repo deletion
// and the upload and index records for that repository being deleted.
const DeletedRepositoryGracePeriod = time.Minute * 30

// DeleteUploadsWithoutRepository deletes uploads associated with repositories that were deleted at least
// DeletedRepositoryGracePeriod ago. This returns the repository identifier mapped to the number of uploads
// that were removed for that repository.
func (s *Store) DeleteUploadsWithoutRepository(ctx context.Context, now time.Time) (_ map[int]int, err error) {
	ctx, traceLog, endObservation := s.operations.deleteUploadsWithoutRepository.WithAndLogger(ctx, &err, observation.Args{})
	defer endObservation(1, observation.Args{})

	repositories, err := scanCounts(s.Store.Query(ctx, sqlf.Sprintf(deleteUploadsWithoutRepositoryQuery, now.UTC(), DeletedRepositoryGracePeriod/time.Second)))
	if err != nil {
		return nil, err
	}

	count := 0
	for _, numDeleted := range repositories {
		count += numDeleted
	}
	traceLog(
		log.Int("count", count),
		log.Int("numRepositories", len(repositories)),
	)

	return repositories, nil
}

const deleteUploadsWithoutRepositoryQuery = `
-- source: enterprise/internal/codeintel/stores/dbstore/uploads.go:DeleteUploadsWithoutRepository
WITH
candidates AS (
	SELECT u.id
	FROM repo r
	JOIN lsif_uploads u ON u.repository_id = r.id
	WHERE %s - r.deleted_at >= %s * interval '1 second'

	-- Lock these rows in a deterministic order so that we don't
	-- deadlock with other processes updating the lsif_uploads table.
	ORDER BY u.id FOR UPDATE
),
deleted AS (
	-- Note: we can go straight from completed -> deleted here as we
	-- do not need to preserve the deleted repository's current commit
	-- graph (the API cannot resolve any queries for this repository).

	UPDATE lsif_uploads u
	SET state = 'deleted'
	WHERE u.id IN (SELECT id FROM candidates)
	RETURNING u.id, u.repository_id
)
SELECT d.repository_id, COUNT(*) FROM deleted d GROUP BY d.repository_id
`

// HardDeleteUploadByID deletes the upload record with the given identifier.
func (s *Store) HardDeleteUploadByID(ctx context.Context, ids ...int) (err error) {
	ctx, endObservation := s.operations.hardDeleteUploadByID.With(ctx, &err, observation.Args{LogFields: []log.Field{
		log.Int("numIDs", len(ids)),
		log.String("ids", intsToString(ids)),
	}})
	defer endObservation(1, observation.Args{})

	if len(ids) == 0 {
		return nil
	}

	var idQueries []*sqlf.Query
	for _, id := range ids {
		idQueries = append(idQueries, sqlf.Sprintf("%s", id))
	}

	tx, err := s.Transact(ctx)
	if err != nil {
		return err
	}
	defer func() { err = tx.Done(err) }()

	// Before deleting the record, ensure that we decrease the number of existant references
	// to all of this upload's dependencies. This also selects a new upload to canonically provide
	// the same package as the deleted upload, if such an upload exists.
	if err := tx.UpdateReferenceCounts(ctx, ids, DependencyReferenceCountUpdateTypeRemove); err != nil {
		return err
	}

	if err := tx.Exec(ctx, sqlf.Sprintf(hardDeleteUploadByIDQuery, sqlf.Join(idQueries, ", "))); err != nil {
		return err
	}

	return nil
}

const hardDeleteUploadByIDQuery = `
-- source: enterprise/internal/codeintel/stores/dbstore/uploads.go:HardDeleteUploadByID
WITH locked_uploads AS (
	SELECT u.id
	FROM lsif_uploads u
	WHERE u.id IN (%s)
	ORDER BY u.id FOR UPDATE
)
DELETE FROM lsif_uploads WHERE id IN (SELECT id FROM locked_uploads)
`

// SelectRepositoriesForIndexScan returns a set of repository identifiers that should be considered
// for indexing jobs. Repositories that were returned previously from this call within the  given
// process delay are not returned.
func (s *Store) SelectRepositoriesForIndexScan(ctx context.Context, processDelay time.Duration, limit int) (_ []int, err error) {
	return s.selectRepositoriesForIndexScan(ctx, processDelay, limit, timeutil.Now())
}

func (s *Store) selectRepositoriesForIndexScan(ctx context.Context, processDelay time.Duration, limit int, now time.Time) (_ []int, err error) {
	ctx, endObservation := s.operations.selectRepositoriesForIndexScan.With(ctx, &err, observation.Args{})
	defer endObservation(1, observation.Args{})

	return basestore.ScanInts(s.Query(ctx, sqlf.Sprintf(
		selectRepositoriesForIndexScanQuery,
		now,
		int(processDelay/time.Second),
		limit,
		now,
		now,
	)))
}

const selectRepositoriesForIndexScanQuery = `
-- source: enterprise/internal/codeintel/stores/dbstore/uploads.go:selectRepositoriesForIndexScan
WITH
search_context_repositories AS (
	SELECT repo_id FROM user_public_repos UNION ALL
	SELECT repo_id FROM search_context_repos UNION ALL
	SELECT repo_id FROM external_service_repos WHERE user_id IS NOT NULL
),
candidate_repositories AS (
	SELECT r.id AS id
	FROM repo r
	WHERE
		r.deleted_at IS NULL AND
		r.blocked IS NULL AND
		r.id IN (SELECT repo_id FROM search_context_repositories)
),
repositories AS (
	SELECT cr.id
	FROM candidate_repositories cr
	LEFT JOIN lsif_last_index_scan lrs ON lrs.repository_id = cr.id

	-- Ignore records that have been checked recently. Note this condition is
	-- true for a null last_index_scan_at (which has never been checked).
	WHERE (%s - lrs.last_index_scan_at > (%s * '1 second'::interval)) IS DISTINCT FROM FALSE
	ORDER BY
		lrs.last_index_scan_at NULLS FIRST,
		cr.id -- tie breaker
	LIMIT %s
)
INSERT INTO lsif_last_index_scan (repository_id, last_index_scan_at)
SELECT r.id, %s::timestamp FROM repositories r
ON CONFLICT (repository_id) DO UPDATE
SET last_index_scan_at = %s
RETURNING repository_id
`

// SelectRepositoriesForRetentionScan returns a set of repository identifiers with live code intelligence
// data and a fresh associated commit graph. Repositories that were returned previously from this call
// within the  given process delay are not returned.
func (s *Store) SelectRepositoriesForRetentionScan(ctx context.Context, processDelay time.Duration, limit int) (_ []int, err error) {
	return s.selectRepositoriesForRetentionScan(ctx, processDelay, limit, timeutil.Now())
}

func (s *Store) selectRepositoriesForRetentionScan(ctx context.Context, processDelay time.Duration, limit int, now time.Time) (_ []int, err error) {
	ctx, endObservation := s.operations.selectRepositoriesForRetentionScan.With(ctx, &err, observation.Args{})
	defer endObservation(1, observation.Args{})

	return basestore.ScanInts(s.Query(ctx, sqlf.Sprintf(
		repositoryIDsForRetentionScanQuery,
		now,
		int(processDelay/time.Second),
		limit,
		now,
		now,
	)))
}

const repositoryIDsForRetentionScanQuery = `
-- source: enterprise/internal/codeintel/stores/dbstore/uploads.go:selectRepositoriesForRetentionScan
WITH candidate_repositories AS (
	SELECT DISTINCT u.repository_id AS id
	FROM lsif_uploads u
	WHERE u.state = 'completed'
),
repositories AS (
	SELECT cr.id
	FROM candidate_repositories cr
	LEFT JOIN lsif_last_retention_scan lrs ON lrs.repository_id = cr.id
	JOIN lsif_dirty_repositories dr ON dr.repository_id = cr.id

	-- Ignore records that have been checked recently. Note this condition is
	-- true for a null last_retention_scan_at (which has never been checked).
	WHERE (%s - lrs.last_retention_scan_at > (%s * '1 second'::interval)) IS DISTINCT FROM FALSE
	AND dr.update_token = dr.dirty_token
	ORDER BY
		lrs.last_retention_scan_at NULLS FIRST,
		cr.id -- tie breaker
	LIMIT %s
)
INSERT INTO lsif_last_retention_scan (repository_id, last_retention_scan_at)
SELECT r.id, %s::timestamp FROM repositories r
ON CONFLICT (repository_id) DO UPDATE
SET last_retention_scan_at = %s
RETURNING repository_id
`

// UpdateUploadRetention updates the last data retention scan timestamp on the upload
// records with the given protected identifiers and sets the expired field on the upload
// records with the given expired identifiers.
func (s *Store) UpdateUploadRetention(ctx context.Context, protectedIDs, expiredIDs []int) error {
	return s.updateUploadRetention(ctx, protectedIDs, expiredIDs, time.Now())
}

func (s *Store) updateUploadRetention(ctx context.Context, protectedIDs, expiredIDs []int, now time.Time) (err error) {
	ctx, endObservation := s.operations.updateUploadRetention.With(ctx, &err, observation.Args{LogFields: []log.Field{
		log.Int("numProtectedIDs", len(protectedIDs)),
		log.String("protectedIDs", intsToString(protectedIDs)),
		log.Int("numExpiredIDs", len(expiredIDs)),
		log.String("expiredIDs", intsToString(expiredIDs)),
	}})
	defer endObservation(1, observation.Args{})

	// Ensure ids are sorted so that we take row locks during the UPDATE
	// query in a determinstic order. This should prevent deadlocks with
	// other queries that mass update lsif_uploads.
	sort.Ints(protectedIDs)
	sort.Ints(expiredIDs)

	tx, err := s.transact(ctx)
	if err != nil {
		return err
	}
	defer func() { err = tx.Done(err) }()

	if len(protectedIDs) > 0 {
		queries := make([]*sqlf.Query, 0, len(protectedIDs))
		for _, id := range protectedIDs {
			queries = append(queries, sqlf.Sprintf("%s", id))
		}

		if err := tx.Exec(ctx, sqlf.Sprintf(updateUploadRetentionQuery, sqlf.Sprintf("last_retention_scan_at = %s", now), sqlf.Join(queries, ","))); err != nil {
			return err
		}
	}

	if len(expiredIDs) > 0 {
		queries := make([]*sqlf.Query, 0, len(expiredIDs))
		for _, id := range expiredIDs {
			queries = append(queries, sqlf.Sprintf("%s", id))
		}

		if err := tx.Exec(ctx, sqlf.Sprintf(updateUploadRetentionQuery, sqlf.Sprintf("expired = TRUE"), sqlf.Join(queries, ","))); err != nil {
			return err
		}
	}

	return nil
}

const updateUploadRetentionQuery = `
-- source: enterprise/internal/codeintel/stores/dbstore/uploads.go:UpdateUploadRetention
UPDATE lsif_uploads SET %s WHERE id IN (%s)
`

type DependencyReferenceCountUpdateType int

const (
	DependencyReferenceCountUpdateTypeNone DependencyReferenceCountUpdateType = iota
	DependencyReferenceCountUpdateTypeAdd
	DependencyReferenceCountUpdateTypeRemove
)

var deltaMap = map[DependencyReferenceCountUpdateType]int{
	DependencyReferenceCountUpdateTypeNone:   +0,
	DependencyReferenceCountUpdateTypeAdd:    +1,
	DependencyReferenceCountUpdateTypeRemove: -1,
}

// UpdateReferenceCounts updates the reference counts of uploads indicated by the given identifiers
// as well as the set of uploads that would be affected by one of the upload's insertion or removal.
// The behavior of this method is determined by the dependencyUpdateType value.
//
//   - Use DependencyReferenceCountUpdateTypeNone to calculate the reference count of each of the given
//     uploads without considering dependency upload counts.
//   - Use DependencyReferenceCountUpdateTypeAdd to calculate the reference count of each of the given
//     uploads while adding one to each direct dependency's reference count.
//   - Use DependencyReferenceCountUpdateTypeRemove to calculate the reference count of each of the given
//     uploads while removing one from each direct dependency's reference count.
//
// To keep reference counts consistent, this method should be called directly after insertion and directly
// before deletion of each upload record.
func (s *Store) UpdateReferenceCounts(ctx context.Context, ids []int, dependencyUpdateType DependencyReferenceCountUpdateType) (err error) {
	ctx, endObservation := s.operations.updateReferenceCounts.With(ctx, &err, observation.Args{LogFields: []log.Field{
		log.Int("numIDs", len(ids)),
		log.String("ids", intsToString(ids)),
		log.Int("dependencyUpdateType", int(dependencyUpdateType)),
	}})
	defer endObservation(1, observation.Args{})

	if len(ids) == 0 {
		return nil
	}

	// Just in case
	if os.Getenv("DEBUG_PRECISE_CODE_INTEL_REFERENCE_COUNTS_BAIL_OUT") != "" {
		log15.Warn("Reference count operations are currently disabled")
		return nil
	}

	tx, err := s.transact(ctx)
	if err != nil {
		return err
	}
	defer func() { err = tx.Done(err) }()

	idArray := pq.Array(ids)

	excludeCondition := sqlf.Sprintf("TRUE")
	if dependencyUpdateType == DependencyReferenceCountUpdateTypeRemove {
		excludeCondition = sqlf.Sprintf("NOT (u.id = ANY (%s))", idArray)
	}

	if err := tx.Exec(ctx, sqlf.Sprintf(
		updateReferenceCountsQuery,
		idArray,
		idArray,
		excludeCondition,
		idArray,
		deltaMap[dependencyUpdateType],
	)); err != nil {
		return err
	}

	return nil
}

var updateReferenceCountsQuery = `
-- source: enterprise/internal/codeintel/stores/dbstore/uploads.go:UpdateReferenceCounts
WITH
-- Select the set of package identifiers provided by the target upload list. This
-- result set includes non-canonical results.
packages_defined_by_target_uploads AS (
	SELECT p.scheme, p.name, p.version
	FROM lsif_packages p
	WHERE p.dump_id = ANY (%s)
),

-- Select the ranked set of uploads that provide a package that is also provided
-- by the target upload list. This over-selects the set of uploads that visibly
-- provide a package so that we can re-rank the canonical uploads for a package
-- on the fly.
ranked_uploads_providing_packages AS (
	SELECT
		u.id,
		p.scheme,
		p.name,
		p.version,
		-- Rank each upload providing the same package from the same directory
		-- within a repository by commit date. We'll choose the oldest commit
		-- date as the canonical choice, and set the reference counts to all
		-- of the duplicate commits to zero.
		` + packageRankingQueryFragment + ` AS rank
	FROM lsif_uploads u
	LEFT JOIN lsif_packages p ON p.dump_id = u.id
	WHERE
		(
			-- Select our target uploads
			u.id = ANY (%s) OR

			-- Also select uploads that provide the same package as a target upload.
			--
			-- It is necessary to select these extra records as the insertion or
			-- deletion of an upload record can change the rank of uploads/packages.
			-- We need to ensure that we update the reference counts of every upload
			-- in this set, not just the ones that were recently inserted or deleted.
			(p.scheme, p.name, p.version) IN (
				SELECT p.scheme, p.name, p.version
				FROM packages_defined_by_target_uploads p
			)
		) AND

		-- Don't match deleted uploads. We may be dealing with uploads still in the
		-- processing state, though, so we allow those here.
		u.state NOT IN ('deleted', 'deleting') AND

		-- If we are deleting uploads that provide intelligence for a package, we need
		-- to ensure that we calculate the correct dependencies as if the records have
		-- been deleted. This condition throws out exact target uploads while keeping
		-- the (newly adjusted) ranked set of uploads providing the same package.
		(%s)
),

-- Calculate the number of references to each upload represented by the CTE
-- ranked_uploads_providing_packages. Those that are not the canonical upload
-- providing their package will have ref count of zero, by having no associated
-- row in this intermediate result set. The canonical uploads will have their
-- reference count re-calculated based on the current set of dependencies known
-- to Postgres.
canonical_package_reference_counts AS (
	SELECT
		ru.id,
		count(*) AS count
	FROM ranked_uploads_providing_packages ru
	JOIN lsif_references r
	ON
		r.scheme = ru.scheme AND
		r.name = ru.name AND
		r.version = ru.version AND
		r.dump_id != ru.id
	WHERE ru.rank = 1
	GROUP BY ru.id
),

-- Count (and ranks) the set of edges that cross over from the target list of uploads
-- to existing uploads that provide a dependent package. This is the modifier by which
-- dependency reference counts must be altered in order for existing package reference
-- counts to remain up-to-date.
dependency_reference_counts AS (
	SELECT
		u.id,
		` + packageRankingQueryFragment + ` AS rank,
		count(*) AS count
	FROM lsif_uploads u
	JOIN lsif_packages p ON p.dump_id = u.id
	JOIN lsif_references r
	ON
		r.scheme = p.scheme AND
		r.name = p.name AND
		r.version = p.version AND
		r.dump_id != p.dump_id
	WHERE
		-- Here we want the set of actually reachable uploads
		u.state = 'completed' AND
		r.dump_id = ANY (%s)
	GROUP BY u.id, p.scheme, p.name, p.version
),

-- Discard dependency edges to non-canonical uploads. Sum the remaining edge counts
-- to find the amount by which we need to update the reference count for the remaining
-- dependent uploads.
canonical_dependency_reference_counts AS (
	SELECT rc.id, SUM(rc.count) AS count
	FROM dependency_reference_counts rc
	WHERE rc.rank = 1
	GROUP BY rc.id
),

-- Determine the set of reference count values to write to the lsif_uploads table, then
-- lock all of the affected rows in a deterministic order. This should prevent hitting
-- deadlock conditions when multiple bulk operations are happening over intersecting
-- rows of the same table.
locked_uploads AS (
	SELECT
		u.id,

		-- If ru.id IS NOT NULL, then we have recalculated the reference count for this
		-- row in the CTE canonical_package_reference_counts. Use this value. Otherwise,
		-- this row is a dependency of the target upload list and we only be incrementally
		-- modifying the row's reference count.
		--
		CASE WHEN ru.id IS NOT NULL THEN COALESCE(pkg_refcount.count, 0) ELSE u.reference_count END +

		-- If ru.id IN canonical_dependency_reference_counts, then we incrementally modify
		-- the row's reference count proportional the number of additional dependent edges
		-- counted in the CTE. The placeholder here is an integer in the range [-1, 1] used
		-- to specify if we are adding or removing a set of upload records.
		COALESCE(dep_refcount.count, 0) * %s AS reference_count
	FROM lsif_uploads u
	LEFT JOIN ranked_uploads_providing_packages ru ON ru.id = u.id
	LEFT JOIN canonical_package_reference_counts pkg_refcount ON pkg_refcount.id = u.id
	LEFT JOIN canonical_dependency_reference_counts dep_refcount ON dep_refcount.id = u.id
	-- Prevent creating no-op updates for every row in the table
	WHERE ru.id IS NOT NULL OR dep_refcount.id IS NOT NULL
	ORDER BY u.id FOR UPDATE
)

-- Perform deterministically ordered update
UPDATE lsif_uploads u
SET reference_count = lu.reference_count
FROM locked_uploads lu WHERE lu.id = u.id
`

// SoftDeleteExpiredUploads marks upload records that are both expired and have no references
// as deleted. The associated repositories will be marked as dirty so that their commit graphs
// are updated in the near future.
func (s *Store) SoftDeleteExpiredUploads(ctx context.Context) (count int, err error) {
	ctx, traceLog, endObservation := s.operations.softDeleteExpiredUploads.WithAndLogger(ctx, &err, observation.Args{})
	defer endObservation(1, observation.Args{})

	tx, err := s.transact(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { err = tx.Done(err) }()

	// Just in case
	if os.Getenv("DEBUG_PRECISE_CODE_INTEL_REFERENCE_COUNTS_BAIL_OUT") != "" {
		log15.Warn("Reference count operations are currently disabled")
		return 0, nil
	}

	repositories, err := scanCounts(tx.Store.Query(ctx, sqlf.Sprintf(softDeleteExpiredUploadsQuery)))
	if err != nil {
		return 0, err
	}

	for _, numUpdated := range repositories {
		count += numUpdated
	}
	traceLog(
		log.Int("count", count),
		log.Int("numRepositories", len(repositories)),
	)

	for repositoryID := range repositories {
		if err := tx.MarkRepositoryAsDirty(ctx, repositoryID); err != nil {
			return 0, err
		}
	}

	return count, nil
}

const softDeleteExpiredUploadsQuery = `
-- source: enterprise/internal/codeintel/stores/dbstore/uploads.go:SoftDeleteExpiredUploads
WITH candidates AS (
	SELECT u.id
	FROM lsif_uploads u
	WHERE u.state = 'completed' AND u.expired AND u.reference_count = 0
	-- Lock these rows in a deterministic order so that we don't
	-- deadlock with other processes updating the lsif_uploads table.
	ORDER BY u.id FOR UPDATE
),
updated AS (
	UPDATE lsif_uploads u
	SET state = 'deleting'
	WHERE u.id IN (SELECT id FROM candidates)
	RETURNING u.id, u.repository_id
)
SELECT u.repository_id, count(*) FROM updated u GROUP BY u.repository_id
`

// GetOldestCommitDate returns the oldest commit date for all uploads for the given repository. If there are no
// non-nil values, a false-valued flag is returned.
func (s *Store) GetOldestCommitDate(ctx context.Context, repositoryID int) (_ time.Time, _ bool, err error) {
	ctx, _, endObservation := s.operations.getOldestCommitDate.WithAndLogger(ctx, &err, observation.Args{LogFields: []log.Field{
		log.Int("repositoryID", repositoryID),
	}})
	defer endObservation(1, observation.Args{})

	return basestore.ScanFirstTime(s.Query(ctx, sqlf.Sprintf(getOldestCommitDateQuery, repositoryID)))
}

// Note: we check against '-infinity' here, as the backfill operation will use this sentinel value in the case
// that the commit is no longer know by gitserver. This allows the backfill migration to make progress without
// having pristine database.
const getOldestCommitDateQuery = `
-- source: enterprise/internal/codeintel/stores/dbstore/uploads.go:GetOldestCommitDate
SELECT committed_at FROM lsif_uploads WHERE repository_id = %s AND state = 'completed' AND committed_at IS NOT NULL AND committed_at != '-infinity' ORDER BY committed_at LIMIT 1
`

// UpdateCommitedAt updates the commit date for the given repository.
func (s *Store) UpdateCommitedAt(ctx context.Context, uploadID int, committedAt time.Time) (err error) {
	ctx, _, endObservation := s.operations.updateCommitedAt.WithAndLogger(ctx, &err, observation.Args{LogFields: []log.Field{
		log.Int("uploadID", uploadID),
	}})
	defer endObservation(1, observation.Args{})

	return s.Exec(ctx, sqlf.Sprintf(updateCommitedAtQuery, committedAt, uploadID))
}

const updateCommitedAtQuery = `
-- source: enterprise/internal/codeintel/stores/dbstore/uploads.go:UpdateCommitedAt
UPDATE lsif_uploads SET committed_at = %s WHERE id = %s
`

func intsToString(vs []int) string {
	strs := make([]string, 0, len(vs))
	for _, v := range vs {
		strs = append(strs, strconv.Itoa(v))
	}

	return strings.Join(strs, ", ")
}

func nilTimeToString(t *time.Time) string {
	if t == nil {
		return ""
	}

	return t.String()
}
