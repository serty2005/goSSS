package services

import (
	"etalon-server/internal/domain/user"
	pyrusplugin "etalon-server/internal/infra/plugins/pyrus"
	"testing"
)

func TestFindPyrusUserSuggestionByIdentity_PrefersEmail(t *testing.T) {
	members := []pyrusplugin.Member{
		{ID: 101, FirstName: "Иван", LastName: "Петров", Email: "ivan.petrov@example.com", Type: "user"},
		{ID: 102, FirstName: "Иван", LastName: "Петров", Email: "other@example.com", Type: "user"},
	}

	suggestion := FindPyrusUserSuggestionByIdentity("Иван", "Петров", "Иван Петров", "ivan.petrov@example.com", members)
	if suggestion == nil {
		t.Fatal("ожидали suggestion по email")
	}
	if suggestion.PyrusUserID != 101 {
		t.Fatalf("ожидали pyrus_user_id=101, получили %d", suggestion.PyrusUserID)
	}
}

func TestFindPyrusUserSuggestionForUser_ReturnsNilOnAmbiguousName(t *testing.T) {
	u := &user.User{
		FirstName: "Иван",
		LastName:  "Петров",
		FullName:  "Иван Петров",
	}
	members := []pyrusplugin.Member{
		{ID: 101, FirstName: "Иван", LastName: "Петров", Email: "first@example.com", Type: "user"},
		{ID: 102, FirstName: "Иван", LastName: "Петров", Email: "second@example.com", Type: "user"},
	}

	if suggestion := FindPyrusUserSuggestionForUser(u, members); suggestion != nil {
		t.Fatalf("не ожидали suggestion при неоднозначном совпадении, получили %+v", suggestion)
	}
}

func TestVerifyPyrusUserMatch_UsesEmail(t *testing.T) {
	email := "ivan.petrov@example.com"
	u := &user.User{
		FirstName: "Иван",
		LastName:  "Сидоров",
		FullName:  "Иван Сидоров",
		Email:     &email,
	}
	members := []pyrusplugin.Member{
		{ID: 101, FirstName: "Иван", LastName: "Петров", Email: email, Type: "user"},
	}

	verified, name, matchedEmail := VerifyPyrusUserMatch(u, "101", members)
	if !verified {
		t.Fatal("ожидали успешную верификацию по email")
	}
	if name != "Иван Петров" {
		t.Fatalf("ожидали имя 'Иван Петров', получили %q", name)
	}
	if matchedEmail != email {
		t.Fatalf("ожидали email %q, получили %q", email, matchedEmail)
	}
}
