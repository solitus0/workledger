package reconcile

import "github.com/solitus0/workledger/internal/adapter/jiradatacenter"

func jiraWorklogAuthorMatchesUser(author jiradatacenter.WorklogUser, user jiradatacenter.User) bool {
	switch {
	case author.AccountID != "" && user.AccountID != "":
		return author.AccountID == user.AccountID
	case author.Key != "" && user.Key != "":
		return author.Key == user.Key
	default:
		return author.Name != "" && author.Name == user.Name
	}
}
