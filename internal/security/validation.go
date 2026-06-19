package security

import (
	"fmt"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

const MaxWorkflowNameLen = 128
const MaxNodesPerWorkflow = 1000
const MaxWebhookPathLen = 128

type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("validation error for field %s: %s", e.Field, e.Message)
}

func ValidateWorkflow(workflow dataplane.Workflow) error {
	if workflow.Name == "" {
		return ValidationError{Field: "name", Message: "name is required"}
	}
	if len(workflow.Name) > MaxWorkflowNameLen {
		return ValidationError{Field: "name", Message: fmt.Sprintf("name too long, max %d characters", MaxWorkflowNameLen)}
	}
	if len(workflow.Nodes) > MaxNodesPerWorkflow {
		return ValidationError{Field: "nodes", Message: fmt.Sprintf("too many nodes, max %d", MaxNodesPerWorkflow)}
	}
	seenNames := map[string]bool{}
	for _, node := range workflow.Nodes {
		if node.Name == "" {
			return ValidationError{Field: "nodes", Message: "node missing name"}
		}
		if node.Type == "" {
			return ValidationError{Field: "nodes", Message: "node missing type"}
		}
		if seenNames[node.Name] {
			return ValidationError{Field: "nodes", Message: "duplicate node name: " + node.Name}
		}
		seenNames[node.Name] = true
	}
	for fromNode, typedConnections := range workflow.Connections {
		if !seenNames[fromNode] {
			return ValidationError{Field: "connections", Message: "connection from unknown node: " + fromNode}
		}
		for connectionType, outputs := range typedConnections {
			for _, output := range outputs {
				for _, connection := range output {
					if !seenNames[connection.Node] {
						return ValidationError{Field: "connections", Message: fmt.Sprintf("connection to unknown node: %s from %s via %s", connection.Node, fromNode, connectionType)}
					}
				}
			}
		}
	}
	return nil
}
