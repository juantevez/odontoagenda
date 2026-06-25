// Package aggregate contiene el Aggregate Root del bounded context Professional.
//
// Aggregate Root: Professional
//
// Entidades internas:
//   - ProfessionalLicense — matrícula por especialidad con estado de habilitación
//   - ClinicAssignment    — asignación a una sede con horarios y duraciones por procedimiento
//
// Invariantes clave:
//   - Un profesional solo puede atender si tiene al menos una License activa.
//   - No puede tener dos Licenses activas para la misma especialidad.
//   - No puede tener dos ClinicAssignments activas para la misma sede.
//   - Los horarios de distintas sedes no pueden solaparse (validado por Domain Service).
//   - Solo puede asignarse a una sede para una especialidad con License activa para esa especialidad.
package aggregate

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/professional/domain/event"
	"github.com/juantevez/odontoagenda/context/professional/domain/valueobject"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

// ── Professional — Aggregate Root ────────────────────────────────

type Professional struct {
	id     sharedtypes.ProfessionalID
	userID *uuid.UUID // vínculo con IAM

	// Información personal
	fullName   sharedvo.FullName
	nationalID sharedvo.NationalID
	email      sharedvo.Email
	phone      sharedvo.PhoneNumber
	bio        string // descripción profesional para la UI

	status valueobject.ProfessionalStatus

	// Entidades internas
	licenses          []ProfessionalLicense
	clinicAssignments []ClinicAssignment

	// Duraciones por procedimiento a nivel profesional (override del catálogo).
	// Se propagan a cada ClinicAssignment como base, pero pueden sobreescribirse por sede.
	defaultDurations map[string]valueobject.ProcedureDuration // ProcedureCode → duration

	createdAt time.Time
	updatedAt time.Time
	createdBy *uuid.UUID
	version   int64

	pendingEvents []event.DomainEvent
}

// ── ProfessionalLicense — Entity ──────────────────────────────────

// ProfessionalLicense representa la matrícula habilitante de un profesional
// para una especialidad específica.
//
// Invariantes:
//   - No pueden existir dos Licenses activas para la misma Specialty.
//   - Una License vencida no habilita al profesional para atender esa especialidad.
//   - Solo el staff admin puede crear, actualizar o revocar licencias.
type ProfessionalLicense struct {
	id            uuid.UUID
	specialty     valueobject.Specialty
	licenseNumber string // número de matrícula emitido por el ente regulador
	issuingBody   string // "Colegio de Odontólogos de CABA", etc.
	issuedAt      time.Time
	expiresAt     *time.Time // nil = sin vencimiento
	status        valueobject.LicenseStatus
	documentRef   string // referencia al documento escaneado (S3 key, etc.)
	createdAt     time.Time
	updatedAt     time.Time
}

// IsCurrentlyValid reporta si la matrícula está activa y no vencida.
func (l ProfessionalLicense) IsCurrentlyValid() bool {
	if l.status != valueobject.LicenseStatusActive {
		return false
	}
	if l.expiresAt != nil && time.Now().UTC().After(*l.expiresAt) {
		return false
	}
	return true
}

func (l ProfessionalLicense) ID() uuid.UUID                     { return l.id }
func (l ProfessionalLicense) Specialty() valueobject.Specialty  { return l.specialty }
func (l ProfessionalLicense) LicenseNumber() string             { return l.licenseNumber }
func (l ProfessionalLicense) IssuingBody() string               { return l.issuingBody }
func (l ProfessionalLicense) IssuedAt() time.Time               { return l.issuedAt }
func (l ProfessionalLicense) ExpiresAt() *time.Time             { return l.expiresAt }
func (l ProfessionalLicense) Status() valueobject.LicenseStatus { return l.status }
func (l ProfessionalLicense) DocumentRef() string               { return l.documentRef }

// ── ClinicAssignment — Entity ─────────────────────────────────────

