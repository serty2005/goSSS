package gateways

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"etalon-server/internal/domain/integration"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type fakeTicketFileRepo struct {
	filesByID      map[string]*tickets.FileAsset
	fileIDByKey    map[string]string
	relationByKey  map[string]*tickets.TicketFileLink
	relationByType map[string]int
}

func newFakeTicketFileRepo() *fakeTicketFileRepo {
	return &fakeTicketFileRepo{
		filesByID:      make(map[string]*tickets.FileAsset),
		fileIDByKey:    make(map[string]string),
		relationByKey:  make(map[string]*tickets.TicketFileLink),
		relationByType: make(map[string]int),
	}
}

func (r *fakeTicketFileRepo) UpsertFileAsset(ctx context.Context, file *tickets.FileAsset) (*tickets.FileAsset, error) {
	if existingID, ok := r.fileIDByKey[file.StorageKey]; ok {
		p := r.filesByID[existingID]
		p.OriginalName = file.OriginalName
		p.MimeType = file.MimeType
		p.Size = file.Size
		p.Checksum = file.Checksum
		p.UpdatedAt = time.Now()
		return p, nil
	}

	copyFile := *file
	if strings.TrimSpace(copyFile.ID) == "" {
		copyFile.ID = uuid.New().String()
	}
	copyFile.CreatedAt = time.Now()
	copyFile.UpdatedAt = time.Now()
	r.filesByID[copyFile.ID] = &copyFile
	r.fileIDByKey[copyFile.StorageKey] = copyFile.ID
	return &copyFile, nil
}

func (r *fakeTicketFileRepo) GetFileAssetByID(ctx context.Context, id string) (*tickets.FileAsset, error) {
	if f, ok := r.filesByID[id]; ok {
		copyFile := *f
		return &copyFile, nil
	}
	return nil, nil
}

func (r *fakeTicketFileRepo) UpsertTicketFileLink(ctx context.Context, link *tickets.TicketFileLink) error {
	comment := ""
	if link.CommentUUID != nil {
		comment = *link.CommentUUID
	}
	key := fmt.Sprintf("%s|%s|%s|%s", link.TicketID, link.FileID, link.RelationType, comment)
	if existing, ok := r.relationByKey[key]; ok {
		existing.UpdatedAt = time.Now()
		return nil
	}
	copyLink := *link
	if strings.TrimSpace(copyLink.ID) == "" {
		copyLink.ID = uuid.New().String()
	}
	copyLink.CreatedAt = time.Now()
	copyLink.UpdatedAt = time.Now()
	r.relationByKey[key] = &copyLink
	r.relationByType[copyLink.RelationType]++
	return nil
}

func (r *fakeTicketFileRepo) GetTicketFileLinksByRelation(ctx context.Context, ticketID string, relationTypes []string) ([]tickets.TicketFileLink, error) {
	typeSet := make(map[string]struct{}, len(relationTypes))
	for _, t := range relationTypes {
		typeSet[t] = struct{}{}
	}
	out := make([]tickets.TicketFileLink, 0)
	for _, link := range r.relationByKey {
		if link.TicketID != ticketID {
			continue
		}
		if len(typeSet) > 0 {
			if _, ok := typeSet[link.RelationType]; !ok {
				continue
			}
		}
		out = append(out, *link)
	}
	return out, nil
}

func (r *fakeTicketFileRepo) countRelationByType(t string) int {
	return r.relationByType[t]
}

func (r *fakeTicketFileRepo) relationFileIDsByType(t string) []string {
	ids := make([]string, 0)
	for _, link := range r.relationByKey {
		if link.RelationType == t {
			ids = append(ids, link.FileID)
		}
	}
	return ids
}

type fakeLinkRepo struct {
	byExternal map[string]*models.ExternalSystemLink
}

func newFakeLinkRepo() *fakeLinkRepo {
	return &fakeLinkRepo{byExternal: make(map[string]*models.ExternalSystemLink)}
}

func (r *fakeLinkRepo) key(systemName, externalID string) string {
	return systemName + "|" + externalID
}

func (r *fakeLinkRepo) GetByExternalID(ctx context.Context, tx *gorm.DB, systemName, externalID string) (*models.ExternalSystemLink, error) {
	if link, ok := r.byExternal[r.key(systemName, externalID)]; ok {
		cp := *link
		return &cp, nil
	}
	return nil, nil
}

