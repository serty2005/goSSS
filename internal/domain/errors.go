package domain

import "errors"

// Стражевые ошибки (sentinel errors) для доменного слоя.
var (
	// ErrNotFound указывает, что сущность не найдена.
	ErrNotFound = errors.New("entity not found")

	// ErrAlreadyExists указывает на конфликт уникальности (сущность уже существует).
	ErrAlreadyExists = errors.New("entity already exists")

	// ErrInternal указывает на общую внутреннюю ошибку.
	ErrInternal = errors.New("internal error")
)
