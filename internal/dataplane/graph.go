package dataplane

import "sort"

type Graph struct {
	workflow Workflow
	nodes    map[string]int
	inverted InvertedConnections
}

func NewGraph(workflow Workflow) *Graph {
	nodes := make(map[string]int, len(workflow.Nodes))
	for index := range workflow.Nodes {
		nodes[workflow.Nodes[index].Name] = index
	}
	return &Graph{
		workflow: workflow,
		nodes:    nodes,
		inverted: InvertConnections(workflow.Connections),
	}
}

func (g *Graph) Node(name string) (*Node, bool) {
	index, ok := g.nodes[name]
	if !ok {
		return nil, false
	}
	return &g.workflow.Nodes[index], true
}

func (g *Graph) Parents(name, connectionType string) []string {
	byType, ok := g.inverted[name]
	if !ok {
		return nil
	}
	inputs := byType[connectionType]
	seen := make(map[string]bool, len(inputs))
	parents := make([]string, 0, len(inputs))
	for _, edges := range inputs {
		for _, edge := range edges {
			if seen[edge.Node] {
				continue
			}
			seen[edge.Node] = true
			parents = append(parents, edge.Node)
		}
	}
	return parents
}

func (g *Graph) Children(name, connectionType string) []string {
	byType, ok := g.workflow.Connections[name]
	if !ok {
		return nil
	}
	outputs := byType[connectionType]
	seen := make(map[string]bool, len(outputs))
	children := make([]string, 0, len(outputs))
	for _, edges := range outputs {
		for _, edge := range edges {
			if seen[edge.Node] {
				continue
			}
			seen[edge.Node] = true
			children = append(children, edge.Node)
		}
	}
	return children
}

func (g *Graph) Ancestors(name string) map[string]bool {
	visited := map[string]bool{name: true}
	queue := []string{name}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, parent := range g.Parents(current, "main") {
			if visited[parent] {
				continue
			}
			visited[parent] = true
			queue = append(queue, parent)
		}
	}
	return visited
}

func (g *Graph) InputCount(name, connectionType string) int {
	byType, ok := g.inverted[name]
	if !ok {
		return 0
	}
	return len(byType[connectionType])
}

func (g *Graph) InputEdges(name, connectionType string) [][]InverseConnection {
	byType, ok := g.inverted[name]
	if !ok {
		return nil
	}
	edges := byType[connectionType]
	if len(edges) == 0 {
		return nil
	}
	result := make([][]InverseConnection, len(edges))
	for index := range edges {
		result[index] = append([]InverseConnection(nil), edges[index]...)
	}
	return result
}

func (g *Graph) InputTypes(name string) []string {
	byType, ok := g.inverted[name]
	if !ok {
		return nil
	}
	types := make([]string, 0, len(byType))
	for connectionType, edges := range byType {
		if len(edges) == 0 {
			continue
		}
		types = append(types, connectionType)
	}
	sort.Strings(types)
	return types
}

func (g *Graph) OutputEdges(name, connectionType string, outputIndex int) []Connection {
	byType, ok := g.workflow.Connections[name]
	if !ok {
		return nil
	}
	outputs := byType[connectionType]
	if outputIndex < 0 || outputIndex >= len(outputs) {
		return nil
	}
	return outputs[outputIndex]
}
