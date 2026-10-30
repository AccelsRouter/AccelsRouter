package larknotify

import (
	"fmt"
	"strings"
)

// Translator renders one message of the backend's i18n files (i18n/locales).
// The cards are notifications with no web console in between, so their text is
// translated by the backend, in the language of DEFAULT_LANGUAGE.
type Translator func(key string, params map[string]any) string

// Message keys. They are declared again in i18n/keys.go and written in
// i18n/locales/*.yaml.
const (
	MsgAlertTitle         = "lark.alert_title"
	MsgAlertFieldChannel  = "lark.alert_field_channel"
	MsgAlertFieldModel    = "lark.alert_field_model"
	MsgAlertFieldFailures = "lark.alert_field_failures"
	MsgAlertFailuresValue = "lark.alert_failures_value"
	MsgAlertFieldStatus   = "lark.alert_field_status"
	MsgAlertFieldCallers  = "lark.alert_field_callers"
	MsgAlertCallerToken   = "lark.alert_caller_token"
	MsgAlertCallerGroup   = "lark.alert_caller_group"
	MsgAlertUnknownUser   = "lark.alert_unknown_user"
	MsgAlertMoreCallers   = "lark.alert_more_callers"
	MsgAlertFieldError    = "lark.alert_field_error"
	MsgAlertFieldRequest  = "lark.alert_field_request"
	MsgAlertNote          = "lark.alert_note"

	MsgTestTitle             = "lark.test_title"
	MsgTestFieldPurpose      = "lark.test_field_purpose"
	MsgTestFieldNote         = "lark.test_field_note"
	MsgTestNoteText          = "lark.test_note_text"
	MsgTestPurposeEnterprise = "lark.test_purpose_enterprise"
	MsgTestPurposeAlert      = "lark.test_purpose_alert"
)

// AlertCard is the message sent when an upstream channel keeps failing for a
// model. It names the channel, the model and who was calling.
func AlertCard(a Alert, tr Translator) Card {
	var callers []string
	for _, c := range a.Callers {
		name := strings.TrimSpace(c.Username)
		if name == "" {
			name = tr(MsgAlertUnknownUser, nil)
		}
		if c.UserID > 0 {
			name = fmt.Sprintf("%s (ID %d)", name, c.UserID)
		}
		parts := []string{name}
		if c.TokenName != "" {
			parts = append(parts, tr(MsgAlertCallerToken, map[string]any{"Name": c.TokenName}))
		}
		if c.Group != "" {
			parts = append(parts, tr(MsgAlertCallerGroup, map[string]any{"Name": c.Group}))
		}
		callers = append(callers, strings.Join(parts, " · "))
	}
	callerText := strings.Join(callers, "\n")
	if extra := a.TotalCallers - len(a.Callers); extra > 0 {
		callerText += "\n" + tr(MsgAlertMoreCallers, map[string]any{"Count": extra})
	}

	channel := fmt.Sprintf("#%d %s", a.Channel.ID, a.Channel.Name)
	if a.Channel.Type != "" {
		channel += " (" + a.Channel.Type + ")"
	}
	status := fmt.Sprintf("%d", a.Latest.StatusCode)
	if a.Latest.ErrorCode != "" {
		status += " · " + a.Latest.ErrorCode
	}

	minutes := int(a.Window.Minutes())
	all := []Field{
		{tr(MsgAlertFieldChannel, nil), channel},
		{tr(MsgAlertFieldModel, nil), a.Model},
		{tr(MsgAlertFieldFailures, nil), tr(MsgAlertFailuresValue, map[string]any{"Count": a.Count, "Minutes": minutes})},
		{tr(MsgAlertFieldStatus, nil), status},
		{tr(MsgAlertFieldCallers, nil), callerText},
		{tr(MsgAlertFieldError, nil), Truncate(a.Latest.Message, 300)},
		{tr(MsgAlertFieldRequest, nil), a.Latest.RequestID},
	}
	var fields []Field
	for _, f := range all {
		if strings.TrimSpace(f.Value) != "" {
			fields = append(fields, f)
		}
	}
	return Card{
		Title:  tr(MsgAlertTitle, nil),
		Color:  "red",
		Fields: fields,
		Note: tr(MsgAlertNote, map[string]any{
			"Time":    a.LastAt.Format("2006-01-02 15:04:05 MST"),
			"Minutes": int(a.Mute.Minutes()),
		}),
	}
}

// TestCard confirms to an administrator that the bot is wired up. purposeKey is
// MsgTestPurposeEnterprise or MsgTestPurposeAlert.
func TestCard(purposeKey string, tr Translator) Card {
	return Card{
		Title: tr(MsgTestTitle, nil),
		Color: "green",
		Fields: []Field{
			{tr(MsgTestFieldPurpose, nil), tr(purposeKey, nil)},
			{tr(MsgTestFieldNote, nil), tr(MsgTestNoteText, nil)},
		},
	}
}
