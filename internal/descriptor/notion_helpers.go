package descriptor

import (
	"fmt"
	"strings"
	"time"
)

type NotionPropertyType string

const (
	NotionPropTitle       NotionPropertyType = "title"
	NotionPropRichText    NotionPropertyType = "rich_text"
	NotionPropNumber      NotionPropertyType = "number"
	NotionPropSelect      NotionPropertyType = "select"
	NotionPropMultiSelect NotionPropertyType = "multi_select"
	NotionPropDate        NotionPropertyType = "date"
	NotionPropPeople      NotionPropertyType = "people"
	NotionPropFiles       NotionPropertyType = "files"
	NotionPropCheckbox    NotionPropertyType = "checkbox"
	NotionPropURL         NotionPropertyType = "url"
	NotionPropEmail       NotionPropertyType = "email"
	NotionPropPhone       NotionPropertyType = "phone_number"
	NotionPropFormula     NotionPropertyType = "formula"
	NotionPropRelation    NotionPropertyType = "relation"
	NotionPropRollup      NotionPropertyType = "rollup"
	NotionPropCreatedTime NotionPropertyType = "created_time"
	NotionPropEditedTime  NotionPropertyType = "last_edited_time"
	NotionPropCreatedBy   NotionPropertyType = "created_by"
	NotionPropEditedBy    NotionPropertyType = "last_edited_by"
	NotionPropStatus      NotionPropertyType = "status"
	NotionPropUniqueID    NotionPropertyType = "unique_id"
)

type NotionPropertyExtractor struct{}

func (NotionPropertyExtractor) Extract(property map[string]any) any {
	propertyType := NotionPropertyType(stringFromAny(property["type"]))
	switch propertyType {
	case NotionPropTitle:
		return notionRichText(property["title"])
	case NotionPropRichText:
		return notionRichText(property["rich_text"])
	case NotionPropNumber:
		return property["number"]
	case NotionPropSelect:
		if selectValue, ok := mapFromAny(property["select"]); ok {
			return selectValue["name"]
		}
		return nil
	case NotionPropMultiSelect:
		return notionNamedList(property["multi_select"])
	case NotionPropDate:
		if date, ok := mapFromAny(property["date"]); ok {
			return date["start"]
		}
		return nil
	case NotionPropCheckbox:
		return property["checkbox"]
	case NotionPropURL:
		return property["url"]
	case NotionPropEmail:
		return property["email"]
	case NotionPropPhone:
		return property["phone_number"]
	case NotionPropStatus:
		if status, ok := mapFromAny(property["status"]); ok {
			return status["name"]
		}
		return nil
	case NotionPropUniqueID:
		if id, ok := mapFromAny(property["unique_id"]); ok {
			prefix := stringFromAny(id["prefix"])
			number := intFromAny(id["number"])
			if prefix != "" {
				return fmt.Sprintf("%s-%d", prefix, number)
			}
			return number
		}
		return nil
	case NotionPropFormula:
		if formula, ok := mapFromAny(property["formula"]); ok {
			formulaType := stringFromAny(formula["type"])
			return formula[formulaType]
		}
		return nil
	case NotionPropCreatedTime:
		return property["created_time"]
	case NotionPropEditedTime:
		return property["last_edited_time"]
	case NotionPropPeople:
		return notionPeople(property["people"])
	case NotionPropRelation:
		return notionRelations(property["relation"])
	default:
		return nil
	}
}

type NotionPropertyBuilder struct{}

func (NotionPropertyBuilder) Title(text string) map[string]any {
	return map[string]any{"title": []map[string]any{{"type": "text", "text": map[string]string{"content": text}}}}
}

func (NotionPropertyBuilder) RichText(text string) map[string]any {
	return map[string]any{"rich_text": []map[string]any{{"type": "text", "text": map[string]string{"content": text}}}}
}

func (NotionPropertyBuilder) Number(number float64) map[string]any {
	return map[string]any{"number": number}
}

func (NotionPropertyBuilder) Select(name string) map[string]any {
	return map[string]any{"select": map[string]string{"name": name}}
}

func (NotionPropertyBuilder) MultiSelect(names []string) map[string]any {
	options := make([]map[string]string, len(names))
	for index, name := range names {
		options[index] = map[string]string{"name": name}
	}
	return map[string]any{"multi_select": options}
}

func (NotionPropertyBuilder) Date(start time.Time, end *time.Time) map[string]any {
	date := map[string]any{"start": start.Format(time.RFC3339)}
	if end != nil {
		date["end"] = end.Format(time.RFC3339)
	}
	return map[string]any{"date": date}
}

