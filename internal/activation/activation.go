package activation

import (
	"context"

	"github.com/n8n-io/n8n-turbo/internal/activemanager"
)

type Manager = activemanager.Manager
type WorkflowRepository = activemanager.WorkflowRepository
type TriggerActivator = activemanager.TriggerActivator
type RecoveryConfig = activemanager.RecoveryConfig
type RecoveryResult = activemanager.RecoveryResult
type WorkflowActivation = activemanager.WorkflowActivation
type ActiveWorkflows = activemanager.ActiveWorkflows
type StateChange = activemanager.StateChange

var ErrAlreadyActive = activemanager.ErrAlreadyActive
var ErrNotActive = activemanager.ErrNotActive

func NewManager(repo WorkflowRepository, webhooks TriggerActivator, cron TriggerActivator, polling TriggerActivator) *Manager {
	return activemanager.NewManager(repo, webhooks, cron, polling)
}

func Recover(ctx context.Context, manager *Manager, config RecoveryConfig) (RecoveryResult, error) {
	return manager.RecoverActiveWorkflows(ctx, config)
}