// ClinicAssignment representa la asignación de un profesional a una sede específica.
// Contiene el horario recurrente, excepciones y las duraciones de procedimientos
// para esa sede en particular.
//
// Invariantes:
//   - No pueden existir dos ClinicAssignments activas para la misma ClinicID.
//   - Los horarios entre distintas sedes no pueden solaparse (validado por Domain Service).
//   - AssignedSpecialties deben tener License activa en el Professional.
type ClinicAssignment struct {
	id       uuid.UUID
	clinicID sharedtypes.ClinicID
	status   valueobject.AssignmentStatus

	// Especialidades que practica en esta sede específica
	// (subconjunto de las especialidades con License activa del Professional).
	assignedSpecialties []valueobject.SpecialtyCode

	// Horario recurrente: uno por día de la semana.
	weeklySchedule []valueobject.DaySchedule

	// Días de excepción: vacaciones, feriados, horarios especiales.
	exceptionDays []valueobject.ExceptionDay

	// Duraciones de procedimientos específicas para esta sede.
	// Sobreescribe los defaultDurations del Professional para esta sede.
	// Si un procedimiento no está aquí, se usa el defaultDuration del Professional.
	procedureDurations map[string]valueobject.ProcedureDuration // ProcedureCode → duration

	assignedFrom  time.Time
	assignedUntil *time.Time // nil = sin fecha de fin

	assignedBy uuid.UUID
	createdAt  time.Time
	updatedAt  time.Time
}

// IsAvailableAt verifica si el profesional atiende en esta sede
// en el momento dado (considera horario recurrente y excepciones).
func (ca *ClinicAssignment) IsAvailableAt(t time.Time) bool {
	if !ca.status.IsActive() {
		return false
	}
	if t.Before(ca.assignedFrom) {
		return false
	}
	if ca.assignedUntil != nil && t.After(*ca.assignedUntil) {
		return false
	}

	// Verificar excepciones primero (tienen prioridad sobre el horario recurrente).
	for _, exc := range ca.exceptionDays {
		if exc.MatchesDate(t) {
			if !exc.IsWorking {
				return false
			}
			if exc.Schedule != nil {
				return exc.Schedule.ContainsTime(t)
			}
		}
	}

	// Sin excepción: usar horario recurrente.
	for _, day := range ca.weeklySchedule {
		if day.ContainsTime(t) {
			return true
		}
	}
	return false
}

// GetDurationForProcedure retorna la duración para un procedimiento en esta sede.
// Retorna (duration, true) si existe override; (zero, false) si debe usarse el default.
func (ca *ClinicAssignment) GetDurationForProcedure(procedureCode string) (valueobject.ProcedureDuration, bool) {
	d, ok := ca.procedureDurations[procedureCode]
	return d, ok
}

// SetDurationForProcedure establece la duración de un procedimiento en esta sede.
func (ca *ClinicAssignment) SetDurationForProcedure(d valueobject.ProcedureDuration) {
	if ca.procedureDurations == nil {
		ca.procedureDurations = make(map[string]valueobject.ProcedureDuration)
	}
	ca.procedureDurations[d.ProcedureCode] = d
	ca.updatedAt = time.Now().UTC()
}

// AddException agrega un día de excepción al horario de esta sede.
func (ca *ClinicAssignment) AddException(exc valueobject.ExceptionDay) error {
	// Verificar que no haya excepción para esa fecha.
	for _, existing := range ca.exceptionDays {
		if existing.MatchesDate(exc.Date) {
			return sharederrors.NewAlreadyExists("ExceptionDay",
				"date", exc.Date.Format("2006-01-02"))
		}
	}
	ca.exceptionDays = append(ca.exceptionDays, exc)
	ca.updatedAt = time.Now().UTC()
	return nil
}

// RemoveException elimina la excepción de una fecha específica.
func (ca *ClinicAssignment) RemoveException(date time.Time) error {
	for i, exc := range ca.exceptionDays {
		if exc.MatchesDate(date) {
			ca.exceptionDays = append(ca.exceptionDays[:i], ca.exceptionDays[i+1:]...)
			ca.updatedAt = time.Now().UTC()
			return nil
		}
	}
	return sharederrors.NewNotFound("ExceptionDay", date.Format("2006-01-02"))
}

