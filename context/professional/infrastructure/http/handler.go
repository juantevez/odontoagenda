// Package http contiene los adaptadores de entrada HTTP del bounded context Professional.
package http

import (
	"github.com/go-chi/chi/v5"
	profcmd "github.com/juantevez/odontoagenda/context/professional/application/command"
	profqry "github.com/juantevez/odontoagenda/context/professional/application/query"
	"github.com/juantevez/odontoagenda/pkg/middleware"
)

// RegisterRoutes monta todas las rutas del contexto Professional.
func RegisterRoutes(
	r chi.Router,
	jwtCfg middleware.JWTConfig,
	registerHandler *profcmd.RegisterProfessionalHandler,
	addLicenseHandler *profcmd.AddLicenseHandler,
	assignClinicHandler *profcmd.AssignToClinicHandler,
	updateScheduleHandler *profcmd.UpdateClinicScheduleHandler,
	addExceptionHandler *profcmd.AddExceptionHandler,
	setDurationHandler *profcmd.SetProcedureDurationHandler,
	suspendHandler *profcmd.SuspendProfessionalHandler,
	getByIDHandler *profqry.GetProfessionalByIDHandler,
	findByClinicHandler *profqry.FindByClinicHandler,
	availableAtHandler *profqry.FindAvailableAtHandler,
	forSchedulingHandler *profqry.GetProfessionalForSchedulingHandler,
) {
	_ = r
	_ = jwtCfg
	_ = registerHandler
	_ = addLicenseHandler
	_ = assignClinicHandler
	_ = updateScheduleHandler
	_ = addExceptionHandler
	_ = setDurationHandler
	_ = suspendHandler
	_ = getByIDHandler
	_ = findByClinicHandler
	_ = availableAtHandler
	_ = forSchedulingHandler
}
