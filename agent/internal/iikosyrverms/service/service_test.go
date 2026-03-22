package service

import (
	"context"
	"testing"

	"etalon-agent/internal/iikosyrverms/domain"
	"etalon-agent/internal/iikosyrverms/registry"
	"etalon-agent/internal/iikosyrverms/testsupport"
)

func TestServiceSelectsFreshestCandidateAcrossProducts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := testsupport.MaterializeAppDataFixture(root, "multi_candidate"); err != nil {
		t.Fatalf("не удалось подготовить фикстуру: %v", err)
	}

	service := New(
		WithPlatform("windows", "amd64"),
		WithDiscovery(registry.Discovery{
			EnvPath:      root,
			EnvAvailable: true,
			Roots:        []registry.AppDataRoot{{Path: root, Priority: 0}},
		}),
	)

	report, err := service.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan вернул ошибку: %v", err)
	}
	if report.SoftwareType != domain.SoftwareTypeSyrve {
		t.Fatalf("ожидался software_type=syrve, получено %q", report.SoftwareType)
	}
	if report.RMSURL != "https://newer.syrve.local/resto/" {
		t.Fatalf("ожидался RMS URL от самого свежего кандидата, получено %q", report.RMSURL)
	}
}

func TestServiceReturnsUnknownWhenSoftwareNotFound(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	service := New(
		WithPlatform("windows", "amd64"),
		WithDiscovery(registry.Discovery{
			EnvPath:      root,
			EnvAvailable: true,
			Roots:        []registry.AppDataRoot{{Path: root, Priority: 0}},
		}),
	)

	report, err := service.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan вернул ошибку: %v", err)
	}
	if report.SoftwareType != domain.SoftwareTypeUnknown {
		t.Fatalf("ожидался software_type=unknown, получено %q", report.SoftwareType)
	}
}

func TestServiceKeepsSoftwareTypeWhenRMSURLMissing(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := testsupport.MaterializeAppDataFixture(root, "missing_url"); err != nil {
		t.Fatalf("не удалось подготовить фикстуру: %v", err)
	}

	service := New(
		WithPlatform("windows", "amd64"),
		WithDiscovery(registry.Discovery{
			EnvPath:      root,
			EnvAvailable: true,
			Roots:        []registry.AppDataRoot{{Path: root, Priority: 0}},
		}),
	)

	report, err := service.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan вернул ошибку: %v", err)
	}
	if report.SoftwareType != domain.SoftwareTypeIiko {
		t.Fatalf("ожидался software_type=iiko, получено %q", report.SoftwareType)
	}
	if report.RMSURL != "" {
		t.Fatalf("ожидался пустой RMS URL, получено %q", report.RMSURL)
	}
}
