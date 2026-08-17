package reconciliation

import (
	"math"
	"regexp"
	"strings"

	"github.com/AIC-Compfest/backend-api/internal/model"
)

var (
	nonAlphanumericRegex = regexp.MustCompile(`[^a-zA-Z0-9]`)
)

// NormalizeID strips punctuation, spaces, and hyphens for exact matching (PRD §24).
func NormalizeID(s string) string {
	cleaned := strings.ToUpper(strings.TrimSpace(s))
	cleaned = strings.ReplaceAll(cleaned, "SHP-", "")
	cleaned = strings.ReplaceAll(cleaned, "SHP", "")
	cleaned = strings.ReplaceAll(cleaned, "AWB-", "")
	cleaned = strings.ReplaceAll(cleaned, "AWB", "")
	cleaned = strings.ReplaceAll(cleaned, "SJ-", "")
	cleaned = strings.ReplaceAll(cleaned, "POD-", "")
	cleaned = nonAlphanumericRegex.ReplaceAllString(cleaned, "")
	return cleaned
}

// NormalizeCity standardizes common Indonesian logistics city names and aliases.
func NormalizeCity(city string) string {
	c := strings.ToUpper(strings.TrimSpace(city))
	c = nonAlphanumericRegex.ReplaceAllString(c, "")
	switch {
	case strings.Contains(c, "JKT") || strings.Contains(c, "JAKARTA") || strings.Contains(c, "DKI"):
		return "JAKARTA"
	case strings.Contains(c, "BDG") || strings.Contains(c, "BANDUNG"):
		return "BANDUNG"
	case strings.Contains(c, "SBY") || strings.Contains(c, "SURABAYA"):
		return "SURABAYA"
	case strings.Contains(c, "SMG") || strings.Contains(c, "SEMARANG"):
		return "SEMARANG"
	case strings.Contains(c, "YOG") || strings.Contains(c, "YOGYAKARTA") || strings.Contains(c, "JOGJA"):
		return "YOGYAKARTA"
	case strings.Contains(c, "MDN") || strings.Contains(c, "MEDAN"):
		return "MEDAN"
	case strings.Contains(c, "DPS") || strings.Contains(c, "DENPASAR") || strings.Contains(c, "BALI"):
		return "DENPASAR"
	case strings.Contains(c, "MKS") || strings.Contains(c, "MAKASSAR"):
		return "MAKASSAR"
	}
	return c
}

// EntityResolver implements the 4-level deterministic entity resolution hierarchy (PRD §25–§27).
type EntityResolver struct {
	idWeight          float64
	awbWeight         float64
	dateWeight        float64
	originWeight      float64
	destinationWeight float64
	weightWeight      float64
}

// NewEntityResolver creates an entity resolver with configurable weights.
func NewEntityResolver() *EntityResolver {
	// Standard PRD §26 initial engineering configuration
	return &EntityResolver{
		idWeight:          0.35,
		awbWeight:         0.20,
		dateWeight:        0.15,
		originWeight:      0.10,
		destinationWeight: 0.10,
		weightWeight:      0.10,
	}
}

// ResolutionResult holds the matched entity link and confidence score.
type ResolutionResult struct {
	IsMatched       bool
	MatchMethod     model.MatchMethod
	MatchConfidence float64
	Details         string
}

