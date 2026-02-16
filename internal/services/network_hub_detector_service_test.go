package services

import (
	"context"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/infra/repositories"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupHubDetector(t *testing.T) (*gorm.DB, *NetworkHubDetectorService) {
	t.Helper()
	dsn := "file:" + uuid.NewString() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&company.Company{},
		&server.Server{},
		&workstation.Workstation{},
		&fiscal.FiscalRegister{},
	))
	companyRepo := repositories.NewCompanyRepo(db)
	detector := NewNetworkHubDetectorService(logger.New("", "test", "error", true), db, companyRepo)
	return db, detector
}

func TestNetworkHubDetector_ParentWorkstationsDoNotDisableHub(t *testing.T) {
	db, detector := setupHubDetector(t)

	parent := company.Company{Title: strRef("Родитель"), OwnerMode: "normal"}
	require.NoError(t, db.Create(&parent).Error)
	child := company.Company{Title: strRef("Дочерняя"), ParentID: strRef(parent.ID), OwnerMode: "normal"}
	require.NoError(t, db.Create(&child).Error)

	require.NoError(t, db.Create(&server.Server{OwnerID: strRef(parent.ID)}).Error)
	require.NoError(t, db.Create(&workstation.Workstation{OwnerID: strRef(parent.ID)}).Error)
	require.NoError(t, db.Create(&workstation.Workstation{OwnerID: strRef(child.ID), Teamviewer: strRef("123")}).Error)

	ok, err := detector.IsNetworkHub(context.Background(), parent.ID)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestNetworkHubDetector_ChildServersDisableHub(t *testing.T) {
	db, detector := setupHubDetector(t)

	parent := company.Company{Title: strRef("Родитель"), OwnerMode: "normal"}
	require.NoError(t, db.Create(&parent).Error)
	child := company.Company{Title: strRef("Дочерняя"), ParentID: strRef(parent.ID), OwnerMode: "normal"}
	require.NoError(t, db.Create(&child).Error)

	require.NoError(t, db.Create(&server.Server{OwnerID: strRef(parent.ID)}).Error)
	require.NoError(t, db.Create(&server.Server{OwnerID: strRef(child.ID)}).Error)
	require.NoError(t, db.Create(&workstation.Workstation{OwnerID: strRef(child.ID), Teamviewer: strRef("123")}).Error)

	ok, err := detector.IsNetworkHub(context.Background(), parent.ID)
	require.NoError(t, err)
	require.False(t, ok)
}
