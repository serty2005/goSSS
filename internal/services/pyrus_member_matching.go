package services

import (
	"etalon-server/internal/domain/user"
	pyrusplugin "etalon-server/internal/infra/plugins/pyrus"
	"strconv"
	"strings"
)

type PyrusUserSuggestion struct {
	PyrusUserID int64
	Name        string
	Email       string
}

func FindPyrusUserSuggestionForUser(u *user.User, members []pyrusplugin.Member) *PyrusUserSuggestion {
	if u == nil {
		return nil
	}
	email := ""
	if u.Email != nil {
		email = *u.Email
	}
	return FindPyrusUserSuggestionByIdentity(u.FirstName, u.LastName, u.FullName, email, members)
}

func FindPyrusUserSuggestionByIdentity(firstName, lastName, fullName, email string, members []pyrusplugin.Member) *PyrusUserSuggestion {
	if len(members) == 0 {
		return nil
	}

	activeMembers := make([]pyrusplugin.Member, 0, len(members))
	for i := range members {
		member := members[i]
		if !isActivePyrusMember(member) {
			continue
		}
		activeMembers = append(activeMembers, member)
	}
	if len(activeMembers) == 0 {
		return nil
	}

	if normalizedEmail := normalizePyrusEmail(email); normalizedEmail != "" {
		emailMatches := make([]pyrusplugin.Member, 0, 1)
		for i := range activeMembers {
			if normalizePyrusEmail(activeMembers[i].Email) != normalizedEmail {
				continue
			}
			emailMatches = append(emailMatches, activeMembers[i])
		}
		if len(emailMatches) == 1 {
			return buildPyrusUserSuggestion(emailMatches[0])
		}
		if len(emailMatches) > 1 {
			return nil
		}
	}

	userFirst := normalizePersonToken(firstName)
	userLast := normalizePersonToken(lastName)
	userFull := normalizePersonToken(fullName)
	if userFirst == "" || userLast == "" {
		if userFull == "" {
			return nil
		}
	}

	matches := make([]pyrusplugin.Member, 0, 1)
	for i := range activeMembers {
		memberFirst := normalizePersonToken(activeMembers[i].FirstName)
		memberLast := normalizePersonToken(activeMembers[i].LastName)
		memberFull := normalizePersonToken(activeMembers[i].DisplayName())
		if userFirst != "" && userLast != "" && userFirst == memberFirst && userLast == memberLast {
			matches = append(matches, activeMembers[i])
			continue
		}
		if userFull != "" && userFull == memberFull {
			matches = append(matches, activeMembers[i])
		}
	}
	if len(matches) != 1 {
		return nil
	}
	return buildPyrusUserSuggestion(matches[0])
}

func VerifyPyrusUserMatch(u *user.User, externalID string, members []pyrusplugin.Member) (bool, string, string) {
	if u == nil {
		return false, "", ""
	}
	targetID, err := strconv.ParseInt(strings.TrimSpace(externalID), 10, 64)
	if err != nil || targetID <= 0 {
		return false, "", ""
	}
	member := findPyrusMemberByID(members, targetID)
	if member == nil || !isActivePyrusMember(*member) {
		return false, "", ""
	}

	memberName := strings.TrimSpace(member.DisplayName())
	memberEmail := strings.TrimSpace(member.Email)
	if normalizedEmail := normalizePyrusEmail(memberEmail); normalizedEmail != "" && u.Email != nil && normalizePyrusEmail(*u.Email) == normalizedEmail {
		return true, memberName, memberEmail
	}

	userFirst := normalizePersonToken(u.FirstName)
	userLast := normalizePersonToken(u.LastName)
	memberFirst := normalizePersonToken(member.FirstName)
	memberLast := normalizePersonToken(member.LastName)
	if userFirst != "" && userLast != "" && userFirst == memberFirst && userLast == memberLast {
		return true, memberName, memberEmail
	}

	userFull := normalizePersonToken(u.FullName)
	memberFull := normalizePersonToken(member.DisplayName())
	if userFull != "" && userFull == memberFull {
		return true, memberName, memberEmail
	}
	return false, "", memberEmail
}

func CollectVerifiedPyrusUserIDs(u *user.User) []int64 {
	if u == nil {
		return nil
	}
	ids := make([]int64, 0, len(u.Integrations)+1)
	seen := make(map[int64]struct{}, len(u.Integrations)+1)

	for i := range u.Integrations {
		integration := u.Integrations[i]
		if strings.TrimSpace(strings.ToLower(integration.IntegrationType)) != user.ExternalTypePyrus {
			continue
		}
		if !integration.IsEnabled {
			continue
		}
		if !integration.IsVerified && !integration.IsLocked {
			continue
		}
		id, err := strconv.ParseInt(strings.TrimSpace(integration.ExternalID), 10, 64)
		if err != nil || id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	if u.ExternalType != nil && u.ExternalID != nil &&
		strings.TrimSpace(strings.ToLower(*u.ExternalType)) == user.ExternalTypePyrus {
		id, err := strconv.ParseInt(strings.TrimSpace(*u.ExternalID), 10, 64)
		if err == nil && id > 0 {
			if _, exists := seen[id]; !exists {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

func FindPyrusMemberByID(members []pyrusplugin.Member, pyrusUserID int64) *pyrusplugin.Member {
	return findPyrusMemberByID(members, pyrusUserID)
}

func findPyrusMemberByID(members []pyrusplugin.Member, pyrusUserID int64) *pyrusplugin.Member {
	for i := range members {
		if members[i].ID != pyrusUserID {
			continue
		}
		return &members[i]
	}
	return nil
}

func buildPyrusUserSuggestion(member pyrusplugin.Member) *PyrusUserSuggestion {
	return &PyrusUserSuggestion{
		PyrusUserID: member.ID,
		Name:        strings.TrimSpace(member.DisplayName()),
		Email:       strings.TrimSpace(member.Email),
	}
}

func isActivePyrusMember(member pyrusplugin.Member) bool {
	return strings.TrimSpace(strings.ToLower(member.Type)) == "user" && !member.Banned && !member.Fired
}

func normalizePyrusEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