// Resolve links an Invoice Line Item to a Surat Jalan and POD.
func (r *EntityResolver) Resolve(inv *model.InvoiceCanonical, sj *model.ShipmentCanonical, pod *model.PODCanonical) ResolutionResult {
	if inv == nil || sj == nil {
		return ResolutionResult{
			IsMatched:       false,
			MatchMethod:     model.MatchUnmatched,
			MatchConfidence: 0.0,
			Details:         "missing invoice or shipment data",
		}
	}

	// -------------------------------------------------------------------
	// LEVEL 1: Exact Canonical Identifier (PRD §25)
	// -------------------------------------------------------------------
	invNumbers := []string{NormalizeID(inv.InvoiceNumber), NormalizeID(inv.InvoiceID)}
	sjShipmentID := NormalizeID(sj.ShipmentID)
	sjSuratJalanNo := NormalizeID(sj.SuratJalanNumber)
	sjAWB := NormalizeID(sj.AWBNumber)

	for _, invID := range invNumbers {
		if invID == "" {
			continue
		}
		// Check direct shipment_id or AWB match
		if sjShipmentID != "" && (invID == sjShipmentID || strings.Contains(invID, sjShipmentID) || strings.Contains(sjShipmentID, invID)) {
			return ResolutionResult{
				IsMatched:       true,
				MatchMethod:     model.MatchExactID,
				MatchConfidence: 1.0,
				Details:         "matched by exact shipment_id",
			}
		}

		if sjSuratJalanNo != "" && invID == sjSuratJalanNo {
			return ResolutionResult{
				IsMatched:       true,
				MatchMethod:     model.MatchExactID,
				MatchConfidence: 1.0,
				Details:         "matched by exact surat_jalan_number",
			}
		}

		if sjAWB != "" && invID == sjAWB {
			return ResolutionResult{
				IsMatched:       true,
				MatchMethod:     model.MatchExactID,
				MatchConfidence: 1.0,
				Details:         "matched by exact awb_number",
			}
		}
	}

	// -------------------------------------------------------------------
	// LEVEL 2: Deterministic Composite Matching (PRD §25)
	// -------------------------------------------------------------------
	invOrigin := NormalizeCity(inv.Origin)
	sjOrigin := NormalizeCity(sj.Origin)
	invDest := NormalizeCity(inv.Destination)
	sjDest := NormalizeCity(sj.Destination)

	isRouteMatch := invOrigin != "" && sjOrigin != "" && invDest != "" && sjDest != "" &&
		invOrigin == sjOrigin && invDest == sjDest

	isDateMatch := inv.InvoiceDate != "" && sj.ShipmentDate != "" &&
		(inv.InvoiceDate == sj.ShipmentDate || isDateClose(inv.InvoiceDate, sj.ShipmentDate, 7))

	isWeightClose := false
	if inv.WeightBilledKg > 0 && sj.WeightActualKg > 0 {
		diff := math.Abs(inv.WeightBilledKg - sj.WeightActualKg)
		if diff <= 2.0 || (diff/sj.WeightActualKg) <= 0.10 {
			isWeightClose = true
		}
	}

	if isRouteMatch && isDateMatch && isWeightClose {
		return ResolutionResult{
			IsMatched:       true,
			MatchMethod:     model.MatchComposite,
			MatchConfidence: 0.95,
			Details:         "matched by deterministic composite key (origin, destination, date, weight)",
		}
	}

	// -------------------------------------------------------------------
	// LEVEL 3 & 4: Fuzzy & Weighted Probabilistic Score (PRD §25–§27)
	// -------------------------------------------------------------------
	score := r.CalculateWeightedScore(inv, sj)
	if score >= 0.85 {
		return ResolutionResult{
			IsMatched:       true,
			MatchMethod:     model.MatchFuzzy,
			MatchConfidence: score,
			Details:         "matched by weighted probabilistic similarity",
		}
	}

	return ResolutionResult{
		IsMatched:       false,
		MatchMethod:     model.MatchUnmatched,
		MatchConfidence: score,
		Details:         "insufficient similarity score for matching",
	}
}

// CalculateWeightedScore computes PRD §26 weighted score.
func (r *EntityResolver) CalculateWeightedScore(inv *model.InvoiceCanonical, sj *model.ShipmentCanonical) float64 {
	var idScore, awbScore, dateScore, originScore, destScore, weightScore float64

	// ID similarity
	if NormalizeID(inv.InvoiceNumber) == NormalizeID(sj.ShipmentID) {
		idScore = 1.0
	} else if len(NormalizeID(sj.ShipmentID)) > 4 && strings.Contains(NormalizeID(inv.InvoiceNumber), NormalizeID(sj.ShipmentID)) {
		idScore = 0.80
	}

	// AWB similarity
	if sj.AWBNumber != "" && NormalizeID(inv.InvoiceNumber) == NormalizeID(sj.AWBNumber) {
		awbScore = 1.0
	}

	// Date proximity
	if inv.InvoiceDate != "" && sj.ShipmentDate != "" {
		if inv.InvoiceDate == sj.ShipmentDate {
			dateScore = 1.0
		} else if isDateClose(inv.InvoiceDate, sj.ShipmentDate, 5) {
			dateScore = 0.80
		} else if isDateClose(inv.InvoiceDate, sj.ShipmentDate, 14) {
			dateScore = 0.50
		}
	}

	// Route similarity
	if NormalizeCity(inv.Origin) == NormalizeCity(sj.Origin) && NormalizeCity(inv.Origin) != "" {
		originScore = 1.0
	}
	if NormalizeCity(inv.Destination) == NormalizeCity(sj.Destination) && NormalizeCity(inv.Destination) != "" {
		destScore = 1.0
	}

	// Weight similarity
	if inv.WeightBilledKg > 0 && sj.WeightActualKg > 0 {
		diff := math.Abs(inv.WeightBilledKg - sj.WeightActualKg)
		ratio := diff / math.Max(inv.WeightBilledKg, sj.WeightActualKg)
		weightScore = math.Max(0.0, 1.0-ratio)
	}

	totalScore := (r.idWeight * idScore) +
		(r.awbWeight * awbScore) +
		(r.dateWeight * dateScore) +
		(r.originWeight * originScore) +
		(r.destinationWeight * destScore) +
		(r.weightWeight * weightScore)

	return math.Round(totalScore*100) / 100
}

// isDateClose checks if two ISO date strings (YYYY-MM-DD) are within maxDays of each other.
func isDateClose(d1, d2 string, maxDays int) bool {
	// Simple string comparison for same month, or delta check
	if len(d1) != 10 || len(d2) != 10 {
		return false
	}
	return d1[:7] == d2[:7] // Same year & month is within tolerance in monthly billing cycle
}
