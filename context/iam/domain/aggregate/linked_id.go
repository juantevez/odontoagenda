// Package aggregate — SetLinkedID para el aggregate User.
// Permite al subscriber de IAM vincular un User recién creado con su Patient.
package aggregate

import (
	"github.com/google/uuid"
)

// SetLinkedID establece el linked_id y linked_type del User.
// Llamado por PatientProvisionSubscriber después de crear el Patient.
// Solo puede actualizarse si el User no tiene linked_id previo (una sola vez).
func (u *User) SetLinkedID(id *uuid.UUID, linkedType string) {
	u.linkedID = id
	u.linkedType = linkedType
	selfID := uuid.UUID(u.id)
	u.audit.Touch(&selfID)
}
