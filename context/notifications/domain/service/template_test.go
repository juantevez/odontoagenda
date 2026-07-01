package service_test

import (
	"strings"
	"testing"
	"time"

	"github.com/juantevez/odontoagenda/context/notifications/domain/service"
	"github.com/juantevez/odontoagenda/context/notifications/domain/valueobject"
)

// ── helpers ───────────────────────────────────────────────────────

var svc = service.NewTemplateService()

// datos base reutilizables en todos los tests
func baseData() service.TemplateData {
	start := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	return service.TemplateData{
		PatientName:      "Ana García",
		PatientPhone:     "+5491112345678",
		PatientEmail:     "ana@example.com",
		ProfessionalName: "Dr. López",
		ProfessionalPhone: "+5491187654321",
		ProfessionalEmail: "lopez@example.com",
		AppointmentID:    "appt-abc-123",
		ProcedureCode:    "D0150",
		SlotStart:        start,
		SlotEnd:          start.Add(30 * time.Minute),
		LicenseNumber:    "MAT-456",
		SpecialtyCode:    "ORTODONCIA",
		ExpiresAt:        time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC),
		DaysRemaining:    15,
		UserEmail:        "user@example.com",
		SuspensionReason: "violación de términos",
		CancellationReason: "patient_request",
	}
}

// ── Render — dispatch ─────────────────────────────────────────────

func TestRender_TipoDesconocido_RetornaError(t *testing.T) {
	_, err := svc.Render("tipo_inventado", valueobject.ChannelWhatsApp, baseData())
	if err == nil {
		t.Fatal("Render() con tipo desconocido debe retornar error")
	}
	if !strings.Contains(err.Error(), "tipo_inventado") {
		t.Errorf("error = %q, debe mencionar el tipo desconocido", err.Error())
	}
}

func TestRender_TodosLosTiposConocidos_SinError(t *testing.T) {
	d := baseData()
	tipos := []valueobject.NotificationType{
		valueobject.TypeAppointmentBooked,
		valueobject.TypeAppointmentConfirmed,
		valueobject.TypeAppointmentCancelled,
		valueobject.TypeAppointmentCompleted,
		valueobject.TypeAppointmentReminder,
		valueobject.TypeAppointmentNoShow,
		valueobject.TypePatientWelcome,
		valueobject.TypeLicenseExpiringSoon,
		valueobject.TypeAccountSuspended,
	}
	for _, tipo := range tipos {
		ch := valueobject.ChannelWhatsApp
		if tipo == valueobject.TypeLicenseExpiringSoon || tipo == valueobject.TypeAccountSuspended {
			ch = valueobject.ChannelEmail
		}
		msg, err := svc.Render(tipo, ch, d)
		if err != nil {
			t.Errorf("Render(%q) error = %v", tipo, err)
		}
		if msg.Body == "" {
			t.Errorf("Render(%q) Body vacío", tipo)
		}
	}
}

// ── buildMessage — routing por canal ─────────────────────────────

func TestRender_ChannelEmail_DestinatarioEsEmail(t *testing.T) {
	d := baseData()
	msg, _ := svc.Render(valueobject.TypeAppointmentBooked, valueobject.ChannelEmail, d)

	if msg.To != d.PatientEmail {
		t.Errorf("To = %q, quería email del paciente %q", msg.To, d.PatientEmail)
	}
	if msg.Subject == "" {
		t.Error("Subject vacío para canal Email")
	}
}

func TestRender_ChannelWhatsApp_DestinatarioEsTelefono(t *testing.T) {
	d := baseData()
	msg, _ := svc.Render(valueobject.TypeAppointmentBooked, valueobject.ChannelWhatsApp, d)

	if msg.To != d.PatientPhone {
		t.Errorf("To = %q, quería teléfono del paciente %q", msg.To, d.PatientPhone)
	}
	if msg.Subject != "" {
		t.Errorf("Subject = %q, debe estar vacío para WhatsApp", msg.Subject)
	}
}

func TestRender_ChannelSMS_DestinatarioEsTelefono(t *testing.T) {
	d := baseData()
	msg, _ := svc.Render(valueobject.TypeAppointmentBooked, valueobject.ChannelSMS, d)

	if msg.To != d.PatientPhone {
		t.Errorf("To = %q, quería teléfono %q", msg.To, d.PatientPhone)
	}
	if msg.Subject != "" {
		t.Errorf("Subject = %q, debe estar vacío para SMS", msg.Subject)
	}
}