func (r *fakeLinkRepo) Upsert(ctx context.Context, tx *gorm.DB, link *models.ExternalSystemLink) error {
	cp := *link
	r.byExternal[r.key(link.SystemName, link.ServiceDeskUUID)] = &cp
	return nil
}

type fakeTicketProvider struct {
	directBySource map[string][]integration.RemoteFile
	bodyByFileUUID map[string][]byte
	mimeByFileUUID map[string]string
	downloadCalls  map[string]int
}

func (p *fakeTicketProvider) GetFilesBySource(ctx context.Context, sourceUUID string) ([]integration.RemoteFile, error) {
	return p.directBySource[sourceUUID], nil
}

func (p *fakeTicketProvider) GetFilesBySources(ctx context.Context, sourceUUIDs []string) (map[string][]integration.RemoteFile, error) {
	result := make(map[string][]integration.RemoteFile, len(sourceUUIDs))
	for _, sourceUUID := range sourceUUIDs {
		result[sourceUUID] = p.directBySource[sourceUUID]
	}
	return result, nil
}

func (p *fakeTicketProvider) DownloadFile(ctx context.Context, fileUUID string) ([]byte, string, error) {
	p.downloadCalls[fileUUID]++
	body, ok := p.bodyByFileUUID[fileUUID]
	if !ok {
		return nil, "", fmt.Errorf("файл %s не найден", fileUUID)
	}
	return body, p.mimeByFileUUID[fileUUID], nil
}

func newTestFileSyncService(t *testing.T) (*ticketFileSyncService, *fakeTicketFileRepo, *fakeLinkRepo, *fakeTicketProvider, string) {
	t.Helper()
	tmpDir := t.TempDir()

	cfg := &config.Config{
		TicketStoragePath: tmpDir,
	}
	log := logger.NewSlogLogger("", "test", "info", true)
	repo := newFakeTicketFileRepo()
	linkRepo := newFakeLinkRepo()
	provider := &fakeTicketProvider{
		directBySource: make(map[string][]integration.RemoteFile),
		bodyByFileUUID: make(map[string][]byte),
		mimeByFileUUID: make(map[string]string),
		downloadCalls:  make(map[string]int),
	}

	svc := newTicketFileSyncService(cfg, log, repo, linkRepo)
	return svc, repo, linkRepo, provider, tmpDir
}

func TestTicketFileSyncService_IdempotentDirectAndInline(t *testing.T) {
	ctx := context.Background()
	svc, repo, _, provider, storageRoot := newTestFileSyncService(t)

	provider.directBySource["serviceCall$1"] = []integration.RemoteFile{
		{UUID: "file$100", Name: "direct.png", MimeType: "image/png", Size: 10},
	}
	provider.bodyByFileUUID["file$100"] = []byte("direct-bytes")
	provider.mimeByFileUUID["file$100"] = "image/png"

	provider.bodyByFileUUID["file$200"] = []byte("desc-inline")
	provider.mimeByFileUUID["file$200"] = "image/png"
	provider.bodyByFileUUID["file$300"] = []byte("comment-inline")
	provider.mimeByFileUUID["file$300"] = "image/png"

	description := `<p><img src="./download?uuid=file$200"></p>`
	comment := `<a href="/download?uuid=file$300">x</a>`
	commentUUID := "comment$1"

	for i := 0; i < 2; i++ {
		description = svc.ProcessInlineContent(ctx, provider, "ticket-1", "serviceCall$1", description, tickets.RelationTypeInlineDescription, nil)
		comment = svc.ProcessInlineContent(ctx, provider, "ticket-1", "serviceCall$1", comment, tickets.RelationTypeInlineComment, &commentUUID)
		err := svc.SyncDirectTicketFiles(ctx, provider, "ticket-1", "serviceCall$1")
		require.NoError(t, err)
	}

	assert.Equal(t, 1, repo.countRelationByType(tickets.RelationTypeDirectTicketAttachment))
	assert.Equal(t, 1, repo.countRelationByType(tickets.RelationTypeInlineDescription))
	assert.Equal(t, 1, repo.countRelationByType(tickets.RelationTypeInlineComment))
	assert.Equal(t, 3, len(repo.filesByID))

	assert.Contains(t, description, "/api/static/tickets/")
	assert.Contains(t, comment, "/api/static/tickets/")

	for _, f := range repo.filesByID {
		abs := filepath.Join(storageRoot, filepath.FromSlash(f.StorageKey))
		_, err := os.Stat(abs)
		assert.NoError(t, err)
	}
}

