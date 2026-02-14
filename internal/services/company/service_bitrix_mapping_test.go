package company

import (
	"context"
	"testing"

	"etalon-server/internal/domain/bitrix"
	domainCompany "etalon-server/internal/domain/company"
	"etalon-server/internal/domain/contract"
	"etalon-server/internal/domain/models"
	dbpkg "etalon-server/internal/infra/db"
	"etalon-server/internal/infra/repositories"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestUpdateBitrixMapping_AssignReassignAndClear(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("РЅРµ СѓРґР°Р»РѕСЃСЊ РѕС‚РєСЂС‹С‚СЊ in-memory Р‘Р”: %v", err)
	}

	if err := db.AutoMigrate(
		&domainCompany.Company{},
		&contract.Contract{},
		&models.CompanyContract{},
		&bitrix.ServicePoint{},
		&bitrix.CompanyServicePointMapping{},
	); err != nil {
		t.Fatalf("РЅРµ СѓРґР°Р»РѕСЃСЊ РїРѕРґРіРѕС‚РѕРІРёС‚СЊ СЃС…РµРјСѓ: %v", err)
	}

	companyRepo := repositories.NewCompanyRepo(db)
	bitrixRepo := repositories.NewBitrixRepo(db)

	ctx := context.Background()

	title1 := "РљРѕРјРїР°РЅРёСЏ 1"
	title2 := "РљРѕРјРїР°РЅРёСЏ 2"
	company1 := &domainCompany.Company{Title: &title1}
	company2 := &domainCompany.Company{Title: &title2}
	if err := companyRepo.Create(ctx, company1); err != nil {
		t.Fatalf("РЅРµ СѓРґР°Р»РѕСЃСЊ СЃРѕР·РґР°С‚СЊ company1: %v", err)
	}
	if err := companyRepo.Create(ctx, company2); err != nil {
		t.Fatalf("РЅРµ СѓРґР°Р»РѕСЃСЊ СЃРѕР·РґР°С‚СЊ company2: %v", err)
	}

	if err := db.Create(&bitrix.ServicePoint{B24ElementID: 101, Name: "РўРѕС‡РєР° 101"}).Error; err != nil {
		t.Fatalf("РЅРµ СѓРґР°Р»РѕСЃСЊ СЃРѕР·РґР°С‚СЊ С‚РѕС‡РєСѓ 101: %v", err)
	}
	if err := db.Create(&bitrix.ServicePoint{B24ElementID: 102, Name: "РўРѕС‡РєР° 102"}).Error; err != nil {
		t.Fatalf("РЅРµ СѓРґР°Р»РѕСЃСЊ СЃРѕР·РґР°С‚СЊ С‚РѕС‡РєСѓ 102: %v", err)
	}

	svc := &serviceImpl{
		tm:          dbpkg.NewGormTransactor(db),
		companyRepo: companyRepo,
		bitrixRepo:  bitrixRepo,
	}

	point101 := int64(101)
	if err := svc.UpdateBitrixMapping(ctx, &company1.ID, &point101); err != nil {
		t.Fatalf("РЅРµ СѓРґР°Р»РѕСЃСЊ РЅР°Р·РЅР°С‡РёС‚СЊ mapping company1->101: %v", err)
	}

	item, err := bitrixRepo.GetCompanyServicePointMappingByCompanyID(ctx, company1.ID)
	if err != nil || item == nil || item.BitrixServicePointID != 101 {
		t.Fatalf("РѕР¶РёРґР°Р»Рё mapping company1->101, РїРѕР»СѓС‡РёР»Рё item=%v err=%v", item, err)
	}

	if err := svc.UpdateBitrixMapping(ctx, &company2.ID, &point101); err != nil {
		t.Fatalf("РЅРµ СѓРґР°Р»РѕСЃСЊ РїРµСЂРµРЅР°Р·РЅР°С‡РёС‚СЊ mapping РЅР° company2: %v", err)
	}

	oldItem, _ := bitrixRepo.GetCompanyServicePointMappingByCompanyID(ctx, company1.ID)
	if oldItem != nil {
		t.Fatalf("РѕР¶РёРґР°Р»Рё, С‡С‚Рѕ mapping company1 Р±СѓРґРµС‚ СѓРґР°Р»С‘РЅ")
	}
	newItem, _ := bitrixRepo.GetCompanyServicePointMappingByCompanyID(ctx, company2.ID)
	if newItem == nil || newItem.BitrixServicePointID != 101 {
		t.Fatalf("РѕР¶РёРґР°Р»Рё mapping company2->101, РїРѕР»СѓС‡РёР»Рё %v", newItem)
	}

	if err := svc.UpdateBitrixMapping(ctx, &company2.ID, nil); err != nil {
		t.Fatalf("РЅРµ СѓРґР°Р»РѕСЃСЊ РѕС‡РёСЃС‚РёС‚СЊ mapping РїРѕ company2: %v", err)
	}
	clearedByCompany, _ := bitrixRepo.GetCompanyServicePointMappingByCompanyID(ctx, company2.ID)
	if clearedByCompany != nil {
		t.Fatalf("РѕР¶РёРґР°Р»Рё, С‡С‚Рѕ mapping company2 Р±СѓРґРµС‚ РѕС‡РёС‰РµРЅ")
	}

	point102 := int64(102)
	if err := svc.UpdateBitrixMapping(ctx, &company1.ID, &point102); err != nil {
		t.Fatalf("РЅРµ СѓРґР°Р»РѕСЃСЊ РЅР°Р·РЅР°С‡РёС‚СЊ mapping company1->102: %v", err)
	}
	if err := svc.UpdateBitrixMapping(ctx, nil, &point102); err != nil {
		t.Fatalf("РЅРµ СѓРґР°Р»РѕСЃСЊ РѕС‡РёСЃС‚РёС‚СЊ mapping РїРѕ С‚РѕС‡РєРµ 102: %v", err)
	}

	clearedByPoint, _ := bitrixRepo.GetCompanyServicePointMappingByPointID(ctx, 102)
	if clearedByPoint != nil {
		t.Fatalf("РѕР¶РёРґР°Р»Рё, С‡С‚Рѕ mapping РїРѕ С‚РѕС‡РєРµ 102 Р±СѓРґРµС‚ РѕС‡РёС‰РµРЅ")
	}
}
