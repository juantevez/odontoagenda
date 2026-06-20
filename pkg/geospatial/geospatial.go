// Package geospatial provee helpers para operaciones geoespaciales con PostGIS.
// Encapsula la construcción de queries ST_DWithin, ST_Distance y ST_MakePoint
// de forma tipada y reutilizable entre bounded contexts.
package geospatial

import (
	"fmt"
	"math"
)

// ── Point ────────────────────────────────────────────────────────

// Point representa una coordenada geográfica en EPSG:4326 (WGS84).
type Point struct {
	Latitude  float64 `json:"lat"`
	Longitude float64 `json:"lng"`
}

// NewPoint crea un Point validando los rangos geográficos.
func NewPoint(lat, lng float64) (Point, error) {
	if lat < -90 || lat > 90 {
		return Point{}, fmt.Errorf("latitud inválida %.6f: rango [-90, 90]", lat)
	}
	if lng < -180 || lng > 180 {
		return Point{}, fmt.Errorf("longitud inválida %.6f: rango [-180, 180]", lng)
	}
	return Point{Latitude: lat, Longitude: lng}, nil
}

// IsZero reporta si el punto no fue inicializado.
func (p Point) IsZero() bool { return p.Latitude == 0 && p.Longitude == 0 }

// String devuelve representación WKT compatible con PostGIS.
func (p Point) String() string {
	return fmt.Sprintf("SRID=4326;POINT(%.8f %.8f)", p.Longitude, p.Latitude)
}

// WKT devuelve la representación Well-Known Text sin SRID prefix.
func (p Point) WKT() string {
	return fmt.Sprintf("POINT(%.8f %.8f)", p.Longitude, p.Latitude)
}

// ── RadiusSearch ─────────────────────────────────────────────────

// RadiusSearch encapsula los parámetros de una búsqueda radial.
type RadiusSearch struct {
	Center      Point
	RadiusMeters float64
}

// NewRadiusSearch crea una búsqueda radial con validación.
func NewRadiusSearch(center Point, radiusMeters float64) (RadiusSearch, error) {
	if radiusMeters <= 0 {
		return RadiusSearch{}, fmt.Errorf("radio debe ser positivo: %.2f", radiusMeters)
	}
	if radiusMeters > 200_000 { // máximo 200km
		return RadiusSearch{}, fmt.Errorf("radio excede máximo permitido (200 km): %.2f m", radiusMeters)
	}
	return RadiusSearch{Center: center, RadiusMeters: radiusMeters}, nil
}

// STDWithinClause genera el fragmento SQL para ST_DWithin.
// Uso típico en WHERE clause:
//
//	search.STDWithinClause("c.location") → "ST_DWithin(c.location::geography, ST_MakePoint($1,$2)::geography, $3)"
//
// Los argumentos posicionales comenzando en startArgN se usan para lng, lat, radius.
func (r RadiusSearch) STDWithinClause(column string, startArgN int) string {
	return fmt.Sprintf(
		"ST_DWithin(%s::geography, ST_MakePoint($%d, $%d)::geography, $%d)",
		column, startArgN, startArgN+1, startArgN+2,
	)
}

// Args devuelve los argumentos en el orden esperado por STDWithinClause: lng, lat, radius.
func (r RadiusSearch) Args() []any {
	return []any{r.Center.Longitude, r.Center.Latitude, r.RadiusMeters}
}

// STDistanceExpr genera la expresión SELECT para calcular la distancia en metros.
//
//	search.STDistanceExpr("c.location") → "ST_Distance(c.location::geography, ST_MakePoint($1,$2)::geography) AS distance_meters"
func (r RadiusSearch) STDistanceExpr(column string, startArgN int) string {
	return fmt.Sprintf(
		"ST_Distance(%s::geography, ST_MakePoint($%d, $%d)::geography) AS distance_meters",
		column, startArgN, startArgN+1,
	)
}

// ── Haversine ────────────────────────────────────────────────────

const earthRadiusMeters = 6_371_000.0

// HaversineDistance calcula la distancia en metros entre dos puntos.
// Para validaciones rápidas en memoria sin ir a la base de datos.
func HaversineDistance(a, b Point) float64 {
	dLat := toRad(b.Latitude - a.Latitude)
	dLng := toRad(b.Longitude - a.Longitude)

	sinLat := math.Sin(dLat / 2)
	sinLng := math.Sin(dLng / 2)

	h := sinLat*sinLat +
		math.Cos(toRad(a.Latitude))*math.Cos(toRad(b.Latitude))*sinLng*sinLng

	return 2 * earthRadiusMeters * math.Asin(math.Sqrt(h))
}

func toRad(deg float64) float64 { return deg * math.Pi / 180.0 }

// ── BoundingBox ──────────────────────────────────────────────────

// BoundingBox representa el rectángulo que contiene una búsqueda radial.
// Útil para pre-filtrar candidatos antes de ST_DWithin (optimización de índice).
type BoundingBox struct {
	MinLat, MaxLat float64
	MinLng, MaxLng float64
}

// BoundingBoxForRadius calcula el bounding box aproximado de un radio en metros.
func BoundingBoxForRadius(center Point, radiusMeters float64) BoundingBox {
	// 1 grado de latitud ≈ 111.32 km
	latDelta := radiusMeters / 111_320.0
	// 1 grado de longitud varía según latitud
	lngDelta := radiusMeters / (111_320.0 * math.Cos(toRad(center.Latitude)))

	return BoundingBox{
		MinLat: center.Latitude - latDelta,
		MaxLat: center.Latitude + latDelta,
		MinLng: center.Longitude - lngDelta,
		MaxLng: center.Longitude + lngDelta,
	}
}

// ── STMakePoint ──────────────────────────────────────────────────

// STMakePointExpr genera la expresión ST_MakePoint para un punto dado.
// Útil en INSERT/UPDATE de geometrías.
//
//	STMakePointExpr(1) → "ST_SetSRID(ST_MakePoint($1, $2), 4326)"
func STMakePointExpr(startArgN int) string {
	return fmt.Sprintf("ST_SetSRID(ST_MakePoint($%d, $%d), 4326)", startArgN, startArgN+1)
}

// PointArgs devuelve los argumentos para STMakePointExpr: lng, lat.
func PointArgs(p Point) []any {
	return []any{p.Longitude, p.Latitude}
}
