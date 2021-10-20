package lsifstore

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/keegancsmith/sqlf"
	"github.com/lib/pq"
	"github.com/opentracing/opentracing-go/log"

	"github.com/sourcegraph/sourcegraph/internal/database/basestore"
	"github.com/sourcegraph/sourcegraph/internal/observation"
	"github.com/sourcegraph/sourcegraph/lib/codeintel/precise"
)

// DocumentationPage returns the documentation page with the given PathID.
func (s *Store) DocumentationPage(ctx context.Context, bundleID int, pathID string) (_ *precise.DocumentationPageData, err error) {
	ctx, _, endObservation := s.operations.documentationPage.WithAndLogger(ctx, &err, observation.Args{LogFields: []log.Field{
		log.Int("bundleID", bundleID),
		log.String("pathID", pathID),
	}})
	defer endObservation(1, observation.Args{})

	page, err := s.scanFirstDocumentationPageData(s.Store.Query(ctx, sqlf.Sprintf(documentationPageDataQuery, bundleID, pathID)))
	if err != nil {
		return nil, err
	}
	return page, nil
}

const documentationPageDataQuery = `
-- source: enterprise/internal/codeintel/stores/lsifstore/documentation.go:DocumentationPage
SELECT
	dump_id,
	path_id,
	data
FROM
	lsif_data_documentation_pages
WHERE
	dump_id = %s AND
	path_id = %s
`

// scanFirstDocumentationPageData reads the first DocumentationPageData row. If no rows match the
// query, a nil is returned.
func (s *Store) scanFirstDocumentationPageData(rows *sql.Rows, queryErr error) (_ *precise.DocumentationPageData, err error) {
	if queryErr != nil {
		return nil, queryErr
	}
	defer func() { err = basestore.CloseRows(rows, err) }()

	if !rows.Next() {
		return nil, nil
	}

	var (
		rawData  []byte
		uploadID int
		pathID   string
	)
	if err := rows.Scan(
		&uploadID,
		&pathID,
		&rawData,
	); err != nil {
		return nil, err
	}
	record, err := s.serializer.UnmarshalDocumentationPageData(rawData)
	if err != nil {
		return nil, err
	}
	return record, nil
}

// DocumentationPathInfo returns info describing what is at the given pathID.
func (s *Store) DocumentationPathInfo(ctx context.Context, bundleID int, pathID string) (_ *precise.DocumentationPathInfoData, err error) {
	ctx, _, endObservation := s.operations.documentationPathInfo.WithAndLogger(ctx, &err, observation.Args{LogFields: []log.Field{
		log.Int("bundleID", bundleID),
		log.String("pathID", pathID),
	}})
	defer endObservation(1, observation.Args{})

	page, err := s.scanFirstDocumentationPathInfoData(s.Store.Query(ctx, sqlf.Sprintf(documentationPathInfoDataQuery, bundleID, pathID)))
	if err != nil {
		return nil, err
	}
	return page, nil
}

const documentationPathInfoDataQuery = `
-- source: enterprise/internal/codeintel/stores/lsifstore/documentation.go:DocumentationPathInfo
SELECT
	dump_id,
	path_id,
	data
FROM
	lsif_data_documentation_path_info
WHERE
	dump_id = %s AND
	path_id = %s
`

// scanFirstDocumentationPathInfoData reads the first DocumentationPathInfoData row. If no rows match the
// query, a nil is returned.
func (s *Store) scanFirstDocumentationPathInfoData(rows *sql.Rows, queryErr error) (_ *precise.DocumentationPathInfoData, err error) {
	if queryErr != nil {
		return nil, queryErr
	}
	defer func() { err = basestore.CloseRows(rows, err) }()

	if !rows.Next() {
		return nil, nil
	}

	var (
		rawData  []byte
		uploadID int
		pathID   string
	)
	if err := rows.Scan(
		&uploadID,
		&pathID,
		&rawData,
	); err != nil {
		return nil, err
	}
	record, err := s.serializer.UnmarshalDocumentationPathInfoData(rawData)
	if err != nil {
		return nil, err
	}
	return record, nil
}

