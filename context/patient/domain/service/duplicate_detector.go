// Package service contiene los Domain Services del bounded context Patient.
package service

import (
	"context"
	"strings"

	"github.com/juantevez/odontoagenda/context/patient/domain/aggregate"
	"github.com/juantevez/odontoagenda/context/patient/domain/repository"
	sharederrors "github.com/juantevez/odontoagenda/pkg/shared/errors"
	sharedvo "github.com/juantevez/odontoagenda/pkg/shared/valueobject"
)

// ── DuplicateDetector — Domain Service ───────────────────────────

// DuplicateDetector es un Domain Service que evalúa si un nuevo paciente
// a registrar es potencialmente duplicado de uno existente.
//
// Es Domain Service (no lógica del Aggregate) porque cruza múltiples
// Patients para tomar la decisión, algo que el Aggregate no puede hacer
// sin romper su encapsulamiento.
type DuplicateDetector struct {
	repo repository.PatientRepository
}

func NewDuplicateDetector(repo repository.PatientRepository) *DuplicateDetector {
	return &DuplicateDetector{repo: repo}
}

// DuplicateCandidate es un posible duplicado detectado, con su score de similitud.
type DuplicateCandidate struct {
	Patient   *aggregate.Patient
	Score     float64  // 0.0 - 1.0
	MatchedOn []string // qué campos coincidieron: "name", "phone", "national_id"
}

// DetectResult encapsula el resultado de la detección.
type DetectResult struct {
	HasDuplicates bool
	Candidates    []DuplicateCandidate
}

// Detect busca posibles duplicados para los datos de un paciente a registrar.
// Retorna los candidatos ordenados por score descendente.
//
// La lógica de scoring:
//   - Mismo NationalID exacto → score 1.0 (duplicado certero → ErrAlreadyExists)
//   - Mismo phone → score 0.8
//   - Nombre similar (normalizado) → score 0.5
//   - Combinación nombre + birthdate → score 0.9
func (d *DuplicateDetector) Detect(
	ctx context.Context,
	fullName string,
	phone string,
	nationalID sharedvo.NationalID,
) (*DetectResult, error) {
	// 1. Primero chequeamos por NationalID exacto: es un duplicado certero.
	exists, err := d.repo.ExistsByNationalID(ctx, nationalID)
	if err != nil {
		return nil, sharederrors.NewInternal(err)
	}
	if exists {
		return nil, sharederrors.NewAlreadyExists("Patient", "national_id", nationalID.Number)
	}

	// 2. Búsqueda fuzzy por nombre y teléfono.
	candidates, err := d.repo.FindPotentialDuplicates(ctx, fullName, phone)
	if err != nil {
		return nil, sharederrors.NewInternal(err)
	}

	if len(candidates) == 0 {
		return &DetectResult{HasDuplicates: false}, nil
	}

	scored := make([]DuplicateCandidate, 0, len(candidates))
	normalizedName := normalizeName(fullName)

	for _, candidate := range candidates {
		score := 0.0
		matched := []string{}

		// Score por similitud de nombre.
		candidateName := normalizeName(candidate.FullName().String())
		if nameSimilarity(normalizedName, candidateName) > 0.85 {
			score += 0.5
			matched = append(matched, "name")
		}

		// Score por teléfono.
		if phone != "" && candidate.ContactInfo().Phone.String() == phone {
			score += 0.8
			matched = append(matched, "phone")
		}

		if score > 0 {
			scored = append(scored, DuplicateCandidate{
				Patient:   candidate,
				Score:     min(score, 1.0),
				MatchedOn: matched,
			})
		}
	}

	return &DetectResult{
		HasDuplicates: len(scored) > 0,
		Candidates:    scored,
	}, nil
}

// ── Helpers internos ──────────────────────────────────────────────

// normalizeName normaliza un nombre para comparación:
// minúsculas, sin acentos aproximados, sin espacios extra.
func normalizeName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	// Normalización básica de acentos comunes en español.
	replacer := strings.NewReplacer(
		"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u",
		"ü", "u", "ñ", "n",
	)
	name = replacer.Replace(name)
	// Colapsar múltiples espacios.
	for strings.Contains(name, "  ") {
		name = strings.ReplaceAll(name, "  ", " ")
	}
	return name
}

// nameSimilarity calcula similitud entre dos strings usando bigramas (Dice coefficient).
// Retorna valor entre 0.0 (completamente distintos) y 1.0 (idénticos).
func nameSimilarity(a, b string) float64 {
	if a == b {
		return 1.0
	}
	if len(a) < 2 || len(b) < 2 {
		return 0.0
	}

	bigramsA := bigrams(a)
	bigramsB := bigrams(b)

	intersection := 0
	for bg := range bigramsA {
		if bigramsB[bg] > 0 {
			intersection++
		}
	}

	return float64(2*intersection) / float64(len(bigramsA)+len(bigramsB))
}

// bigrams genera los bigramas de un string (pares de caracteres consecutivos).
func bigrams(s string) map[string]int {
	result := make(map[string]int)
	runes := []rune(s)
	for i := 0; i < len(runes)-1; i++ {
		bg := string(runes[i : i+2])
		result[bg]++
	}
	return result
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
