package services

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// EnsureFiscalSerialUniqueness нормализует серийники ФР, удаляет дубли и создает уникальный индекс.
// Правило выбора записи при дублях: сохраняется запись с более поздней датой окончания ФН.
func EnsureFiscalSerialUniqueness(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Снимаем все известные варианты уникального индекса перед массовой нормализацией,
		// иначе UPDATE может упасть на промежуточных конфликтах.
		if err := tx.Exec(`DROP INDEX IF EXISTS idx_fiscal_registers_fr_serial_normalized`).Error; err != nil {
			return fmt.Errorf("не удалось удалить старый уникальный индекс fr_serial_normalized: %w", err)
		}
		if err := tx.Exec(`DROP INDEX IF EXISTS idx_fiscal_registers_fr_serial_normalized_unique`).Error; err != nil {
			return fmt.Errorf("не удалось удалить новый уникальный индекс fr_serial_normalized: %w", err)
		}

		// Нормализуем серийный номер: trim + upper + удаление пробелов.
		if err := tx.Exec(`
			UPDATE fiscal_registers
			SET fr_serial_normalized = NULLIF(UPPER(REPLACE(TRIM(COALESCE(fr_serial_number, '')), ' ', '')), '')
		`).Error; err != nil {
			return fmt.Errorf("не удалось нормализовать серийные номера ФР: %w", err)
		}

		// Удаляем неактуальные дубли по нормализованному серийнику.
		// Приоритет оставшейся записи: fn_expire_date DESC, updated_at DESC, created_at DESC.
		if err := tx.Exec(`
			WITH ranked AS (
				SELECT
					id,
					ROW_NUMBER() OVER (
						PARTITION BY fr_serial_normalized
						ORDER BY fn_expire_date DESC NULLS LAST, updated_at DESC, created_at DESC, id DESC
					) AS rn
				FROM fiscal_registers
				WHERE fr_serial_normalized IS NOT NULL
				  AND fr_serial_normalized <> ''
				  AND deleted_at IS NULL
			)
			DELETE FROM fiscal_registers f
			USING ranked r
			WHERE f.id = r.id
			  AND r.rn > 1
		`).Error; err != nil {
			return fmt.Errorf("не удалось удалить дубли ФР: %w", err)
		}

		if err := tx.Exec(`
			CREATE UNIQUE INDEX IF NOT EXISTS idx_fiscal_registers_fr_serial_normalized
			ON fiscal_registers (fr_serial_normalized)
			WHERE fr_serial_normalized IS NOT NULL AND fr_serial_normalized <> ''
		`).Error; err != nil {
			return fmt.Errorf("не удалось создать уникальный индекс серийника ФР: %w", err)
		}

		return nil
	})
}