// UpdateWeeklySchedule reemplaza el horario recurrente.
func (ca *ClinicAssignment) UpdateWeeklySchedule(schedule []valueobject.DaySchedule) error {
	if len(schedule) == 0 {
		return sharederrors.NewInvalidArgument("weekly_schedule",
			"debe tener al menos un día de atención")
	}
	// Verificar que no haya días duplicados en el nuevo horario.
	seen := make(map[valueobject.Weekday]bool)
	for _, d := range schedule {
		if seen[d.Weekday] {
			return sharederrors.NewInvalidArgument("weekly_schedule",
				fmt.Sprintf("día duplicado: %s", d.Weekday))
		}
		seen[d.Weekday] = true
	}
	ca.weeklySchedule = schedule
	ca.updatedAt = time.Now().UTC()
	return nil
}

// End cierra la asignación a una sede a partir de una fecha dada.
func (ca *ClinicAssignment) End(until time.Time, by uuid.UUID) error {
	if !ca.status.IsActive() {
		return sharederrors.NewPrecondition("assignment_active",
			"solo se puede finalizar una asignación activa")
	}
	if until.Before(time.Now().UTC()) {
		return sharederrors.NewInvalidArgument("until",
			"la fecha de fin no puede ser pasada")
	}
	ca.assignedUntil = &until
	ca.status = valueobject.AssignmentStatusEnded
	ca.updatedAt = time.Now().UTC()
	return nil
}

// Getters de ClinicAssignment
func (ca *ClinicAssignment) ID() uuid.UUID                        { return ca.id }
func (ca *ClinicAssignment) ClinicID() sharedtypes.ClinicID       { return ca.clinicID }
func (ca *ClinicAssignment) Status() valueobject.AssignmentStatus { return ca.status }
func (ca *ClinicAssignment) AssignedSpecialties() []valueobject.SpecialtyCode {
	return ca.assignedSpecialties
}
func (ca *ClinicAssignment) WeeklySchedule() []valueobject.DaySchedule { return ca.weeklySchedule }
func (ca *ClinicAssignment) ExceptionDays() []valueobject.ExceptionDay { return ca.exceptionDays }
func (ca *ClinicAssignment) ProcedureDurations() map[string]valueobject.ProcedureDuration {
	return ca.procedureDurations
}
func (ca *ClinicAssignment) AssignedFrom() time.Time   { return ca.assignedFrom }
func (ca *ClinicAssignment) AssignedUntil() *time.Time { return ca.assignedUntil }
func (ca *ClinicAssignment) AssignedBy() uuid.UUID     { return ca.assignedBy }
func (ca *ClinicAssignment) CreatedAt() time.Time      { return ca.createdAt }
func (ca *ClinicAssignment) UpdatedAt() time.Time      { return ca.updatedAt }

// ── Constructor ───────────────────────────────────────────────────

// NewProfessional crea un Professional nuevo.
func NewProfessional(
	userID *uuid.UUID,
	fullName sharedvo.FullName,
	nationalID sharedvo.NationalID,
	email sharedvo.Email,
	phone sharedvo.PhoneNumber,
	bio string,
	createdBy *uuid.UUID,
) *Professional {
	id := sharedtypes.NewID()
	now := time.Now().UTC()

	p := &Professional{
		id:                id,
		userID:            userID,
		fullName:          fullName,
		nationalID:        nationalID,
		email:             email,
		phone:             phone,
		bio:               strings.TrimSpace(bio),
		status:            valueobject.ProfessionalStatusActive,
		licenses:          []ProfessionalLicense{},
		clinicAssignments: []ClinicAssignment{},
		defaultDurations:  make(map[string]valueobject.ProcedureDuration),
		createdAt:         now,
		updatedAt:         now,
		createdBy:         createdBy,
		version:           1,
	}

	p.pendingEvents = append(p.pendingEvents, event.ProfessionalRegistered{
		ProfessionalID: id,
		FullName:       fullName.String(),
		OccurredAt:     now,
	})

	return p
}

