package services

import (
	"testing"

	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/user"
)

func TestCanManageTicketCommentOnlyAuthor(t *testing.T) {
	authorID := uint(7)
	comment := &tickets.TicketComment{
		AuthorUserID: &authorID,
		AuthorName:   "Автор комментария",
	}
	ticket := &tickets.Ticket{}

	admin := &user.User{ID: 1, FullName: "Администратор"}
	if canEditTicketComment(ticket, admin, comment, []string{user.RoleAdmin}) {
		t.Fatal("администратор не должен редактировать чужой комментарий")
	}
	if canDeleteTicketComment(ticket, admin, comment, []string{user.RoleAdmin}) {
		t.Fatal("администратор не должен удалять чужой комментарий")
	}

	author := &user.User{ID: authorID, FullName: "Автор комментария"}
	if !canEditTicketComment(ticket, author, comment, nil) {
		t.Fatal("автор должен редактировать свой комментарий")
	}
	if !canDeleteTicketComment(ticket, author, comment, nil) {
		t.Fatal("автор должен удалять свой комментарий")
	}
}
