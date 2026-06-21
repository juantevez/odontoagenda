package aggregate

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedtypes "github.com/juantevez/odontoagenda/pkg/shared/types"
)

// ── FamilyAccount — Aggregate Root ───────────────────────────────

// FamilyAccount agrupa a los miembros de una familia bajo una cuenta común.
// Permite que adultos con rol Guardian reserven citas en nombre de menores
// o de otros miembros a su cargo.
//
// Invariantes:
//   - Siempre debe existir exactamente un PrimaryAdult.
//   - Un menor siempre debe tener al menos un Guardian asignado.
//   - Solo adultos (Primary o Secondary) pueden ser Guardians.
//   - Un paciente pertenece a máximo 1 FamilyAccount activa.
type FamilyAccount struct {
	id             sharedtypes.FamilyID
	familyName     string
	primaryAdultID sharedtypes.PatientID
	members        []FamilyMember
	status         FamilyStatus
	audit          sharedtypes.AuditInfo
	version        int64
}

// FamilyStatus es el estado de la cuenta familiar.
type FamilyStatus string

const (
	FamilyStatusActive    FamilyStatus = "Active"
	FamilyStatusSuspended FamilyStatus = "Suspended"
)

// ── FamilyMember — Entity ─────────────────────────────────────────

// FamilyMember representa un miembro dentro de la FamilyAccount.
type FamilyMember struct {
	PatientID    sharedtypes.PatientID   `json:"patient_id"`
	Role         FamilyRole              `json:"role"`
	Relationship Relationship            `json:"relationship"`
	IsMinor      bool                    `json:"is_minor"`
	GuardianIDs  []sharedtypes.PatientID `json:"guardian_ids,omitempty"`
	JoinedAt     time.Time               `json:"joined_at"`
}

type FamilyRole string

const (
	FamilyRolePrimaryAdult   FamilyRole = "PrimaryAdult"
	FamilyRoleSecondaryAdult FamilyRole = "SecondaryAdult"
	FamilyRoleDependent      FamilyRole = "Dependent"
)

type Relationship string

const (
	RelationshipSelf    Relationship = "Titular"
	RelationshipSpouse  Relationship = "Cónyuge"
	RelationshipChild   Relationship = "Hijo/a"
	RelationshipParent  Relationship = "Padre/Madre"
	RelationshipSibling Relationship = "Hermano/a"
	RelationshipOther   Relationship = "Otro"
)

// ── Constructor ───────────────────────────────────────────────────

// NewFamilyAccount crea una cuenta familiar con el adulto principal como único miembro inicial.
// Se crea automáticamente al registrar un paciente adulto.
func NewFamilyAccount(
	primaryAdultID sharedtypes.PatientID,
	familyName string,
	createdBy *uuid.UUID,
) *FamilyAccount {
	id := sharedtypes.NewID()
	now := time.Now().UTC()

	primaryMember := FamilyMember{
		PatientID:    primaryAdultID,
		Role:         FamilyRolePrimaryAdult,
		Relationship: RelationshipSelf,
		IsMinor:      false,
		GuardianIDs:  nil,
		JoinedAt:     now,
	}

	return &FamilyAccount{
		id:             id,
		familyName:     familyName,
		primaryAdultID: primaryAdultID,
		members:        []FamilyMember{primaryMember},
		status:         FamilyStatusActive,
		audit:          sharedtypes.NewAuditInfo(createdBy),
		version:        1,
	}
}

// ── Comportamiento de dominio ─────────────────────────────────────

// AddMinor agrega un menor con sus guardianes.
// Invariante: el menor debe tener al menos un Guardian, y ese Guardian debe
// ser un adulto de la misma FamilyAccount.
func (f *FamilyAccount) AddMinor(
	patientID sharedtypes.PatientID,
	relationship Relationship,
	guardianIDs []sharedtypes.PatientID,
	addedBy uuid.UUID,
) error {
	if len(guardianIDs) == 0 {
		return sharederrors.NewPrecondition("minor_requires_guardian",
			"un menor debe tener al menos un guardian asignado")
	}

	// Validar que los guardianes son adultos de esta cuenta.
	for _, gID := range guardianIDs {
		if !f.isAdult(gID) {
			return sharederrors.NewPrecondition("guardian_must_be_adult",
				"el guardian debe ser un adulto miembro de esta cuenta familiar")
		}
	}

	if f.hasMember(patientID) {
		return sharederrors.NewAlreadyExists("FamilyMember", "patient_id", patientID.String())
	}

	f.members = append(f.members, FamilyMember{
		PatientID:    patientID,
		Role:         FamilyRoleDependent,
		Relationship: relationship,
		IsMinor:      true,
		GuardianIDs:  guardianIDs,
		JoinedAt:     time.Now().UTC(),
	})

	f.audit.Touch(&addedBy)
	return nil
}

// AddSecondaryAdult agrega un adulto secundario (ej: cónyuge).
// El adulto secundario NO requiere guardian y puede ser guardian de menores.
func (f *FamilyAccount) AddSecondaryAdult(
	patientID sharedtypes.PatientID,
	relationship Relationship,
	addedBy uuid.UUID,
) error {
	if f.hasMember(patientID) {
		return sharederrors.NewAlreadyExists("FamilyMember", "patient_id", patientID.String())
	}

	f.members = append(f.members, FamilyMember{
		PatientID:    patientID,
		Role:         FamilyRoleSecondaryAdult,
		Relationship: relationship,
		IsMinor:      false,
		GuardianIDs:  nil,
		JoinedAt:     time.Now().UTC(),
	})

	f.audit.Touch(&addedBy)
	return nil
}

