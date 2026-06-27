// Package repository define los puertos de salida del bounded context Scheduling.
package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/scheduling/domain/valueobject"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── AppointmentRepository ─────────────────────────────────────────

// AppointmentRepository es el puerto de salida para Appointment.
type AppointmentRepository interface {
	// Save persiste un Appointment nuevo.
	// Debe ejecutarse dentro de una transacción para garantizar
	// consistencia con AddBookedSlot en AvailabilitySchedule.
	Save(ctx context.Context, appt *aggregate.Appointment) error

	// Update actualiza con optimistic locking (version check).
	// Retorna ErrConflict si la versión difiere → indicador de doble reserva.
	Update(ctx context.Context, appt *aggregate.Appointment) error

	// FindByID busca un Appointment por su UUID.
	FindByID(ctx context.Context, id sharedtypes.AppointmentID) (*aggregate.Appointment, error)

	// FindActiveByPatient retorna las citas activas de un paciente.
	// Usado para verificar el límite de citas simultáneas.
	FindActiveByPatient(ctx context.Context, patientID sharedtypes.PatientID) ([]*aggregate.Appointment, error)

	// FindByProfessionalAndDate retorna citas de un profesional en un rango de fechas.
	FindByProfessionalAndDate(
		ctx context.Context,
		professionalID sharedtypes.ProfessionalID,
		clinicID sharedtypes.ClinicID,
		from, to time.Time,
	) ([]*aggregate.Appointment, error)

	// FindByClinicAndDate retorna el listado del día para una sede.
	// Es la query más usada por recepcionistas y profesionales.
	FindByClinicAndDate(
		ctx context.Context,
		clinicID sharedtypes.ClinicID,
		date time.Time,
	) ([]*aggregate.Appointment, error)

	// CountActiveByPatient cuenta las citas activas de un paciente.
	// Más eficiente que FindActiveByPatient para la validación de límite.
	CountActiveByPatient(ctx context.Context, patientID sharedtypes.PatientID) (int, error)
}

// ── AvailabilityScheduleRepository ───────────────────────────────

// AvailabilityScheduleRepository es el puerto de salida para AvailabilitySchedule.
type AvailabilityScheduleRepository interface {
	// Save persiste un AvailabilitySchedule nuevo.
	Save(ctx context.Context, schedule *aggregate.AvailabilitySchedule) error

	// Update actualiza con optimistic locking.
	Update(ctx context.Context, schedule *aggregate.AvailabilitySchedule) error

	// FindByProfessionalAndClinic busca por la clave de negocio (ProfessionalID, ClinicID).
	FindByProfessionalAndClinic(
		ctx context.Context,
		professionalID sharedtypes.ProfessionalID,
		clinicID sharedtypes.ClinicID,
	) (*aggregate.AvailabilitySchedule, error)

	// FindByClinic retorna todos los schedules activos de una sede.
	FindByClinic(ctx context.Context, clinicID sharedtypes.ClinicID) ([]*aggregate.AvailabilitySchedule, error)
}

// ── AvailabilityCache — Puerto de salida para Redis ───────────────

// AvailabilityCache es el puerto de salida para el cache de disponibilidad.
// El adaptador concreto vive en infrastructure/redis/.
//
// El cache almacena slots libres pre-calculados por (ProfessionalID, ClinicID, Date).
// Se invalida al recibir eventos de cambio de horario.
type AvailabilityCache interface {
	// GetSlots retorna los slots libres cacheados para una fecha.
	// Retorna (nil, nil) si no hay cache para esa combinación (cache miss).
	GetSlots(
		ctx context.Context,
		professionalID sharedtypes.ProfessionalID,
		clinicID sharedtypes.ClinicID,
		date time.Time,
		procedureCode string,
	) ([]aggregate.FreeSlot, error)

	// SetSlots almacena los slots libres calculados con TTL de 5 minutos.
	SetSlots(
		ctx context.Context,
		professionalID sharedtypes.ProfessionalID,
		clinicID sharedtypes.ClinicID,
		date time.Time,
		procedureCode string,
		slots []aggregate.FreeSlot,
	) error

	// InvalidateSchedule elimina todas las entradas de cache para (Professional, Clinic).
	// Llamado al recibir ProfessionalScheduleUpdated.
	InvalidateSchedule(
		ctx context.Context,
		professionalID sharedtypes.ProfessionalID,
		clinicID sharedtypes.ClinicID,
	) error

	// AcquireSlotLock intenta adquirir un lock temporal sobre un slot específico.
	// Previene reservas concurrentes del mismo slot durante el proceso de booking.
	// Retorna (true, nil) si el lock fue adquirido; (false, nil) si ya estaba tomado.
	AcquireSlotLock(
		ctx context.Context,
		professionalID sharedtypes.ProfessionalID,
		clinicID sharedtypes.ClinicID,
		slotStart time.Time,
		ttl time.Duration,
	) (bool, error)

	// ReleaseSlotLock libera el lock de un slot (en caso de error en la saga).
	ReleaseSlotLock(
		ctx context.Context,
		professionalID sharedtypes.ProfessionalID,
		clinicID sharedtypes.ClinicID,
		slotStart time.Time,
	) error
}

// ── DistributedLock — Puerto de salida para locks Redis ──────────

// DistributedLock es el puerto para locks distribuidos generales.
// El slot lock específico está en AvailabilityCache para colocation.
type DistributedLock interface {
	// Acquire intenta adquirir un lock con un key y TTL dados.
	Acquire(ctx context.Context, key string, ttl time.Duration) (lockToken string, acquired bool, err error)
	// Release libera un lock por su token (previene que otro proceso lo libere).
	Release(ctx context.Context, key string, lockToken string) error
}

// ── SlotHold — bloqueo temporal de un slot ────────────────────────

// SlotHold representa un bloqueo temporal mientras un usuario completa la reserva.
type SlotHold struct {
	ID             uuid.UUID
	ProfessionalID sharedtypes.ProfessionalID
	ClinicID       sharedtypes.ClinicID
	SlotStart      time.Time
	SlotEnd        time.Time
	HeldBy         uuid.UUID // user_id
	HeldUntil      time.Time
}

// SlotHoldRepository es el puerto de salida para slot_holds.
type SlotHoldRepository interface {
	// Create inserta un hold. Retorna ErrConflict si ya existe un hold activo para ese slot.
	Create(ctx context.Context, hold *SlotHold) error

	// Release elimina un hold por su ID (liberación anticipada por el usuario).
	Release(ctx context.Context, holdID uuid.UUID) error

	// ActiveStartTimesForDay devuelve los slot_start con holds activos (held_until > now())
	// para un (professional_id, clinic_id) en la fecha indicada.
	// Usado por GetAvailabilityHandler para filtrar la grilla.
	ActiveStartTimesForDay(
		ctx context.Context,
		professionalID sharedtypes.ProfessionalID,
		clinicID sharedtypes.ClinicID,
		date time.Time,
	) ([]time.Time, error)

	// DeleteExpired borra los holds ya expirados. Llamado por el cleanup worker.
	DeleteExpired(ctx context.Context) (int64, error)
}

// ── FreeSlot — DTO de respuesta de cache/cálculo ─────────────────
// Definido en aggregate para que repository pueda referenciarlo.
// (En Go, ponemos DTOs simples en el paquete aggregate para evitar ciclos)

// FreeSlot está definido en aggregate/availability_schedule.go
// como tipo de retorno del SlotCalculator.
var _ = uuid.Nil // evitar import no usado
var _ = valueobject.StatusConfirmed