// Reconstitute reconstruye el aggregate desde persistencia sin disparar eventos.
func Reconstitute(
	id sharedtypes.ProfessionalID,
	userID *uuid.UUID,
	fullName sharedvo.FullName,
	nationalID sharedvo.NationalID,
	email sharedvo.Email,
	phone sharedvo.PhoneNumber,
	bio string,
	status valueobject.ProfessionalStatus,
	licenses []ProfessionalLicense,
	assignments []ClinicAssignment,
	defaultDurations map[string]valueobject.ProcedureDuration,
	createdAt, updatedAt time.Time,
	createdBy *uuid.UUID,
	version int64,
) *Professional {
	return &Professional{
		id:                id,
		userID:            userID,
		fullName:          fullName,
		nationalID:        nationalID,
		email:             email,
		phone:             phone,
		bio:               bio,
		status:            status,
		licenses:          licenses,
		clinicAssignments: assignments,
		defaultDurations:  defaultDurations,
		createdAt:         createdAt,
		updatedAt:         updatedAt,
		createdBy:         createdBy,
		version:           version,
		pendingEvents:     []event.DomainEvent{},
	}
}

// ── Comportamiento: Licenses ──────────────────────────────────────

// AddLicense agrega una nueva matrícula profesional.
//
// Invariante: no pueden existir dos Licenses activas para la misma Specialty.
func (p *Professional) AddLicense(
	specialty valueobject.Specialty,
	licenseNumber, issuingBody string,
	issuedAt time.Time,
	expiresAt *time.Time,
	documentRef string,
) error {
	// Verificar unicidad de specialty activa.
	for _, l := range p.licenses {
		if l.specialty.Equals(specialty) && l.IsCurrentlyValid() {
			return sharederrors.NewPrecondition("duplicate_active_license",
				fmt.Sprintf("ya existe una matrícula activa para la especialidad '%s'", specialty.Code))
		}
	}

	if strings.TrimSpace(licenseNumber) == "" {
		return sharederrors.NewInvalidArgument("license_number", "requerido")
	}
	if strings.TrimSpace(issuingBody) == "" {
		return sharederrors.NewInvalidArgument("issuing_body", "requerido")
	}

	now := time.Now().UTC()
	license := ProfessionalLicense{
		id:            uuid.New(),
		specialty:     specialty,
		licenseNumber: strings.TrimSpace(licenseNumber),
		issuingBody:   strings.TrimSpace(issuingBody),
		issuedAt:      issuedAt,
		expiresAt:     expiresAt,
		status:        valueobject.LicenseStatusActive,
		documentRef:   documentRef,
		createdAt:     now,
		updatedAt:     now,
	}

	p.licenses = append(p.licenses, license)
	p.updatedAt = now

	p.pendingEvents = append(p.pendingEvents, event.ProfessionalLicenseAdded{
		ProfessionalID: p.id,
		LicenseID:      license.id,
		SpecialtyCode:  string(specialty.Code),
		LicenseNumber:  licenseNumber,
		ExpiresAt:      expiresAt,
		OccurredAt:     now,
	})

	return nil
}

// ReconstituteLicense agrega una matrícula ya existente desde la BD, sin disparar eventos.
func (p *Professional) ReconstituteLicense(
	id uuid.UUID,
	specialty valueobject.Specialty,
	licenseNumber string,
	issuingBody string,
	issuedAt time.Time,
	expiresAt *time.Time,
	status valueobject.LicenseStatus,
	documentRef string,
) {
	now := time.Now().UTC()
	p.licenses = append(p.licenses, ProfessionalLicense{
		id:            id,
		specialty:     specialty,
		licenseNumber: licenseNumber,
		issuingBody:   issuingBody,
		issuedAt:      issuedAt,
		expiresAt:     expiresAt,
		status:        status,
		documentRef:   documentRef,
		createdAt:     now,
		updatedAt:     now,
	})
}