// documentationIDsToPathIDs returns a mapping of the given documentationResult IDs to their
// associative path IDs. Empty result IDs ("") are ignored.
func (s *Store) documentationIDsToPathIDs(ctx context.Context, bundleID int, ids []precise.ID) (_ map[precise.ID]string, err error) {
	ctx, _, endObservation := s.operations.documentationIDsToPathIDs.WithAndLogger(ctx, &err, observation.Args{LogFields: []log.Field{
		log.Int("bundleID", bundleID),
		log.String("ids", fmt.Sprint(ids)),
	}})
	defer endObservation(1, observation.Args{})

	var wantIDs []uint64
	for _, id := range ids {
		if id != "" {
			idInt, err := strconv.ParseUint(string(id), 10, 64)
			if err != nil {
				return nil, err
			}
			wantIDs = append(wantIDs, idInt)
		}
	}
	if len(wantIDs) == 0 {
		return nil, nil
	}

	pathIDs, err := s.scanDocumentationPathIDs(s.Store.Query(ctx, sqlf.Sprintf(documentationIDsToPathIDsQuery, bundleID, pq.Array(wantIDs))))
	if err != nil {
		return nil, err
	}
	return pathIDs, nil
}

// scanDocumentationPathIDs reads documentation path IDs from the given row object.
func (s *Store) scanDocumentationPathIDs(rows *sql.Rows, queryErr error) (_ map[precise.ID]string, err error) {
	if queryErr != nil {
		return nil, queryErr
	}
	defer func() { err = basestore.CloseRows(rows, err) }()

	values := map[precise.ID]string{}
	for rows.Next() {
		var (
			resultID uint64
			pathID   string
		)
		if err := rows.Scan(&resultID, &pathID); err != nil {
			return nil, err
		}
		values[precise.ID(strconv.FormatUint(resultID, 10))] = pathID
	}
	return values, nil
}

const documentationIDsToPathIDsQuery = `
-- source: enterprise/internal/codeintel/stores/lsifstore/ranges.go:documentationIDsToPathIDs
SELECT
	result_id,
	path_id
FROM
	lsif_data_documentation_mappings
WHERE
	dump_id = %s AND
	result_id = ANY (%s)
`

func (s *Store) documentationPathIDToID(ctx context.Context, bundleID int, pathID string) (_ precise.ID, err error) {
	ctx, _, endObservation := s.operations.documentationPathIDToID.WithAndLogger(ctx, &err, observation.Args{LogFields: []log.Field{
		log.Int("bundleID", bundleID),
		log.String("pathID", pathID),
	}})
	defer endObservation(1, observation.Args{})

	resultID, err := s.scanFirstDocumentationResultID(s.Store.Query(ctx, sqlf.Sprintf(documentationPathIDToIDQuery, bundleID, pathID)))
	if err != nil {
		return "", err
	}
	if resultID == -1 {
		return "", err
	}
	return precise.ID(strconv.FormatInt(resultID, 10)), nil
}

const documentationPathIDToIDQuery = `
-- source: enterprise/internal/codeintel/stores/lsifstore/ranges.go:documentationPathIDToID
SELECT
	result_id
FROM
	lsif_data_documentation_mappings
WHERE
	dump_id = %s AND
	path_id = %s
LIMIT 1
`

// scanFirstDocumentationResultID reads the first result_id row. If no rows match the query, an empty string is returned.
func (s *Store) scanFirstDocumentationResultID(rows *sql.Rows, queryErr error) (_ int64, err error) {
	if queryErr != nil {
		return -1, queryErr
	}
	defer func() { err = basestore.CloseRows(rows, err) }()

	if !rows.Next() {
		return -1, nil
	}

	var resultID int64
	if err := rows.Scan(&resultID); err != nil {
		return -1, err
	}
	return resultID, nil
}

// documentationPathIDToFilePath queries the file path associated with a documentation path ID,
// e.g. the file where the documented symbol is located - if the path ID is describing such a
// symbol, or nil otherwise.
func (s *Store) documentationPathIDToFilePath(ctx context.Context, bundleID int, pathID string) (_ *string, err error) {
	ctx, _, endObservation := s.operations.documentationPathIDToFilePath.WithAndLogger(ctx, &err, observation.Args{LogFields: []log.Field{
		log.Int("bundleID", bundleID),
		log.String("pathID", pathID),
	}})
	defer endObservation(1, observation.Args{})

	return s.scanFirstDocumentationFilePath(s.Store.Query(ctx, sqlf.Sprintf(documentationPathIDToFilePathQuery, bundleID, pathID)))
}

