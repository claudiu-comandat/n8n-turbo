package discord

import "fmt"

const NodeType = "n8n-nodes-base.discord"

type Error struct {
	Code    int            `json:"code"`
	Message string         `json:"message"`
	Errors  map[string]any `json:"errors,omitempty"`
}

func (e *Error) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("discord API error %d", e.Code)
	}
	return fmt.Sprintf("discord API error %d: %s", e.Code, e.Message)
}

type Embed struct {
	Title       string         `json:"title,omitempty"`
	Type        string         `json:"type,omitempty"`
	Description string         `json:"description,omitempty"`
	URL         string         `json:"url,omitempty"`
	Timestamp   string         `json:"timestamp,omitempty"`
	Color       int            `json:"color,omitempty"`
	Footer      *EmbedFooter   `json:"footer,omitempty"`
	Image       *EmbedMedia    `json:"image,omitempty"`
	Thumbnail   *EmbedMedia    `json:"thumbnail,omitempty"`
	Video       *EmbedMedia    `json:"video,omitempty"`
	Provider    *EmbedProvider `json:"provider,omitempty"`
	Author      *EmbedAuthor   `json:"author,omitempty"`
	Fields      []EmbedField   `json:"fields,omitempty"`
}

type EmbedFooter struct {
	Text         string `json:"text"`
	IconURL      string `json:"icon_url,omitempty"`
	ProxyIconURL string `json:"proxy_icon_url,omitempty"`
}

type EmbedMedia struct {
	URL      string `json:"url"`
	ProxyURL string `json:"proxy_url,omitempty"`
	Height   int    `json:"height,omitempty"`
	Width    int    `json:"width,omitempty"`
}

type EmbedProvider struct {
	Name string `json:"name,omitempty"`
	URL  string `json:"url,omitempty"`
}

type EmbedAuthor struct {
	Name         string `json:"name"`
	URL          string `json:"url,omitempty"`
	IconURL      string `json:"icon_url,omitempty"`
	ProxyIconURL string `json:"proxy_icon_url,omitempty"`
}

type EmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

type AllowedMentions struct {
	Parse       []string `json:"parse,omitempty"`
	Roles       []string `json:"roles,omitempty"`
	Users       []string `json:"users,omitempty"`
	RepliedUser bool     `json:"replied_user,omitempty"`
}

type MessageReference struct {
	MessageID string `json:"message_id"`
	ChannelID string `json:"channel_id,omitempty"`
	GuildID   string `json:"guild_id,omitempty"`
}
