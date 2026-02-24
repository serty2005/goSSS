package saga

import (
	"context"
	"fmt"
	"log"
)

type Step struct {
	Name       string
	Do         func(context.Context) error
	Compensate func(context.Context)
}

type Runner struct{}

func NewRunner() *Runner {
	return &Runner{}
}

func (r *Runner) Run(ctx context.Context, sagaName string, steps []Step) error {
	done := make([]Step, 0, len(steps))
	for _, step := range steps {
		if err := step.Do(ctx); err != nil {
			for i := len(done) - 1; i >= 0; i-- {
				if done[i].Compensate != nil {
					done[i].Compensate(ctx)
				}
			}
			return fmt.Errorf("ошибка шага %s: %w", step.Name, err)
		}
		log.Printf("Сага %s: выполнен шаг %s", sagaName, step.Name)
		done = append(done, step)
	}
	return nil
}
