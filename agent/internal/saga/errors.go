package saga

import (
	"encoding/json"
)

type StepExecutionError struct {
	Err    error
	Output json.RawMessage
}

func (e *StepExecutionError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *StepExecutionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}