func TestTicketFileSyncService_RestoreByExistingExternalLink(t *testing.T) {
	ctx := context.Background()
	svc, repo, linkRepo, provider, _ := newTestFileSyncService(t)

	existing := &tickets.FileAsset{
		ID:           "internal-file-1",
		StorageKey:   "files/internal-file-1.png",
		OriginalName: "old.png",
		MimeType:     "image/png",
		Size:         5,
		Checksum:     "old",
	}
	_, err := repo.UpsertFileAsset(ctx, existing)
	require.NoError(t, err)

	err = linkRepo.Upsert(ctx, nil, &models.ExternalSystemLink{
		InternalID:      "internal-file-1",
		SystemName:      "naumen",
		ServiceDeskUUID: "file$777",
		EntityType:      "File",
	})
	require.NoError(t, err)

	provider.bodyByFileUUID["file$777"] = []byte("new-bytes")
	provider.mimeByFileUUID["file$777"] = "image/png"

	html := svc.ProcessInlineContent(ctx, provider, "ticket-1", "serviceCall$1", `<img src="./download?uuid=file$777">`, tickets.RelationTypeInlineDescription, nil)
	assert.Contains(t, html, "/api/static/tickets/")

	assert.Equal(t, 1, len(repo.filesByID))
	ids := repo.relationFileIDsByType(tickets.RelationTypeInlineDescription)
	require.Len(t, ids, 1)
	assert.Equal(t, "internal-file-1", ids[0])
}

func TestTicketFileSyncService_RenameSameExternalUUID(t *testing.T) {
	ctx := context.Background()
	svc, repo, linkRepo, provider, _ := newTestFileSyncService(t)

	provider.directBySource["serviceCall$1"] = []integration.RemoteFile{
		{UUID: "file$500", Name: "old_name.png", MimeType: "image/png", Size: 10},
	}
	provider.bodyByFileUUID["file$500"] = []byte("same-bytes")
	provider.mimeByFileUUID["file$500"] = "image/png"

	err := svc.SyncDirectTicketFiles(ctx, provider, "ticket-1", "serviceCall$1")
	require.NoError(t, err)
	assert.Equal(t, 1, provider.downloadCalls["file$500"])

	link, err := linkRepo.GetByExternalID(ctx, nil, "naumen", "file$500")
	require.NoError(t, err)
	require.NotNil(t, link)
	fileBefore, err := repo.GetFileAssetByID(ctx, link.InternalID)
	require.NoError(t, err)
	require.NotNil(t, fileBefore)
	assert.Equal(t, "old_name.png", fileBefore.OriginalName)

	provider.directBySource["serviceCall$1"] = []integration.RemoteFile{
		{UUID: "file$500", Name: "new_name.png", MimeType: "image/png", Size: 10},
	}
	err = svc.SyncDirectTicketFiles(ctx, provider, "ticket-1", "serviceCall$1")
	require.NoError(t, err)
	assert.Equal(t, 1, provider.downloadCalls["file$500"])

	fileAfter, err := repo.GetFileAssetByID(ctx, link.InternalID)
	require.NoError(t, err)
	require.NotNil(t, fileAfter)
	assert.Equal(t, "new_name.png", fileAfter.OriginalName)
	assert.Equal(t, 1, len(repo.filesByID))
	assert.Equal(t, 1, repo.countRelationByType(tickets.RelationTypeDirectTicketAttachment))
}

func TestTicketFileSyncService_SkipDownloadForUnchangedDirectFile(t *testing.T) {
	ctx := context.Background()
	svc, _, _, provider, _ := newTestFileSyncService(t)

	provider.directBySource["serviceCall$1"] = []integration.RemoteFile{
		{UUID: "file$900", Name: "report.pdf", MimeType: "application/pdf", Size: 10},
	}
	provider.bodyByFileUUID["file$900"] = []byte("0123456789")
	provider.mimeByFileUUID["file$900"] = "application/pdf"

	err := svc.SyncDirectTicketFiles(ctx, provider, "ticket-1", "serviceCall$1")
	require.NoError(t, err)
	assert.Equal(t, 1, provider.downloadCalls["file$900"])

	err = svc.SyncDirectTicketFiles(ctx, provider, "ticket-1", "serviceCall$1")
	require.NoError(t, err)
	assert.Equal(t, 1, provider.downloadCalls["file$900"])
}
