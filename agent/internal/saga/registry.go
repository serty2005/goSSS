package saga

import (
	"context"
	"fmt"
	"strings"

	"etalon-agent/internal/protocol"
)

type Definition interface {
	Type() string
	BuildPlan(protocol.SagaRunTaskPayload) (Request, error)
}

type StepHandler interface {
	Execute(context.Context, *Execution, Step) (StepOutcome, error)
}

type StepHandlerFunc func(context.Context, *Execution, Step) (StepOutcome, error)

func (f StepHandlerFunc) Execute(ctx context.Context, execution *Execution, step Step) (StepOutcome, error) {
	return f(ctx, execution, step)
}

type DefinitionRegistry struct {
	definitions map[string]Definition
}

func NewDefinitionRegistry() *DefinitionRegistry {
	return &DefinitionRegistry{definitions: make(map[string]Definition)}
}

func (r *DefinitionRegistry) Register(definition Definition) error {
	if definition == nil {
		return fmt.Errorf("definition saga не задан")
	}
	key := strings.TrimSpace(definition.Type())
	if key == "" {
		return fmt.Errorf("definition saga вернул пустой type")
	}
	r.definitions[key] = definition
	return nil
}

func (r *DefinitionRegistry) Resolve(sagaType string) (Definition, bool) {
	if r == nil {
		return nil, false
	}
	definition, ok := r.definitions[strings.TrimSpace(sagaType)]
	return definition, ok
}

type StepRegistry struct {
	handlers map[string]StepHandler
}

func NewStepRegistry() *StepRegistry {
	return &StepRegistry{handlers: make(map[string]StepHandler)}
}

func (r *StepRegistry) Register(stepType string, handler StepHandler) error {
	key := strings.TrimSpace(stepType)
	switch {
	case key == "":
		return fmt.Errorf("step handler type обязателен")
	case handler == nil:
		return fmt.Errorf("step handler %s не задан", key)
	default:
		r.handlers[key] = handler
		return nil
	}
}

func (r *StepRegistry) Resolve(stepType string) (StepHandler, bool) {
	if r == nil {
		return nil, false
	}
	handler, ok := r.handlers[strings.TrimSpace(stepType)]
	return handler, ok
}