func TestRender_ChannelLog_DestinatarioEsTelefonoConSubject(t *testing.T) {
	d := baseData()
	msg, _ := svc.Render(valueobject.TypeAppointmentNoShow, valueobject.ChannelLog, d)

	if msg.To != d.PatientPhone {
		t.Errorf("To = %q, quería teléfono %q", msg.To, d.PatientPhone)
	}
	if msg.Subject == "" {
		t.Error("Subject vacío para ChannelLog — debe incluirlo")
	}
}

// ── appointmentBooked ─────────────────────────────────────────────

func TestRender_AppointmentBooked_BodyContieneSlotYProcedimiento(t *testing.T) {
	d := baseData()
	msg, _ := svc.Render(valueobject.TypeAppointmentBooked, valueobject.ChannelWhatsApp, d)

	for _, want := range []string{"15/03/2026", "10:00", "10:30", d.ProfessionalName, d.ProcedureCode} {
		if !strings.Contains(msg.Body, want) {
			t.Errorf("Body = %q, no contiene %q", msg.Body, want)
		}
	}
}

// ── appointmentConfirmed ──────────────────────────────────────────

func TestRender_AppointmentConfirmed_BodyContieneAutorizadoYSlot(t *testing.T) {
	d := baseData()
	msg, _ := svc.Render(valueobject.TypeAppointmentConfirmed, valueobject.ChannelWhatsApp, d)

	for _, want := range []string{"autorizado", "15/03/2026", d.ProfessionalName} {
		if !strings.Contains(msg.Body, want) {
			t.Errorf("Body no contiene %q", want)
		}
	}
}

// ── appointmentCancelled ──────────────────────────────────────────

func TestRender_AppointmentCancelled_BodyContieneMotivoYSlot(t *testing.T) {
	d := baseData()
	msg, _ := svc.Render(valueobject.TypeAppointmentCancelled, valueobject.ChannelEmail, d)

	for _, want := range []string{d.CancellationReason, d.ProfessionalName, "15/03/2026"} {
		if !strings.Contains(msg.Body, want) {
			t.Errorf("Body no contiene %q", want)
		}
	}
}

func TestRender_AppointmentCancelled_SinLateCancellation_SinAviso(t *testing.T) {
	d := baseData()
	d.IsLateCancellation = false
	msg, _ := svc.Render(valueobject.TypeAppointmentCancelled, valueobject.ChannelWhatsApp, d)

	if strings.Contains(msg.Body, "tardía") {
		t.Error("Body contiene aviso de cancelación tardía, pero IsLateCancellation=false")
	}
}

func TestRender_AppointmentCancelled_ConLateCancellation_ContieneAviso(t *testing.T) {
	d := baseData()
	d.IsLateCancellation = true
	msg, _ := svc.Render(valueobject.TypeAppointmentCancelled, valueobject.ChannelWhatsApp, d)

	if !strings.Contains(msg.Body, "tardía") {
		t.Error("Body no contiene aviso de cancelación tardía, pero IsLateCancellation=true")
	}
}

// ── appointmentCompleted ──────────────────────────────────────────

func TestRender_AppointmentCompleted_BodyContieneProcedimientoYProfesional(t *testing.T) {
	d := baseData()
	msg, _ := svc.Render(valueobject.TypeAppointmentCompleted, valueobject.ChannelWhatsApp, d)

	for _, want := range []string{d.ProcedureCode, d.ProfessionalName} {
		if !strings.Contains(msg.Body, want) {
			t.Errorf("Body no contiene %q", want)
		}
	}
}

// ── appointmentReminder ───────────────────────────────────────────

func TestRender_AppointmentReminder_BodyContieneRecordatorioYSlot(t *testing.T) {
	d := baseData()
	msg, _ := svc.Render(valueobject.TypeAppointmentReminder, valueobject.ChannelWhatsApp, d)

	for _, want := range []string{"ecordatorio", "15/03/2026", d.ProfessionalName} {
		if !strings.Contains(msg.Body, want) {
			t.Errorf("Body no contiene %q", want)
		}
	}
}

// ── appointmentNoShow ─────────────────────────────────────────────

func TestRender_AppointmentNoShow_BodyContieneMarkerYDatos(t *testing.T) {
	d := baseData()
	msg, _ := svc.Render(valueobject.TypeAppointmentNoShow, valueobject.ChannelLog, d)

	for _, want := range []string{"[NO-SHOW]", d.PatientName, d.ProfessionalName, d.AppointmentID} {
		if !strings.Contains(msg.Body, want) {
			t.Errorf("Body no contiene %q", want)
		}
	}
}

// ── patientWelcome ────────────────────────────────────────────────

