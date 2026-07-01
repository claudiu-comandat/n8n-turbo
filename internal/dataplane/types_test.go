package dataplane

import (
	"encoding/json"
	"testing"
)

func TestNodeMarshalPreservesExplicitFalseBooleans(t *testing.T) {
	var node Node
	input := []byte(`{
		"name": "AI Keyword Generator",
		"type": "@n8n/n8n-nodes-langchain.agent",
		"parameters": {},
		"disabled": false,
		"continueOnFail": false,
		"alwaysOutputData": false,
		"executeOnce": false,
		"retryOnFail": false,
		"useExponentialBackoff": false,
		"notesInFlow": false
	}`)
	if err := json.Unmarshal(input, &node); err != nil {
		t.Fatalf("unmarshal node: %v", err)
	}

	data, err := json.Marshal(node)
	if err != nil {
		t.Fatalf("marshal node: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}

	for _, key := range []string{
		"disabled",
		"continueOnFail",
		"alwaysOutputData",
		"executeOnce",
		"retryOnFail",
		"useExponentialBackoff",
		"notesInFlow",
	} {
		value, ok := got[key]
		if !ok {
			t.Fatalf("%s missing from marshaled node: %s", key, string(data))
		}
		if value != false {
			t.Fatalf("%s = %v, want false", key, value)
		}
	}
}

func TestNodeMarshalOmitsAbsentFalseBooleans(t *testing.T) {
	node := Node{
		Name:       "Set",
		Type:       "n8n-nodes-base.set",
		Parameters: map[string]any{},
	}

	data, err := json.Marshal(node)
	if err != nil {
		t.Fatalf("marshal node: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}

	if _, ok := got["executeOnce"]; ok {
		t.Fatalf("executeOnce should be omitted when it was never explicit: %s", string(data))
	}
	if _, ok := got["notesInFlow"]; ok {
		t.Fatalf("notesInFlow should be omitted when it was never explicit: %s", string(data))
	}
}

func TestWorkflowMarshalPreservesExplicitEmptyObjects(t *testing.T) {
	var workflow Workflow
	input := []byte(`{
		"name": "Order Insert",
		"active": false,
		"nodes": [],
		"connections": {},
		"settings": {},
		"pinData": {},
		"staticData": {},
		"meta": {}
	}`)
	if err := json.Unmarshal(input, &workflow); err != nil {
		t.Fatalf("unmarshal workflow: %v", err)
	}

	data, err := json.Marshal(workflow)
	if err != nil {
		t.Fatalf("marshal workflow: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}

	for _, key := range []string{"settings", "pinData", "staticData", "meta"} {
		value, ok := got[key].(map[string]any)
		if !ok {
			t.Fatalf("%s missing or not an object from marshaled workflow: %s", key, string(data))
		}
		if len(value) != 0 {
			t.Fatalf("%s = %v, want empty object", key, value)
		}
	}
}

func TestWorkflowPreserveFieldsKeepsEmptyObjectAfterRehydrate(t *testing.T) {
	workflow := Workflow{
		Name:        "Order Insert",
		Active:      false,
		Nodes:       []Node{},
		Connections: Connections{},
		PinData:     map[string][]Item{},
	}
	workflow.PreserveFields("pinData")

	data, err := json.Marshal(workflow)
	if err != nil {
		t.Fatalf("marshal workflow: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if value, ok := got["pinData"].(map[string]any); !ok || len(value) != 0 {
		t.Fatalf("pinData = %v, want empty object in %s", got["pinData"], string(data))
	}
}
