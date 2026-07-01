package valueobject_test

import (
	"strings"
	"testing"

	"github.com/juantevez/odontoagenda/context/notifications/domain/valueobject"
)

// ── ParseChannel ──────────────────────────────────────────────────

func TestParseChannel_CanalesValidos(t *testing.T) {
	casos := []struct {
		input string
		want  valueobject.Channel
	}{
		{"WhatsApp", valueobject.ChannelWhatsApp},
		{"Email", valueobject.ChannelEmail},
		{"SMS", valueobject.ChannelSMS},
		{"Log", valueobject.ChannelLog},
	}
	for _, tc := range casos {
		got, err := valueobject.ParseChannel(tc.input)
		if err != nil {
			t.Errorf("ParseChannel(%q) error = %v", tc.input, err)
		}
		if got != tc.want {
			t.Errorf("ParseChannel(%q) = %q, quería %q", tc.input, got, tc.want)
		}
	}
}

func TestParseChannel_CanalInvalido_RetornaError(t *testing.T) {
	invalidos := []string{"whatsapp", "email", "sms", "Telegram", "Push", ""}
	for _, s := range invalidos {
		got, err := valueobject.ParseChannel(s)
		if err == nil {
			t.Errorf("ParseChannel(%q) error = nil, quería error", s)
		}
		if got != "" {
			t.Errorf("ParseChannel(%q) = %q, quería string vacío en error", s, got)
		}
	}
}

func TestParseChannel_ErrorMencionaElValor(t *testing.T) {
	_, err := valueobject.ParseChannel("Telegram")
	if err == nil {
		t.Fatal("error = nil")
	}
	const want = "Telegram"
	if got := err.Error(); !strings.Contains(got, want) {
		t.Errorf("error %q no menciona el valor %q", got, want)
	}
}

// ── Channel.String ────────────────────────────────────────────────

func TestChannel_String_RetornaStringSubyacente(t *testing.T) {
	casos := []struct {
		ch   valueobject.Channel
		want string
	}{
		{valueobject.ChannelWhatsApp, "WhatsApp"},
		{valueobject.ChannelEmail, "Email"},
		{valueobject.ChannelSMS, "SMS"},
		{valueobject.ChannelLog, "Log"},
	}
	for _, tc := range casos {
		if got := tc.ch.String(); got != tc.want {
			t.Errorf("Channel(%q).String() = %q, quería %q", tc.ch, got, tc.want)
		}
	}
}

// ── Channel constantes ────────────────────────────────────────────

func TestChannel_ValoresDeConstantes(t *testing.T) {
	if valueobject.ChannelWhatsApp != "WhatsApp" {
		t.Errorf("ChannelWhatsApp = %q", valueobject.ChannelWhatsApp)
	}
	if valueobject.ChannelEmail != "Email" {
		t.Errorf("ChannelEmail = %q", valueobject.ChannelEmail)
	}
	if valueobject.ChannelSMS != "SMS" {
		t.Errorf("ChannelSMS = %q", valueobject.ChannelSMS)
	}
	if valueobject.ChannelLog != "Log" {
		t.Errorf("ChannelLog = %q", valueobject.ChannelLog)
	}
}

// ── NotificationType.String ───────────────────────────────────────

func TestNotificationType_String_RetornaStringSubyacente(t *testing.T) {
	casos := []struct {
		nt   valueobject.NotificationType
		want string
	}{
		{valueobject.TypeAppointmentBooked, "appointment_booked"},
		{valueobject.TypeAppointmentConfirmed, "appointment_confirmed"},
		{valueobject.TypeAppointmentCancelled, "appointment_cancelled"},
		{valueobject.TypeAppointmentReminder, "appointment_reminder"},
		{valueobject.TypeAppointmentCompleted, "appointment_completed"},
		{valueobject.TypeAppointmentNoShow, "appointment_no_show"},
		{valueobject.TypePatientCheckedIn, "patient_checked_in"},
		{valueobject.TypePatientWelcome, "patient_welcome"},
		{valueobject.TypeLicenseExpiringSoon, "license_expiring_soon"},
		{valueobject.TypeAccountSuspended, "account_suspended"},
	}
	for _, tc := range casos {
		if got := tc.nt.String(); got != tc.want {
			t.Errorf("NotificationType(%q).String() = %q, quería %q", tc.nt, got, tc.want)
		}
	}
}

// ── NotificationStatus constantes ────────────────────────────────

func TestNotificationStatus_ValoresDeConstantes(t *testing.T) {
	if valueobject.StatusSent != "sent" {
		t.Errorf("StatusSent = %q", valueobject.StatusSent)
	}
	if valueobject.StatusFailed != "failed" {
		t.Errorf("StatusFailed = %q", valueobject.StatusFailed)
	}
	if valueobject.StatusSkipped != "skipped" {
		t.Errorf("StatusSkipped = %q", valueobject.StatusSkipped)
	}
}