// RevokeLicense revoca una matrícula por su ID (ej: sanción del ente regulador).
func (p *Professional) RevokeLicense(licenseID uuid.UUID, reason string, by uuid.UUID) error {
	for i := range p.licenses {
		if p.licenses[i].id == licenseID {
			if p.licenses[i].status == valueobject.LicenseStatusRevoked {
				return sharederrors.NewPrecondition("already_revoked",
					"la matrícula ya está revocada")
			}
			p.licenses[i].status = valueobject.LicenseStatusRevoked
			p.licenses[i].updatedAt = time.Now().UTC()
			p.updatedAt = time.Now().UTC()
			return nil
		}
	}
	return sharederrors.NewNotFound("ProfessionalLicense", licenseID.String())
}

// HasValidLicenseFor verifica si el profesional tiene matrícula activa para una especialidad.
func (p *Professional) HasValidLicenseFor(code valueobject.SpecialtyCode) bool {
	for _, l := range p.licenses {
		if l.specialty.Code == code && l.IsCurrentlyValid() {
			return true
		}
	}
	return false
}

// ActiveSpecialties retorna los códigos de especialidad con licencia activa.
func (p *Professional) ActiveSpecialties() []valueobject.SpecialtyCode {
	codes := make([]valueobject.SpecialtyCode, 0)
	seen := make(map[valueobject.SpecialtyCode]bool)
	for _, l := range p.licenses {
		if l.IsCurrentlyValid() && !seen[l.specialty.Code] {
			codes = append(codes, l.specialty.Code)
			seen[l.specialty.Code] = true
		}
	}
	return codes
}

// ── Comportamiento: ClinicAssignments ────────────────────────────

// AssignToClinic asigna el profesional a una sede con horario recurrente.
//
// Pre-condición: el Domain Service ScheduleConflictChecker debe haber validado
// que no hay solapamiento de horarios con otras sedes antes de llamar este método.
//
// Invariante: no puede haber dos asignaciones activas para la misma ClinicID.
// Invariante: las especialidades asignadas deben tener License activa.
func (p *Professional) AssignToClinic(
	clinicID sharedtypes.ClinicID,
	specialties []valueobject.SpecialtyCode,
	weeklySchedule []valueobject.DaySchedule,
	assignedFrom time.Time,
	assignedBy uuid.UUID,
) error {
	if !p.status.IsActive() {
		return sharederrors.NewPrecondition("professional_active",
			"el profesional debe estar activo para asignarse a una sede")
	}

	// Verificar unicidad de sede activa.
	for _, a := range p.clinicAssignments {
		if a.clinicID == clinicID && a.status.IsActive() {
			return sharederrors.NewPrecondition("duplicate_clinic_assignment",
				fmt.Sprintf("ya existe una asignación activa para la sede '%s'", clinicID))
		}
	}

	// Verificar que cada especialidad asignada tenga License activa.
	if len(specialties) == 0 {
		return sharederrors.NewInvalidArgument("specialties",
			"debe asignar al menos una especialidad")
	}
	for _, code := range specialties {
		if !p.HasValidLicenseFor(code) {
			return sharederrors.NewPrecondition("valid_license_required",
				fmt.Sprintf("el profesional no tiene matrícula activa para '%s'", code))
		}
	}

	if len(weeklySchedule) == 0 {
		return sharederrors.NewInvalidArgument("weekly_schedule",
			"debe definir al menos un día de atención")
	}

	now := time.Now().UTC()
	assignment := ClinicAssignment{
		id:                  uuid.New(),
		clinicID:            clinicID,
		status:              valueobject.AssignmentStatusActive,
		assignedSpecialties: specialties,
		weeklySchedule:      weeklySchedule,
		exceptionDays:       []valueobject.ExceptionDay{},
		procedureDurations:  make(map[string]valueobject.ProcedureDuration),
		assignedFrom:        assignedFrom,
		assignedBy:          assignedBy,
		createdAt:           now,
		updatedAt:           now,
	}

	// Propagar defaultDurations del profesional a la nueva asignación.
	for code, dur := range p.defaultDurations {
		assignment.procedureDurations[code] = dur
	}

	p.clinicAssignments = append(p.clinicAssignments, assignment)
	p.updatedAt = now

	p.pendingEvents = append(p.pendingEvents, event.ProfessionalAssignedToClinic{
		ProfessionalID: p.id,
		AssignmentID:   assignment.id,
		ClinicID:       clinicID,
		Specialties:    specialties,
		AssignedFrom:   assignedFrom,
		OccurredAt:     now,
	})

	return nil
}

