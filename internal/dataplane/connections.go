package dataplane

import "encoding/json"

func (c *Connections) UnmarshalJSON(data []byte) error {
	raw := map[string]map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	result := make(Connections, len(raw))
	for source, byType := range raw {
		result[source] = make(map[string][][]Connection, len(byType))
		for connectionType, payload := range byType {
			normalized, err := normalizeConnectionPayload(payload)
			if err != nil {
				return err
			}
			result[source][connectionType] = normalized
		}
	}
	*c = result
	return nil
}

func normalizeConnectionPayload(payload json.RawMessage) ([][]Connection, error) {
	var byOutput [][]Connection
	if err := json.Unmarshal(payload, &byOutput); err == nil {
		return byOutput, nil
	}
	var flat []Connection
	if err := json.Unmarshal(payload, &flat); err == nil {
		return [][]Connection{flat}, nil
	}
	return nil, json.Unmarshal(payload, &byOutput)
}

func InvertConnections(connections Connections) InvertedConnections {
	inverted := make(InvertedConnections)
	for source, byType := range connections {
		for connectionType, outputs := range byType {
			for outputIndex, edges := range outputs {
				for _, edge := range edges {
					if inverted[edge.Node] == nil {
						inverted[edge.Node] = make(map[string][][]InverseConnection)
					}
					for len(inverted[edge.Node][connectionType]) <= edge.Index {
						inverted[edge.Node][connectionType] = append(inverted[edge.Node][connectionType], nil)
					}
					inverted[edge.Node][connectionType][edge.Index] = append(inverted[edge.Node][connectionType][edge.Index], InverseConnection{
						Node:      source,
						OutputIdx: outputIndex,
					})
				}
			}
		}
	}
	return inverted
}

func NodeByName(workflow Workflow, name string) (Node, bool) {
	for _, node := range workflow.Nodes {
		if node.Name == name {
			return node, true
		}
	}
	return Node{}, false
}

func StartNodes(workflow Workflow) []Node {
	inverted := InvertConnections(workflow.Connections)
	nodes := make([]Node, 0, len(workflow.Nodes))
	for _, node := range workflow.Nodes {
		if node.Disabled {
			continue
		}
		if len(inverted[node.Name]["main"]) == 0 || isTriggerNode(node.Type) {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

func isTriggerNode(nodeType string) bool {
	switch nodeType {
	case "n8n-nodes-base.manualTrigger", "n8n-nodes-base.start", "n8n-nodes-base.webhook", "n8n-nodes-base.scheduleTrigger", "n8n-nodes-base.executeWorkflowTrigger", "n8n-nodes-base.errorTrigger", "n8n-nodes-base.formTrigger":
		return true
	default:
		return false
	}
}
