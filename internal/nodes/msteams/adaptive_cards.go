package msteams

import "encoding/json"

type AdaptiveCard struct {
	Schema  string        `json:"$schema"`
	Type    string        `json:"type"`
	Version string        `json:"version"`
	Body    []any         `json:"body"`
	Actions []any         `json:"actions,omitempty"`
	MSTeams *TeamsOptions `json:"msteams,omitempty"`
}

type TeamsOptions struct {
	Width string `json:"width,omitempty"`
}

type AdaptiveCardBuilder struct {
	card AdaptiveCard
}

func NewAdaptiveCard() *AdaptiveCardBuilder {
	return &AdaptiveCardBuilder{card: AdaptiveCard{Schema: "http://adaptivecards.io/schemas/adaptive-card.json", Type: "AdaptiveCard", Version: "1.5"}}
}

func (b *AdaptiveCardBuilder) AddTextBlock(text string, opts ...map[string]string) *AdaptiveCardBuilder {
	block := map[string]any{"type": "TextBlock", "text": text, "wrap": true}
	if len(opts) > 0 {
		for key, value := range opts[0] {
			block[key] = value
		}
	}
	b.card.Body = append(b.card.Body, block)
	return b
}

func (b *AdaptiveCardBuilder) AddFactSet(facts map[string]string) *AdaptiveCardBuilder {
	items := []map[string]string{}
	for title, value := range facts {
		items = append(items, map[string]string{"title": title, "value": value})
	}
	b.card.Body = append(b.card.Body, map[string]any{"type": "FactSet", "facts": items})
	return b
}

func (b *AdaptiveCardBuilder) AddButton(title string, url string) *AdaptiveCardBuilder {
	b.card.Actions = append(b.card.Actions, map[string]any{"type": "Action.OpenUrl", "title": title, "url": url})
	return b
}

func (b *AdaptiveCardBuilder) SetFullWidth() *AdaptiveCardBuilder {
	b.card.MSTeams = &TeamsOptions{Width: "Full"}
	return b
}

func (b *AdaptiveCardBuilder) Build() string {
	data, _ := json.Marshal(b.card)
	return string(data)
}