// EndClinicAssignment finaliza la asignación a una sede.
func (p *Professional) EndClinicAssignment(assignmentID uuid.UUID, until time.Time, by uuid.UUID) error {
	for i := range p.clinicAssignments {
		if p.clinicAssignments[i].id == assignmentID {
			if err := p.clinicAssignments[i].End(until, by); err != nil {
				return err
			}
			p.updatedAt = time.Now().UTC()

			p.pendingEvents = append(p.pendingEvents, event.ProfessionalScheduleUpdated{
				ProfessionalID: p.id,
				ClinicID:       p.clinicAssignments[i].clinicID,
				OccurredAt:     time.Now().UTC(),
			})
			return nil
		}
	}
	return sharederrors.NewNotFound("ClinicAssignment", assignmentID.String())
}

// FindAssignmentForClinic retorna la asignación activa de una sede, si existe.
func (p *Professional) FindAssignmentForClinic(clinicID sharedtypes.ClinicID) (*ClinicAssignment, bool) {
	for i := range p.clinicAssignments {
		if p.clinicAssignments[i].clinicID == clinicID &&
			p.clinicAssignments[i].status.IsActive() {
			return &p.clinicAssignments[i], true
		}
	}
	return nil, false
}

// AddExceptionToClinic agrega un día de excepción para una sede específica.
func (p *Professional) AddExceptionToClinic(
	assignmentID uuid.UUID,
	exc valueobject.ExceptionDay,
) error {
	for i := range p.clinicAssignments {
		if p.clinicAssignments[i].id == assignmentID {
			if err := p.clinicAssignments[i].AddException(exc); err != nil {
				return err
			}
			p.updatedAt = time.Now().UTC()

			p.pendingEvents = append(p.pendingEvents, event.ProfessionalScheduleUpdated{
				ProfessionalID: p.id,
				ClinicID:       p.clinicAssignments[i].clinicID,
				OccurredAt:     time.Now().UTC(),
			})
			return nil
		}
	}
	return sharederrors.NewNotFound("ClinicAssignment", assignmentID.String())
}

// UpdateClinicSchedule reemplaza el horario recurrente de una sede.
func (p *Professional) UpdateClinicSchedule(
	assignmentID uuid.UUID,
	newSchedule []valueobject.DaySchedule,
) error {
	for i := range p.clinicAssignments {
		if p.clinicAssignments[i].id == assignmentID {
			if err := p.clinicAssignments[i].UpdateWeeklySchedule(newSchedule); err != nil {
				return err
			}
			p.updatedAt = time.Now().UTC()

			p.pendingEvents = append(p.pendingEvents, event.ProfessionalScheduleUpdated{
				ProfessionalID: p.id,
				ClinicID:       p.clinicAssignments[i].clinicID,
				OccurredAt:     time.Now().UTC(),
			})
			return nil
		}
	}
	return sharederrors.NewNotFound("ClinicAssignment", assignmentID.String())
}

// ── Comportamiento: Default Durations ────────────────────────────

// SetDefaultDuration establece la duración por defecto de un procedimiento
// para este profesional (aplica a todas sus sedes como base).
func (p *Professional) SetDefaultDuration(d valueobject.ProcedureDuration) {
	p.defaultDurations[d.ProcedureCode] = d
	p.updatedAt = time.Now().UTC()
}

