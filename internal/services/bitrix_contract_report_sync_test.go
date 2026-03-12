package services

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildDuplicateNameConflictsIncludesOnlyUnmappedDeletionCandidates(t *testing.T) {
	statesByName := map[string][]bitrixServicePointState{
		"test point": {
			{ID: 101, Name: "Test Point"},
			{ID: 102, Name: "Test Point"},
			{ID: 103, Name: "Test Point"},
		},
	}
	mappedPointIDs := map[int64]struct{}{
		101: {},
	}

	conflicts := buildDuplicateNameConflicts(statesByName, mappedPointIDs)
	require.Len(t, conflicts, 1)
	require.Equal(t, "Test Point", conflicts[0].ServicePointName)
	require.Equal(t, []int64{101, 102, 103}, conflicts[0].MatchedPointIDs)
	require.Equal(t, []int64{101}, conflicts[0].MappedPointIDs)
	require.Equal(t, []int64{102, 103}, conflicts[0].DeletionCandidateIDs)
}

func TestBuildDuplicateNameConflictsSkipsGroupsWithoutDeletionCandidates(t *testing.T) {
	statesByName := map[string][]bitrixServicePointState{
		"mapped point": {
			{ID: 201, Name: "Mapped Point"},
			{ID: 202, Name: "Mapped Point"},
		},
	}
	mappedPointIDs := map[int64]struct{}{
		201: {},
		202: {},
	}

	conflicts := buildDuplicateNameConflicts(statesByName, mappedPointIDs)
	require.Empty(t, conflicts)
}