const documentationPathIDToFilePathQuery = `
-- source: enterprise/internal/codeintel/stores/lsifstore/ranges.go:documentationPathIDToFilePath
SELECT
	file_path
FROM
	lsif_data_documentation_mappings
WHERE
	dump_id = %s AND
	path_id = %s
LIMIT 1
`

// scanFirstDocumentationFilePath reads the first file_path row. If no rows match the query, an empty string is returned.
func (s *Store) scanFirstDocumentationFilePath(rows *sql.Rows, queryErr error) (_ *string, err error) {
	if queryErr != nil {
		return nil, queryErr
	}
	defer func() { err = basestore.CloseRows(rows, err) }()

	if !rows.Next() {
		return nil, nil
	}

	var filePath *string
	if err := rows.Scan(&filePath); err != nil {
		return nil, err
	}
	return filePath, nil
}

// DocumentationDefinitions returns the set of locations defining the symbol found at the given path ID, if any.
func (s *Store) DocumentationDefinitions(ctx context.Context, bundleID int, pathID string, limit, offset int) (_ []Location, _ int, err error) {
	resultID, err := s.documentationPathIDToID(ctx, bundleID, pathID)
	if err != nil || resultID == "" {
		return nil, 0, err
	}
	extractor := func(r precise.RangeData) precise.ID { return r.DefinitionResultID }
	operation := s.operations.documentationDefinitions
	return s.documentationDefinitions(ctx, extractor, operation, bundleID, pathID, resultID, limit, offset)
}

