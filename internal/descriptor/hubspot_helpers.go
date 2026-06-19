package descriptor

import (
	"encoding/json"
	"fmt"
)

type HubSpotFilter struct {
	PropertyName string   `json:"propertyName"`
	Operator     string   `json:"operator"`
	Value        any      `json:"value,omitempty"`
	Values       []string `json:"values,omitempty"`
	HighValue    string   `json:"highValue,omitempty"`
}

type HubSpotFilterGroup struct {
	Filters []HubSpotFilter `json:"filters"`
}

type HubSpotSort struct {
	PropertyName string `json:"propertyName"`
	Direction    string `json:"direction"`
}

type HubSpotSearchBuilder struct {
	filterGroups []HubSpotFilterGroup
	sorts        []HubSpotSort
	properties   []string
	limit        int
	after        string
}

type HubSpotContactProperties struct {
	Email          string `json:"email,omitempty"`
	FirstName      string `json:"firstname,omitempty"`
	LastName       string `json:"lastname,omitempty"`
	Phone          string `json:"phone,omitempty"`
	Company        string `json:"company,omitempty"`
	Website        string `json:"website,omitempty"`
	JobTitle       string `json:"jobtitle,omitempty"`
	Address        string `json:"address,omitempty"`
	City           string `json:"city,omitempty"`
	Country        string `json:"country,omitempty"`
	LifecycleStage string `json:"lifecyclestage,omitempty"`
	LeadStatus     string `json:"hs_lead_status,omitempty"`
}

type HubSpotDealProperties struct {
	DealName    string `json:"dealname,omitempty"`
	Amount      string `json:"amount,omitempty"`
	DealStage   string `json:"dealstage,omitempty"`
	Pipeline    string `json:"pipeline,omitempty"`
	CloseDate   string `json:"closedate,omitempty"`
	OwnerID     string `json:"hubspot_owner_id,omitempty"`
	Description string `json:"description,omitempty"`
}

func NewHubSpotSearchBuilder() *HubSpotSearchBuilder {
	return &HubSpotSearchBuilder{limit: 100}
}

func (b *HubSpotSearchBuilder) FilterEquals(property string, value any) *HubSpotSearchBuilder {
	b.filterGroups = append(b.filterGroups, HubSpotFilterGroup{Filters: []HubSpotFilter{{PropertyName: property, Operator: "EQ", Value: fmt.Sprint(value)}}})
	return b
}

func (b *HubSpotSearchBuilder) FilterContains(property string, token string) *HubSpotSearchBuilder {
	b.filterGroups = append(b.filterGroups, HubSpotFilterGroup{Filters: []HubSpotFilter{{PropertyName: property, Operator: "CONTAINS_TOKEN", Value: token}}})
	return b
}

func (b *HubSpotSearchBuilder) FilterHasProperty(property string) *HubSpotSearchBuilder {
	b.filterGroups = append(b.filterGroups, HubSpotFilterGroup{Filters: []HubSpotFilter{{PropertyName: property, Operator: "HAS_PROPERTY"}}})
	return b
}

func (b *HubSpotSearchBuilder) SortBy(property string, direction string) *HubSpotSearchBuilder {
	b.sorts = append(b.sorts, HubSpotSort{PropertyName: property, Direction: direction})
	return b
}

func (b *HubSpotSearchBuilder) Properties(properties ...string) *HubSpotSearchBuilder {
	b.properties = append([]string(nil), properties...)
	return b
}

func (b *HubSpotSearchBuilder) Limit(limit int) *HubSpotSearchBuilder {
	if limit > 0 {
		b.limit = limit
	}
	return b
}

func (b *HubSpotSearchBuilder) After(after string) *HubSpotSearchBuilder {
	b.after = after
	return b
}

func (b *HubSpotSearchBuilder) Build() map[string]any {
	body := map[string]any{"limit": b.limit}
	if len(b.filterGroups) > 0 {
		body["filterGroups"] = b.filterGroups
	}
	if len(b.sorts) > 0 {
		body["sorts"] = b.sorts
	}
	if len(b.properties) > 0 {
		body["properties"] = b.properties
	}
	if b.after != "" {
		body["after"] = b.after
	}
	return body
}

func HubSpotBatchInputs(items []any, batchSize int) [][]any {
	if batchSize <= 0 || batchSize > 100 {
		batchSize = 100
	}
	var batches [][]any
	for start := 0; start < len(items); start += batchSize {
		end := start + batchSize
		if end > len(items) {
			end = len(items)
		}
		batches = append(batches, items[start:end])
	}
	return batches
}

func HubSpotPropertiesFromStruct(value any) map[string]any {
	raw, _ := jsonMarshalMap(value)
	return raw
}

func jsonMarshalMap(value any) (map[string]any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	result := map[string]any{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}
