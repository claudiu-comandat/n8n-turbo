package twilio

import (
	"fmt"
	"strings"
)

type TwiML struct {
	verbs []TwiMLVerb
}

type TwiMLVerb interface {
	toXML() string
}

type SayVerb struct {
	Text     string
	Voice    string
	Language string
	Loop     int
}

type PlayVerb struct {
	URL  string
	Loop int
}

type GatherVerb struct {
	Action      string
	Method      string
	NumDigits   int
	Timeout     int
	FinishOnKey string
	Nested      []TwiMLVerb
}

type HangupVerb struct{}

type RedirectVerb struct {
	URL string
}

func (v SayVerb) toXML() string {
	attrs := ""
	if v.Voice != "" {
		attrs += fmt.Sprintf(` voice="%s"`, xmlEscape(v.Voice))
	}
	if v.Language != "" {
		attrs += fmt.Sprintf(` language="%s"`, xmlEscape(v.Language))
	}
	if v.Loop > 0 {
		attrs += fmt.Sprintf(` loop="%d"`, v.Loop)
	}
	return fmt.Sprintf("<Say%s>%s</Say>", attrs, xmlEscape(v.Text))
}

func (v PlayVerb) toXML() string {
	attrs := ""
	if v.Loop > 0 {
		attrs = fmt.Sprintf(` loop="%d"`, v.Loop)
	}
	return fmt.Sprintf("<Play%s>%s</Play>", attrs, xmlEscape(v.URL))
}

func (v GatherVerb) toXML() string {
	attrs := ""
	if v.Action != "" {
		attrs += fmt.Sprintf(` action="%s"`, xmlEscape(v.Action))
	}
	if v.Method != "" {
		attrs += fmt.Sprintf(` method="%s"`, xmlEscape(v.Method))
	}
	if v.NumDigits > 0 {
		attrs += fmt.Sprintf(` numDigits="%d"`, v.NumDigits)
	}
	if v.Timeout > 0 {
		attrs += fmt.Sprintf(` timeout="%d"`, v.Timeout)
	}
	if v.FinishOnKey != "" {
		attrs += fmt.Sprintf(` finishOnKey="%s"`, xmlEscape(v.FinishOnKey))
	}
	nested := strings.Builder{}
	for _, verb := range v.Nested {
		nested.WriteString(verb.toXML())
	}
	return fmt.Sprintf("<Gather%s>%s</Gather>", attrs, nested.String())
}

func (HangupVerb) toXML() string {
	return "<Hangup/>"
}

func (v RedirectVerb) toXML() string {
	return fmt.Sprintf("<Redirect>%s</Redirect>", xmlEscape(v.URL))
}

func (t *TwiML) Say(text string, opts ...SayVerb) *TwiML {
	verb := SayVerb{Text: text, Voice: "alice"}
	if len(opts) > 0 {
		verb = opts[0]
		verb.Text = text
	}
	t.verbs = append(t.verbs, verb)
	return t
}

func (t *TwiML) Play(url string, opts ...PlayVerb) *TwiML {
	verb := PlayVerb{URL: url}
	if len(opts) > 0 {
		verb = opts[0]
		verb.URL = url
	}
	t.verbs = append(t.verbs, verb)
	return t
}

func (t *TwiML) Gather(verb GatherVerb) *TwiML {
	t.verbs = append(t.verbs, verb)
	return t
}

func (t *TwiML) Hangup() *TwiML {
	t.verbs = append(t.verbs, HangupVerb{})
	return t
}

func (t *TwiML) Redirect(url string) *TwiML {
	t.verbs = append(t.verbs, RedirectVerb{URL: url})
	return t
}

func (t *TwiML) Build() string {
	builder := strings.Builder{}
	builder.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	builder.WriteString("<Response>")
	for _, verb := range t.verbs {
		builder.WriteString(verb.toXML())
	}
	builder.WriteString("</Response>")
	return builder.String()
}

func xmlEscape(value string) string {
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, "<", "&lt;")
	value = strings.ReplaceAll(value, ">", "&gt;")
	value = strings.ReplaceAll(value, `"`, "&quot;")
	value = strings.ReplaceAll(value, "'", "&apos;")
	return value
}