// RemoveMember elimina a un miembro de la cuenta.
// Invariante: el PrimaryAdult no puede ser removido.
func (f *FamilyAccount) RemoveMember(patientID sharedtypes.PatientID, removedBy uuid.UUID) error {
	if patientID == f.primaryAdultID {
		return sharederrors.NewPrecondition("cannot_remove_primary",
			"el titular principal no puede ser removido de la cuenta familiar")
	}

	// Verificar que ningún menor quede sin guardian tras la remoción.
	for _, m := range f.members {
		if m.IsMinor {
			guardians := f.activeGuardiansExcluding(m.GuardianIDs, patientID)
			if len(guardians) == 0 {
				return sharederrors.NewPrecondition("minor_needs_guardian",
					"no se puede remover: un menor de la cuenta quedaría sin guardian")
			}
		}
	}

	newMembers := make([]FamilyMember, 0, len(f.members))
	for _, m := range f.members {
		if m.PatientID != patientID {
			newMembers = append(newMembers, m)
		}
	}

	if len(newMembers) == len(f.members) {
		return sharederrors.NewNotFound("FamilyMember", patientID.String())
	}

	f.members = newMembers
	f.audit.Touch(&removedBy)
	return nil
}

// CanBookFor evalúa la política de autorización de reserva en nombre de otro.
// Retorna nil si está permitido, error descriptivo si no.
func (f *FamilyAccount) CanBookFor(requesterPatientID, targetPatientID sharedtypes.PatientID) error {
	// Reserva propia: siempre permitida.
	if requesterPatientID == targetPatientID {
		return nil
	}

	// El target debe estar en esta cuenta.
	if !f.hasMember(targetPatientID) {
		return sharederrors.NewForbidden("book_appointment",
			"el paciente objetivo no pertenece a esta cuenta familiar")
	}

	// El requester debe ser guardian del target.
	target := f.findMember(targetPatientID)
	if target == nil {
		return sharederrors.NewForbidden("book_appointment", "miembro no encontrado")
	}

	for _, gID := range target.GuardianIDs {
		if gID == requesterPatientID {
			return nil // es guardian, permitido
		}
	}

	// Adultos pueden reservar para menores si son de la misma cuenta y el target es menor.
	if target.IsMinor && f.isAdult(requesterPatientID) {
		return nil
	}

	return sharederrors.NewForbidden("book_appointment",
		"no tiene autorización para reservar en nombre de este paciente")
}

// ReconstituteFamilyAccount reconstruye una FamilyAccount desde persistencia sin disparar eventos.
func ReconstituteFamilyAccount(
	id sharedtypes.FamilyID,
	familyName string,
	primaryAdultID sharedtypes.PatientID,
	members []FamilyMember,
	status FamilyStatus,
	audit sharedtypes.AuditInfo,
	version int64,
) *FamilyAccount {
	return &FamilyAccount{
		id:             id,
		familyName:     familyName,
		primaryAdultID: primaryAdultID,
		members:        members,
		status:         status,
		audit:          audit,
		version:        version,
	}
}

// ParseFamilyStatus valida y convierte un string al tipo FamilyStatus.
func ParseFamilyStatus(s string) (FamilyStatus, error) {
	switch FamilyStatus(s) {
	case FamilyStatusActive, FamilyStatusSuspended:
		return FamilyStatus(s), nil
	default:
		return "", fmt.Errorf("family status inválido '%s'", s)
	}
}

// ── Getters ───────────────────────────────────────────────────────

func (f *FamilyAccount) ID() sharedtypes.FamilyID              { return f.id }
func (f *FamilyAccount) FamilyName() string                    { return f.familyName }
func (f *FamilyAccount) PrimaryAdultID() sharedtypes.PatientID { return f.primaryAdultID }
func (f *FamilyAccount) Members() []FamilyMember               { return f.members }
func (f *FamilyAccount) Status() FamilyStatus                  { return f.status }
func (f *FamilyAccount) Version() int64                        { return f.version }
func (f *FamilyAccount) Audit() sharedtypes.AuditInfo          { return f.audit }

// ── Helpers internos ──────────────────────────────────────────────

func (f *FamilyAccount) hasMember(id sharedtypes.PatientID) bool {
	return f.findMember(id) != nil
}

func (f *FamilyAccount) findMember(id sharedtypes.PatientID) *FamilyMember {
	for i := range f.members {
		if f.members[i].PatientID == id {
			return &f.members[i]
		}
	}
	return nil
}

func (f *FamilyAccount) isAdult(id sharedtypes.PatientID) bool {
	m := f.findMember(id)
	return m != nil && !m.IsMinor
}

func (f *FamilyAccount) activeGuardiansExcluding(guardianIDs []sharedtypes.PatientID, exclude sharedtypes.PatientID) []sharedtypes.PatientID {
	result := make([]sharedtypes.PatientID, 0)
	for _, gID := range guardianIDs {
		if gID != exclude && f.isAdult(gID) {
			result = append(result, gID)
		}
	}
	return result
}
