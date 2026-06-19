package graph

import "github.com/n8n-io/n8n-turbo/internal/dataplane"

type Graph = dataplane.Graph
type Connection = dataplane.Connection
type Connections = dataplane.Connections
type InvertedConnections = dataplane.InvertedConnections

func New(workflow dataplane.Workflow) *Graph {
	return dataplane.NewGraph(workflow)
}

func Invert(connections dataplane.Connections) dataplane.InvertedConnections {
	return dataplane.InvertConnections(connections)
}

func StartNodes(workflow dataplane.Workflow) []dataplane.Node {
	return dataplane.StartNodes(workflow)
}

func NodeByName(workflow dataplane.Workflow, name string) (dataplane.Node, bool) {
	return dataplane.NodeByName(workflow, name)
}