func TestRender_PatientWelcome_BodyContieneNombreDePareja(t *testing.T) {
	d := baseData()
	d.PatientName = "Ana García"
	msg, _ := svc.Render(valueobject.TypePatientWelcome, valueobject.ChannelWhatsApp, d)

	// debe usar solo el primer nombre
	if !strings.Contains(msg.Body, "Ana") {
		t.Error("Body no contiene el primer nombre 'Ana'")
	}
	if strings.Contains(msg.Body, "García") {
		t.Error("Body contiene el apellido 'García' — debe usar solo el primer nombre")
	}
}

func TestRender_PatientWelcome_NombreSimple_SinCortar(t *testing.T) {
	d := baseData()
	d.PatientName = "Monique"
	msg, _ := svc.Render(valueobject.TypePatientWelcome, valueobject.ChannelEmail, d)

	if !strings.Contains(msg.Body, "Monique") {
		t.Error("Body no contiene el nombre de un solo token")
	}
}

func TestRender_PatientWelcome_NombreVacio_NoFalla(t *testing.T) {
	d := baseData()
	d.PatientName = ""
	_, err := svc.Render(valueobject.TypePatientWelcome, valueobject.ChannelWhatsApp, d)
	if err != nil {
		t.Errorf("Render con PatientName vacío error = %v, quería nil", err)
	}
}

// ── licenseExpiringSoon ───────────────────────────────────────────

func TestRender_LicenseExpiringSoon_UsaDestinatarioProfesional(t *testing.T) {
	d := baseData()
	msg, _ := svc.Render(valueobject.TypeLicenseExpiringSoon, valueobject.ChannelEmail, d)

	if msg.To != d.ProfessionalEmail {
		t.Errorf("To = %q, quería email del profesional %q", msg.To, d.ProfessionalEmail)
	}
}

func TestRender_LicenseExpiringSoon_BodyContieneMatriculaDiasYFecha(t *testing.T) {
	d := baseData()
	msg, _ := svc.Render(valueobject.TypeLicenseExpiringSoon, valueobject.ChannelEmail, d)

	for _, want := range []string{d.LicenseNumber, d.SpecialtyCode, "15 días", "30/06/2026"} {
		if !strings.Contains(msg.Body, want) {
			t.Errorf("Body no contiene %q", want)
		}
	}
}

func TestRender_LicenseExpiringSoon_FechaEnFormatoDDMMAAAA(t *testing.T) {
	d := baseData()
	d.ExpiresAt = time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	msg, _ := svc.Render(valueobject.TypeLicenseExpiringSoon, valueobject.ChannelEmail, d)

	if !strings.Contains(msg.Body, "05/01/2026") {
		t.Errorf("Body no contiene fecha en formato DD/MM/YYYY: %q", msg.Body)
	}
}

// ── accountSuspended ──────────────────────────────────────────────

func TestRender_AccountSuspended_DestinatarioEsUserEmail(t *testing.T) {
	d := baseData()
	msg, _ := svc.Render(valueobject.TypeAccountSuspended, valueobject.ChannelEmail, d)

	if msg.To != d.UserEmail {
		t.Errorf("To = %q, quería UserEmail %q", msg.To, d.UserEmail)
	}
}

func TestRender_AccountSuspended_BodyContieneMotivo(t *testing.T) {
	d := baseData()
	msg, _ := svc.Render(valueobject.TypeAccountSuspended, valueobject.ChannelEmail, d)

	if !strings.Contains(msg.Body, d.SuspensionReason) {
		t.Errorf("Body no contiene el motivo de suspensión %q", d.SuspensionReason)
	}
}

// ── formatSlot (vía Render) ───────────────────────────────────────

func TestRender_FormatoSlot_DDMMAAAAdeHHMMaHHMMhs(t *testing.T) {
	d := baseData()
	d.SlotStart = time.Date(2026, 7, 4, 9, 5, 0, 0, time.UTC)
	d.SlotEnd = d.SlotStart.Add(45 * time.Minute)

	msg, _ := svc.Render(valueobject.TypeAppointmentBooked, valueobject.ChannelWhatsApp, d)

	// Formato esperado: "04/07/2026 de 09:05 a 09:50 hs"
	want := "04/07/2026 de 09:05 a 09:50 hs"
	if !strings.Contains(msg.Body, want) {
		t.Errorf("Body no contiene el slot formateado %q\nBody: %q", want, msg.Body)
	}
}

func TestRender_FormatoSlot_MediodiaMidnight(t *testing.T) {
	d := baseData()
	d.SlotStart = time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	d.SlotEnd = d.SlotStart.Add(30 * time.Minute)

	msg, _ := svc.Render(valueobject.TypeAppointmentBooked, valueobject.ChannelWhatsApp, d)

	if !strings.Contains(msg.Body, "00:00") {
		t.Errorf("Body no contiene '00:00' para slot de medianoche: %q", msg.Body)
	}
}
