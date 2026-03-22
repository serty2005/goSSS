package detector

import (
	"context"
	"testing"

	"etalon-agent/internal/iikosyrverms/domain"
	"etalon-agent/internal/iikosyrverms/registry"
	"etalon-agent/internal/iikosyrverms/testsupport"
)

func TestDetectFindsIikoCandidate(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := testsupport.MaterializeAppDataFixture(root, "iiko_active"); err != nil {
		t.Fatalf("не удалось подготовить фикстуру: %v", err)
	}

	knownPaths, candidates, err := Detect(context.Background(), []registry.AppDataRoot{{Path: root, Priority: 0}}, registry.DefaultProducts())
	if err != nil {
		t.Fatalf("Detect вернул ошибку: %v", err)
	}
	if len(knownPaths) == 0 {
		t.Fatal("ожидались проверенные известные пути")
	}
	if len(candidates) != 1 {
		t.Fatalf("ожидался один кандидат, получено %d", len(candidates))
	}
	if candidates[0].SoftwareType != domain.SoftwareTypeIiko {
		t.Fatalf("ожидался software_type=iiko, получено %q", candidates[0].SoftwareType)
	}
	if len(candidates[0].ConfigFiles) != 1 {
		t.Fatalf("ожидался один config.xml, получено %d", len(candidates[0].ConfigFiles))
	}
}

func TestDetectFindsSyrveCandidate(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := testsupport.MaterializeAppDataFixture(root, "syrve_active"); err != nil {
		t.Fatalf("не удалось подготовить фикстуру: %v", err)
	}

	_, candidates, err := Detect(context.Background(), []registry.AppDataRoot{{Path: root, Priority: 0}}, registry.DefaultProducts())
	if err != nil {
		t.Fatalf("Detect вернул ошибку: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("ожидался один кандидат, получено %d", len(candidates))
	}
	if candidates[0].SoftwareType != domain.SoftwareTypeSyrve {
		t.Fatalf("ожидался software_type=syrve, получено %q", candidates[0].SoftwareType)
	}
	if candidates[0].ActivityPath == "" {
		t.Fatal("ожидался определённый active path")
	}
}
