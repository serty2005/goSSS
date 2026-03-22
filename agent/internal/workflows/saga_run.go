package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"etalon-agent/internal/protocol"
	"etalon-agent/internal/saga"
	sagahandlers "etalon-agent/internal/saga/handlers"
	"etalon-agent/internal/updater"
)

type SelfUpdateDownloader interface {
	Download(context.Context, string, string) (string, error)
}

type SagaRunWorkflowOptions struct {
	CurrentVersion  string
	SelfUpdater     SelfUpdateDownloader
	AdapterRunner   adapterRunner
	DataDir         string
	Debug           bool
	Infof           func(string, ...any)
	Debugf          func(string, ...any)
	CommandRunner   sagahandlers.CommandRunner
	VerifySHA256    func(string, string) error
	ApplyAndRestart func(string, []string) error
}

type SagaRunWorkflow struct {
	service *saga.Service
}

func NewSagaRunWorkflow(options SagaRunWorkflowOptions) (*SagaRunWorkflow, error) {
	infof := options.Infof
	if infof == nil {
		infof = log.Printf
	}

	definitions := saga.NewDefinitionRegistry()
	if err := definitions.Register(saga.NewAgentSelfUpdateDefinition()); err != nil {
		return nil, err
	}

	stepRegistry := saga.NewStepRegistry()
	nativeSelfUpdate := agentSelfUpdateCapability{
		currentVersion:  strings.TrimSpace(options.CurrentVersion),
		downloader:      options.SelfUpdater,
		verifySHA256:    options.VerifySHA256,
		applyAndRestart: options.ApplyAndRestart,
	}
	for _, registration := range []struct {
		stepType string
		handler  saga.StepHandler
	}{
		{stepType: "runner.self_update_preflight", handler: sagahandlers.NewSelfUpdatePreflightHandler()},
		{stepType: "runner.self_update_target_version_check", handler: sagahandlers.NewSelfUpdateTargetVersionCheckHandler(nativeSelfUpdate)},
		{stepType: "runner.self_update_download_metadata_check", handler: sagahandlers.NewSelfUpdateDownloadMetadataCheckHandler()},
		{stepType: "native.agent_self_update", handler: sagahandlers.NewNativeAgentSelfUpdateHandler(nativeSelfUpdate)},
		{stepType: "adapter.run", handler: sagahandlers.NewAdapterRunHandler(options.AdapterRunner)},
		{stepType: "external.command_run", handler: sagahandlers.NewExternalCommandHandler(options.CommandRunner)},
	} {
		if err := stepRegistry.Register(registration.stepType, registration.handler); err != nil {
			return nil, err
		}
	}

	engine, err := saga.NewEngine(saga.EngineOptions{
		Store:    saga.NewFileStore(filepath.Join(strings.TrimSpace(options.DataDir), "saga-state")),
		Handlers: stepRegistry,
		Debug:    options.Debug,
		Infof:    infof,
		Debugf:   options.Debugf,
	})
	if err != nil {
		return nil, err
	}

	service, err := saga.NewService(saga.ServiceOptions{
		Definitions: definitions,
		Engine:      engine,
	})
	if err != nil {
		return nil, err
	}

	return &SagaRunWorkflow{service: service}, nil
}

func (w *SagaRunWorkflow) Type() string {
	return "saga_run"
}

func (w *SagaRunWorkflow) Run(ctx context.Context, payload []byte) (protocol.TaskExecutionResult, error) {
	if w.service == nil {
		err := fmt.Errorf("service saga не настроен")
		return protocol.TaskExecutionResult{
			Status: "failed",
			Error:  err.Error(),
		}, err
	}

	var task protocol.SagaRunTaskPayload
	if err := json.Unmarshal(payload, &task); err != nil {
		parseErr := fmt.Errorf("невалидный payload saga_run: %w", err)
		return protocol.TaskExecutionResult{
			Status: "failed",
			Error:  parseErr.Error(),
		}, parseErr
	}
	return w.RunTask(ctx, task)
}

func (w *SagaRunWorkflow) RunTask(ctx context.Context, task protocol.SagaRunTaskPayload) (protocol.TaskExecutionResult, error) {
	if w.service == nil {
		err := fmt.Errorf("service saga не настроен")
		return protocol.TaskExecutionResult{
			Status: "failed",
			Error:  err.Error(),
		}, err
	}
	return w.service.Execute(ctx, task)
}

type agentSelfUpdateCapability struct {
	currentVersion  string
	downloader      SelfUpdateDownloader
	verifySHA256    func(string, string) error
	applyAndRestart func(string, []string) error
}

func (c agentSelfUpdateCapability) CurrentVersion() string {
	return strings.TrimSpace(c.currentVersion)
}

func (c agentSelfUpdateCapability) Download(ctx context.Context, url, fileName string) (string, error) {
	if c.downloader == nil {
		return "", fmt.Errorf("native downloader self_update не настроен")
	}
	return c.downloader.Download(ctx, url, fileName)
}

func (c agentSelfUpdateCapability) VerifySHA256(filePath, expected string) error {
	if c.verifySHA256 != nil {
		return c.verifySHA256(filePath, expected)
	}
	return updater.VerifySHA256(filePath, expected)
}

func (c agentSelfUpdateCapability) ApplyAndRestart(filePath string, args []string) error {
	if c.applyAndRestart != nil {
		return c.applyAndRestart(filePath, args)
	}
	return updater.ApplyAndRestart(filePath, args)
}
