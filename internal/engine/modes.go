package engine

import (
	"fmt"
	"strings"
)

type ExecutionMode string

const (
	ExecutionModeManual      ExecutionMode = "manual"
	ExecutionModeTrigger     ExecutionMode = "trigger"
	ExecutionModeWebhook     ExecutionMode = "webhook"
	ExecutionModeForm        ExecutionMode = "form"
	ExecutionModeScheduled   ExecutionMode = "scheduled"
	ExecutionModeCLI         ExecutionMode = "cli"
	ExecutionModeIntegrated  ExecutionMode = "integrated"
	ExecutionModeError       ExecutionMode = "error"
	ExecutionModeInternal    ExecutionMode = "internal"
	ExecutionModeRetry       ExecutionMode = "retry"
	ExecutionModeWebhookTest ExecutionMode = "webhook-test"
	ExecutionModeFormTest    ExecutionMode = "form-test"
)

func NormalizeExecutionMode(mode string) ExecutionMode {
	value := ExecutionMode(strings.TrimSpace(strings.ToLower(mode)))
	if value == "" {
		return ExecutionModeManual
	}
	return value
}

func (m ExecutionMode) String() string {
	return string(m)
}

func IsProductionMode(mode ExecutionMode) bool {
	switch mode {
	case ExecutionModeTrigger, ExecutionModeWebhook, ExecutionModeForm, ExecutionModeScheduled, ExecutionModeCLI:
		return true
	default:
		return false
	}
}

func ShouldLaunchErrorWorkflow(mode ExecutionMode) bool {
	switch mode {
	case ExecutionModeManual, ExecutionModeError, ExecutionModeInternal, ExecutionModeIntegrated, ExecutionModeWebhookTest, ExecutionModeFormTest:
		return false
	default:
		return true
	}
}

func ShouldSaveExecution(mode ExecutionMode, settings map[string]any) bool {
	switch mode {
	case ExecutionModeInternal:
		return false
	case ExecutionModeManual:
		return boolSetting(settings, "saveManualExecutions", "saveDataManualExecutions")
	default:
		return true
	}
}

func boolSetting(settings map[string]any, keys ...string) bool {
	for _, key := range keys {
		value, ok := settings[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case bool:
			return typed
		case string:
			return strings.EqualFold(typed, "true") || typed == "1" || strings.EqualFold(typed, "all")
		case float64:
			return typed != 0
		case int:
			return typed != 0
		default:
			return fmt.Sprint(typed) == "true"
		}
	}
	return false
}
