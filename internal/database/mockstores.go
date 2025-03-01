package database

var Mocks MockStores

// MockStores has a field for each store interface with the concrete mock type
// (to obviate the need for tedious type assertions in test code).
// DEPRECATED:
//   MockStores has been deprecated in favor of the generated database mocks in
//   internal/database/dbmock. If you came here looking for a store that isn't listed,
//   consider passing in the generated db or stores from there.
type MockStores struct {
	AccessTokens MockAccessTokens

	Repos        MockRepos
	Orgs         MockOrgs
	OrgMembers   MockOrgMembers
	Users        MockUsers
	UserEmails   MockUserEmails
	SubRepoPerms MockSubRepoPerms

	ExternalAccounts MockExternalAccounts

	ExternalServices MockExternalServices
}