func (s *Store) documentationDefinitions(
	ctx context.Context,
	extractor func(r precise.RangeData) precise.ID,
	operation *observation.Operation,
	bundleID int,
	pathID string,
	resultID precise.ID,
	limit,
	offset int,
) (_ []Location, _ int, err error) {
	ctx, traceLog, endObservation := operation.WithAndLogger(ctx, &err, observation.Args{LogFields: []log.Field{
		log.Int("bundleID", bundleID),
		log.String("resultID", string(resultID)),
	}})
	defer endObservation(1, observation.Args{})

	filePath, err := s.documentationPathIDToFilePath(ctx, bundleID, pathID)
	if err != nil {
		return nil, 0, errors.Wrap(err, "documentationPathIDToFilePath")
	}
	if filePath == nil {
		// The documentation result is not attached to a file, it cannot have references.
		return nil, 0, nil
	}

	documentData, exists, err := s.scanFirstDocumentData(s.Store.Query(ctx, sqlf.Sprintf(locationsDocumentQuery, bundleID, filePath)))
	if err != nil || !exists {
		return nil, 0, err
	}

	traceLog(log.Int("numRanges", len(documentData.Document.Ranges)))
	var found *precise.RangeData
	for _, rn := range documentData.Document.Ranges {
		if rn.DocumentationResultID == resultID {
			found = &rn
			break
		}
	}
	traceLog(log.Bool("found", found == nil))
	if found == nil {
		return nil, 0, errors.New("not found")
	}

	orderedResultIDs := extractResultIDs([]precise.RangeData{*found}, extractor)
	locationsMap, totalCount, err := s.locations(ctx, bundleID, orderedResultIDs, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	traceLog(log.Int("totalCount", totalCount))

	locations := make([]Location, 0, limit)
	for _, resultID := range orderedResultIDs {
		locations = append(locations, locationsMap[resultID]...)
	}

	return locations, totalCount, nil
}

// DocumentationSearch searches API documentation in either the "public" or "private" table.
//
// 🚨 SECURITY: If the input tableSuffix is "private", then it is the callers responsibility to
// enforce that the returned results are from repositories the user has permission to.
func (s *Store) DocumentationSearch(ctx context.Context, tableSuffix, query string, repos []string) (_ []precise.DocumentationSearchResult, err error) {
	ctx, _, endObservation := s.operations.documentationSearch.WithAndLogger(ctx, &err, observation.Args{LogFields: []log.Field{
		log.String("query", query),
		log.String("repos", fmt.Sprint(repos)),
	}})
	defer endObservation(1, observation.Args{})

	// First, we split the query at the first colon if there is one. We support two syntaxes:
	//
	// 	<metadata>:<query terms>
	// 	<query terms and metadata>
	//
	// That is, if the user jumbling all the query terms and metadata (repo names, tags, language name, etc.)
	// into the query does not yield good results, they can prefix their query with metadata and a colon, for
	// queries like:
	//
	// 	golang/go: net/http
	// 	go package: mux
	// 	go private variable my/repo: mux
	//
	// This is a stupid-simple form for people to get more specific results.
	metaQuery := query
	mainQuery := query
	if i := strings.Index(query, ":"); i != -1 {
		metaQuery = strings.TrimSpace(query[:i])
		mainQuery = strings.TrimSpace(query[i+len(":"):])
	}

	// whether or not lexemes should match substrings. e.g. should "gith.com/sourcegraph/source"
	// be matched as "*gith*.*com*/*sourcegraph*/*source*".
	subStringMatches := true
	langRepoTagsClause := textSearchQuery("tsv", metaQuery, subStringMatches)

	var primaryClauses []*sqlf.Query
	primaryClauses = append(primaryClauses, textSearchQuery("search_key_tsv", mainQuery, subStringMatches))
	primaryClauses = append(primaryClauses, textSearchQuery("search_key_reverse_tsv", reverse(mainQuery), subStringMatches))
	primaryClauses = append(primaryClauses, textSearchQuery("label_tsv", mainQuery, subStringMatches))
	primaryClauses = append(primaryClauses, textSearchQuery("label_reverse_tsv", reverse(mainQuery), subStringMatches))

	return s.scanDocumentationSearchResults(s.Store.Query(ctx, sqlf.Sprintf(
		strings.ReplaceAll(documentationSearchQuery, "$SUFFIX", tableSuffix),
		langRepoTagsClause, // matching_lang_names CTE WHERE conditions
		langRepoTagsClause, // matching_repo_names CTE WHERE conditions
		langRepoTagsClause, // matching_tags CTE WHERE conditions

		textSearchRank("search_key_tsv", mainQuery, subStringMatches),                  // search_key_rank
		textSearchRank("search_key_reverse_tsv", reverse(mainQuery), subStringMatches), // search_key_reverse_rank
		textSearchRank("label_tsv", mainQuery, subStringMatches),                       // label_rank
		textSearchRank("label_reverse_tsv", reverse(mainQuery), subStringMatches),      // label_reverse_rank

		sqlf.Join(primaryClauses, " OR "), // primary WHERE clause
		50,                                // result limit
	)))
}

func textSearchRank(columnName, query string, subStringMatches bool) *sqlf.Query {
	var rankFunctions []*sqlf.Query
	for _, term := range strings.Fields(query) {
		seq := lexemeSequence(term, subStringMatches)
		rankFunctions = append(rankFunctions, sqlf.Sprintf("ts_rank_cd("+columnName+", %s, 2)", seq))
	}
	return sqlf.Join(rankFunctions, " + ")
}

// Note: This composing a tsquery _value_ and so using fmt.Sprintf instead of sqlf.Sprintf is
// correct here.
func lexemeSequence(s string, subStringmatches bool) string {
	lexemes := lexemes(s)
	sequence := make([]string, 0, len(lexemes))
	for _, lexeme := range lexemes {
		if subStringmatches {
			sequence = append(sequence, fmt.Sprintf("%s:*", lexeme))
		} else {
			sequence = append(sequence, lexeme)
		}
	}
	return strings.Join(sequence, " <-> ")
}

func textSearchQuery(columnName, query string, subStringMatches bool) *sqlf.Query {
	var expressions []*sqlf.Query
	for _, term := range strings.Fields(query) {
		seq := lexemeSequence(term, subStringMatches)
		expressions = append(expressions, sqlf.Sprintf(columnName+" @@ %s", seq))
	}
	return sqlf.Join(expressions, " OR ")
}

const documentationSearchQuery = `
-- source: enterprise/internal/codeintel/stores/lsifstore/documentation.go:DocumentationSearch
SELECT
	id,
	repo_id,
	path_id,
	detail,
	search_key,
	label,
	lang_name,
	repo_name,
	tags,
	search_key_rank,
	search_key_reverse_rank,
	label_rank,
	label_reverse_rank
FROM (
    WITH
        matching_lang_names AS (
            SELECT id, lang_name
            FROM lsif_data_docs_search_lang_names_$SUFFIX
            WHERE %s LIMIT 1
        ),
        matching_repo_names AS (
            SELECT id, repo_name
            FROM lsif_data_docs_search_repo_names_$SUFFIX
            WHERE %s LIMIT 1
        ),
        matching_tags AS (
            SELECT id, tags
            FROM lsif_data_docs_search_tags_$SUFFIX
            WHERE %s LIMIT 1
        )
    SELECT
		lsif_data_docs_search_$SUFFIX.id::bigint,
        repo_id,
        path_id,
        detail,
        search_key,
        label,
        lang_name,
        repo_name,
        tags,
		%s AS search_key_rank,
		%s AS search_key_reverse_rank,
		%s AS label_rank,
		%s AS label_reverse_rank
    FROM lsif_data_docs_search_$SUFFIX
    JOIN lsif_data_docs_search_lang_names_$SUFFIX ON lsif_data_docs_search_lang_names_$SUFFIX.id = lang_name_id
    JOIN lsif_data_docs_search_repo_names_$SUFFIX ON lsif_data_docs_search_repo_names_$SUFFIX.id = repo_name_id
    JOIN lsif_data_docs_search_tags_$SUFFIX ON lsif_data_docs_search_tags_$SUFFIX.id = tags_id
    WHERE
        (%s)

		-- If we found matching lang names, filter to just those.
        AND (CASE WHEN (SELECT COUNT(*) FROM matching_lang_names) > 0 THEN
            lang_name_id = ANY(array(SELECT id FROM matching_lang_names))
        ELSE lang_name_id IS NOT NULL END)

		-- If we found matching repo names, filter to just those.
        AND (CASE WHEN (SELECT COUNT(*) FROM matching_repo_names) > 0 THEN
            repo_name_id = ANY(array(SELECT id FROM matching_repo_names))
        ELSE repo_name_id IS NOT NULL END)

		-- If we found matching tags, filter to just those.
		AND (CASE WHEN (SELECT COUNT(*) FROM matching_tags) > 0 THEN
            tags_id = ANY(array(SELECT id FROM matching_tags))
        ELSE tags_id IS NOT NULL END)

	-- maximum candidates we'll consider: the more candidates we have the better ranking we get,
	-- but the slower searching is. See https://about.sourcegraph.com/blog/postgres-text-search-balancing-query-time-and-relevancy/
	-- 10,000 is an arbitrary choice based on current Sourcegraph.com corpus size and performance,
	-- it'll be tuned as we scale to more repos if perf gets worse or we find we need better
	-- relevance.
    LIMIT 10000
) sub
ORDER BY
	-- First rank by search keys, as those are ideally super specific if you write the correct
	-- format.
    GREATEST(search_key_rank, search_key_reverse_rank) DESC,

	-- Secondarily rank by label, e.g. function signature. These contain less specific info and
	-- due to e.g. containing arguments a function takes, have higher chance of collision with the
	-- desired symbol result, producing a bad match.
    GREATEST(label_rank, label_reverse_rank) DESC,

	-- If all else failed, sort by something reasonable and deterministic.
	repo_name DESC,
	tags DESC,
	id DESC
LIMIT %s -- Maximum results we'll actually return.
`

// scanDocumentationSearchResults reads the documentation search results rows. If no rows matched, (nil, nil) is returned.
func (s *Store) scanDocumentationSearchResults(rows *sql.Rows, queryErr error) (_ []precise.DocumentationSearchResult, err error) {
	if queryErr != nil {
		return nil, queryErr
	}
	defer func() { err = basestore.CloseRows(rows, err) }()

	var results []precise.DocumentationSearchResult
	for rows.Next() {
		var (
			result                   precise.DocumentationSearchResult
			tags                     string
			searchKeyRank, searchKeyReverseRank, labelRank, labelReverseRank float32
		)
		if err := rows.Scan(
			&result.ID,
			&result.RepoID,
			&result.PathID,
			&result.Detail,
			&result.SearchKey,
			&result.Label,
			&result.Lang,
			&result.RepoName,
			&tags,
			&searchKeyRank,
			&searchKeyReverseRank,
			&labelRank,
			&labelReverseRank,
		); err != nil {
			return nil, err
		}
		result.Tags = strings.Fields(tags)
		results = append(results, result)
	}
	return results, nil
}
