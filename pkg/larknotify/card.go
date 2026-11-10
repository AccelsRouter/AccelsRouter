package larknotify

import "unicode/utf8"

// Field is one labelled value on a card.
type Field struct {
	Name  string
	Value string
}

// Card is the small subset of Lark interactive cards this project uses: a
// coloured title, labelled values and a footnote.
type Card struct {
	Title  string
	Color  string // header template: red, orange, yellow, green, blue, grey ...
	Fields []Field
	Note   string
}

// Truncate cuts s to at most max runes, adding an ellipsis when it cut.
func Truncate(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max]) + "…"
}

// Payload renders the card object of a Lark "interactive" message.
func (c Card) Payload() map[string]any {
	color := c.Color
	if color == "" {
		color = "blue"
	}
	parts := make([]any, 0, len(c.Fields))
	for _, f := range c.Fields {
		parts = append(parts, map[string]any{
			"is_short": utf8.RuneCountInString(f.Value) <= 36,
			"text": map[string]any{
				"tag":     "lark_md",
				"content": "**" + f.Name + "**\n" + Truncate(f.Value, 600),
			},
		})
	}
	elements := []any{}
	if len(parts) > 0 {
		elements = append(elements, map[string]any{"tag": "div", "fields": parts})
	}
	if c.Note != "" {
		elements = append(elements,
			map[string]any{"tag": "hr"},
			map[string]any{"tag": "note", "elements": []any{
				map[string]any{"tag": "plain_text", "content": c.Note},
			}})
	}
	return map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"template": color,
			"title":    map[string]any{"tag": "plain_text", "content": c.Title},
		},
		"elements": elements,
	}
}