func (NotionPropertyBuilder) Checkbox(checked bool) map[string]any {
	return map[string]any{"checkbox": checked}
}

func (NotionPropertyBuilder) URL(value string) map[string]any {
	return map[string]any{"url": value}
}

func (NotionPropertyBuilder) Email(value string) map[string]any {
	return map[string]any{"email": value}
}

func (NotionPropertyBuilder) Relation(pageIDs []string) map[string]any {
	relations := make([]map[string]string, len(pageIDs))
	for index, id := range pageIDs {
		relations[index] = map[string]string{"id": id}
	}
	return map[string]any{"relation": relations}
}

type NotionPageFlattener struct {
	extractor NotionPropertyExtractor
}

func NewNotionPageFlattener() NotionPageFlattener {
	return NotionPageFlattener{extractor: NotionPropertyExtractor{}}
}

func (f NotionPageFlattener) Flatten(page map[string]any) map[string]any {
	result := map[string]any{
		"id":               page["id"],
		"url":              page["url"],
		"created_time":     page["created_time"],
		"last_edited_time": page["last_edited_time"],
		"archived":         page["archived"],
	}
	if icon, ok := mapFromAny(page["icon"]); ok {
		if emoji := stringFromAny(icon["emoji"]); emoji != "" {
			result["icon"] = emoji
		}
	}
	if properties, ok := mapFromAny(page["properties"]); ok {
		for name, raw := range properties {
			if property, ok := mapFromAny(raw); ok {
				result[name] = f.extractor.Extract(property)
			}
		}
	}
	return result
}

func (f NotionPageFlattener) FlattenAll(pages []any) []map[string]any {
	result := make([]map[string]any, 0, len(pages))
	for _, page := range pages {
		if object, ok := mapFromAny(page); ok {
			result = append(result, f.Flatten(object))
		}
	}
	return result
}

type NotionBlockBuilder struct{}

func (NotionBlockBuilder) Paragraph(text string) map[string]any {
	return notionTextBlock("paragraph", text)
}

func (NotionBlockBuilder) Heading1(text string) map[string]any {
	return notionTextBlock("heading_1", text)
}

func (NotionBlockBuilder) BulletedListItem(text string) map[string]any {
	return notionTextBlock("bulleted_list_item", text)
}

func (NotionBlockBuilder) TaskItem(text string, checked bool) map[string]any {
	block := notionTextBlock("to_do", text)
	block["to_do"].(map[string]any)["checked"] = checked
	return block
}

func (NotionBlockBuilder) Divider() map[string]any {
	return map[string]any{"object": "block", "type": "divider", "divider": map[string]any{}}
}

func notionTextBlock(blockType string, text string) map[string]any {
	return map[string]any{
		"object": "block",
		"type":   blockType,
		blockType: map[string]any{
			"rich_text": []map[string]any{{"type": "text", "text": map[string]string{"content": text}}},
		},
	}
}

func notionRichText(raw any) string {
	parts := []string{}
	for _, entry := range sliceFromAny(raw) {
		if text, ok := mapFromAny(entry); ok {
			value := stringFromAny(text["plain_text"])
			if value == "" {
				if content, ok := mapFromAny(text["text"]); ok {
					value = stringFromAny(content["content"])
				}
			}
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, "")
}

func notionNamedList(raw any) []string {
	values := sliceFromAny(raw)
	result := make([]string, 0, len(values))
	for _, entry := range values {
		if object, ok := mapFromAny(entry); ok {
			if name := stringFromAny(object["name"]); name != "" {
				result = append(result, name)
			}
		}
	}
	return result
}

func notionPeople(raw any) []map[string]any {
	values := sliceFromAny(raw)
	result := make([]map[string]any, 0, len(values))
	for _, entry := range values {
		person, ok := mapFromAny(entry)
		if !ok {
			continue
		}
		simplified := map[string]any{"id": person["id"], "name": person["name"], "type": person["type"]}
		if details, ok := mapFromAny(person["person"]); ok {
			simplified["email"] = details["email"]
		}
		result = append(result, simplified)
	}
	return result
}

func notionRelations(raw any) []string {
	values := sliceFromAny(raw)
	result := make([]string, 0, len(values))
	for _, entry := range values {
		if relation, ok := mapFromAny(entry); ok {
			if id := stringFromAny(relation["id"]); id != "" {
				result = append(result, id)
			}
		}
	}
	return result
}