// SetClinicDuration sobreescribe la duración de un procedimiento en una sede específica.
func (p *Professional) SetClinicDuration(assignmentID uuid.UUID, d valueobject.ProcedureDuration) error {
	for i := range p.clinicAssignments {
		if p.clinicAssignments[i].id == assignmentID {
			p.clinicAssignments[i].SetDurationForProcedure(d)
			p.updatedAt = time.Now().UTC()
			return nil
		}
	}
	return sharederrors.NewNotFound("ClinicAssignment", assignmentID.String())
}

// GetDurationForProcedureAtClinic resuelve la duración efectiva de un procedimiento
// en una sede específica: primero busca el override de la sede, luego el default del profesional.
func (p *Professional) GetDurationForProcedureAtClinic(
	clinicID sharedtypes.ClinicID,
	procedureCode string,
) (valueobject.ProcedureDuration, bool) {
	if assignment, ok := p.FindAssignmentForClinic(clinicID); ok {
		if d, ok := assignment.GetDurationForProcedure(procedureCode); ok {
			return d, true
		}
	}
	// Fallback al default del profesional.
	if d, ok := p.defaultDurations[procedureCode]; ok {
		return d, true
	}
	return valueobject.ProcedureDuration{}, false
}

// ── Comportamiento: Estado ────────────────────────────────────────

// Suspend suspende al profesional (todas sus asignaciones quedan congeladas).
func (p *Professional) Suspend(reason string, by uuid.UUID) error {
	if p.status == valueobject.ProfessionalStatusSuspended {
		return sharederrors.NewPrecondition("already_suspended",
			"el profesional ya está suspendido")
	}
	p.status = valueobject.ProfessionalStatusSuspended
	p.updatedAt = time.Now().UTC()

	p.pendingEvents = append(p.pendingEvents, event.ProfessionalSuspended{
		ProfessionalID: p.id,
		Reason:         reason,
		SuspendedBy:    by,
		OccurredAt:     time.Now().UTC(),
	})
	return nil
}

// Activate reactiva un profesional suspendido o inactivo.
func (p *Professional) Activate(by uuid.UUID) error {
	if p.status == valueobject.ProfessionalStatusActive {
		return sharederrors.NewPrecondition("already_active",
			"el profesional ya está activo")
	}
	p.status = valueobject.ProfessionalStatusActive
	p.updatedAt = time.Now().UTC()
	return nil
}

// CanAttendAtClinic verifica si el profesional puede atender en una sede en un momento dado.
func (p *Professional) CanAttendAtClinic(clinicID sharedtypes.ClinicID, at time.Time) bool {
	if !p.status.IsActive() {
		return false
	}
	assignment, ok := p.FindAssignmentForClinic(clinicID)
	if !ok {
		return false
	}
	return assignment.IsAvailableAt(at)
}

// ── Getters ───────────────────────────────────────────────────────

func (p *Professional) ID() sharedtypes.ProfessionalID         { return p.id }
func (p *Professional) UserID() *uuid.UUID                     { return p.userID }
func (p *Professional) FullName() sharedvo.FullName            { return p.fullName }
func (p *Professional) NationalID() sharedvo.NationalID        { return p.nationalID }
func (p *Professional) Email() sharedvo.Email                  { return p.email }
func (p *Professional) Phone() sharedvo.PhoneNumber            { return p.phone }
func (p *Professional) Bio() string                            { return p.bio }
func (p *Professional) Status() valueobject.ProfessionalStatus { return p.status }
func (p *Professional) Licenses() []ProfessionalLicense        { return p.licenses }
func (p *Professional) ClinicAssignments() []ClinicAssignment  { return p.clinicAssignments }
func (p *Professional) DefaultDurations() map[string]valueobject.ProcedureDuration {
	return p.defaultDurations
}
func (p *Professional) CreatedAt() time.Time  { return p.createdAt }
func (p *Professional) UpdatedAt() time.Time  { return p.updatedAt }
func (p *Professional) CreatedBy() *uuid.UUID { return p.createdBy }
func (p *Professional) Version() int64        { return p.version }

func (p *Professional) PendingEvents() []event.DomainEvent {
	evts := p.pendingEvents
	p.pendingEvents = nil
	return evts
}
