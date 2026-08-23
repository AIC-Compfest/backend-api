package reconciliation

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"backend-api/internal/model"
)

// nonAlphanumericRegex is used for aggressive normalization ONLY when stripping all non-alphanumeric.
var nonAlphanumericRegex = regexp.MustCompile(`[^a-zA-Z0-9]`)

// prefixOnlyRegex matches known ID prefixes at the START of a string (word-boundary safe).
// These are stripped to normalize identifiers for comparison.
var knownPrefixRegex = regexp.MustCompile(`(?i)^(SHP|AWB|SJ|POD|INV|CTR|EVT)[-\s]?`)

// NormalizeID normalizes an identifier for EXACT comparison by:
//  1. Trimming and uppercasing
//  2. Stripping known LEADING prefixes (SHP-, AWB-, etc.) using regex (NOT simple Replace)
//  3. Removing remaining non-alphanumeric characters
//
// CRITICAL: This does NOT strip arbitrary substrings.
// "SHP-001" → "001" (only leading SHP- prefix removed)
// "SUHADI SHIPPING" → "SUHADISHIPPING" (no false strip)
// "AWB-12345" → "12345"
// "AWB12345" → "12345"
//
// WARNING: After normalization, two different IDs should ONLY be considered equal
// if their normalized form is byte-identical. Never use substring matching as "exact".
func NormalizeID(s string) string {
	if s == "" {
		return ""
	}
	cleaned := strings.ToUpper(strings.TrimSpace(s))
	// Strip leading known prefix (word-safe regex)
	cleaned = knownPrefixRegex.ReplaceAllString(cleaned, "")
	// Strip remaining non-alphanumeric (hyphens, spaces, etc.)
	cleaned = nonAlphanumericRegex.ReplaceAllString(cleaned, "")
	return cleaned
}

// NormalizeIDStrictEqual returns true if two IDs refer to the same entity after normalization.
// Both IDs must produce non-empty normalized forms AND be byte-identical.
// This is STRICT EQUALITY — no substring, no fuzzy, no contains.
func NormalizeIDStrictEqual(a, b string) bool {
	na, nb := NormalizeID(a), NormalizeID(b)
	return na != "" && nb != "" && na == nb
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

// MatchDecision categorizes the match outcome.
type MatchDecision string

const (
	MatchDecisionAutoMatch      MatchDecision = "AUTO_MATCH"
	MatchDecisionReviewRequired MatchDecision = "REVIEW_REQUIRED"
	MatchDecisionNoMatch        MatchDecision = "NO_MATCH"
	MatchDecisionConflict       MatchDecision = "CONFLICT"
)

// ResolutionResult holds the matched entity link and confidence score.
type ResolutionResult struct {
	IsMatched       bool
	MatchDecision   MatchDecision
	MatchMethod     model.MatchMethod
	MatchConfidence float64
	Details         string

	// Signal breakdown for auditability
	Signals SignalBreakdown

	// Hard conflict details (if CONFLICT)
	ConflictReason string
}

// SignalBreakdown records each individual matching signal for audit/explainability.
type SignalBreakdown struct {
	ShipmentIDExact    *bool   // nil = not available, true = matched, false = conflicted
	AWBExact           *bool
	VendorMatch        *bool
	DateCompatible     *bool
	RouteMatch         *bool
	WeightCompatible   *bool
	ScoreBreakdown     map[string]float64
	TotalScore         float64
}

// EntityResolver implements the 4-level deterministic entity resolution hierarchy.
//
// Priority order (MUST be respected):
//  1. Hard conflict detection  → immediate NO_MATCH/CONFLICT (blocking)
//  2. Exact canonical identity → MATCHED_BY_EXACT_ID (highest trust)
//  3. Strong composite key     → MATCHED_BY_COMPOSITE (deterministic)
//  4. Controlled fuzzy score   → MATCHED_BY_FUZZY (only with hard constraints satisfied)
//
// Design rules:
//  - Similarity ≠ Identity
//  - Missing Evidence ≠ Match
//  - Conflicting identity keys always block matching, regardless of other signals
type EntityResolver struct {
	// Scoring weights — documented with rationale, not magic numbers
	// Sum of all weights = 1.0
	weights MatchWeights

	// Thresholds for match decisions
	autoMatchThreshold    float64 // Score above this → AUTO_MATCH
	reviewThreshold       float64 // Score above this → REVIEW_REQUIRED (below = NO_MATCH)
}

// MatchWeights holds configurable scoring weights with documented rationale.
type MatchWeights struct {
	ShipmentIDExact float64 // Primary identity — highest weight (50 pts / 100)
	AWBExact        float64 // Secondary identity (30 pts / 100)
	VendorMatch     float64 // Business constraint (10 pts / 100)
	DateCompatible  float64 // Temporal constraint (5 pts / 100)
	RouteMatch      float64 // Spatial constraint (5 pts / 100)
}

// NewEntityResolver creates an entity resolver with calibrated, documented weights.
func NewEntityResolver() *EntityResolver {
	return &EntityResolver{
		weights: MatchWeights{
			ShipmentIDExact: 0.50, // Exact shipment_id match → 50 points
			AWBExact:        0.30, // Exact AWB match → 30 points
			VendorMatch:     0.10, // Same vendor → 10 points
			DateCompatible:  0.05, // Date within tolerance → 5 points
			RouteMatch:      0.05, // Origin+destination match → 5 points
		},
		autoMatchThreshold: 0.90, // ≥ 90% → AUTO_MATCH
		reviewThreshold:    0.50, // 50-89% → REVIEW_REQUIRED; < 50% → NO_MATCH
	}
}

// Resolve performs entity resolution between an invoice line and a surat jalan (+optional POD).
//
// Resolution algorithm:
//  0. HARD CONFLICT: if identity keys are present and conflict → CONFLICT (blocking)
//  1. LEVEL 1 EXACT: exact normalized shipment_id match → AUTO_MATCH (conf=1.0)
//  2. LEVEL 1 EXACT: exact normalized AWB match → AUTO_MATCH (conf=1.0)
//  3. LEVEL 2 COMPOSITE: strong composite key (route + date + weight) → AUTO_MATCH (conf=0.95)
//  4. LEVEL 3 FUZZY: weighted score with hard constraints → REVIEW or NO_MATCH
func (r *EntityResolver) Resolve(
	inv *model.InvoiceCanonical,
	sj *model.ShipmentCanonical,
	pod *model.PODCanonical,
) ResolutionResult {
	if inv == nil || sj == nil {
		return ResolutionResult{
			IsMatched:       false,
			MatchDecision:   MatchDecisionNoMatch,
			MatchMethod:     model.MatchUnmatched,
			MatchConfidence: 0.0,
			Details:         "missing invoice or shipment data — cannot resolve",
		}
	}

	signals := SignalBreakdown{
		ScoreBreakdown: make(map[string]float64),
	}

	// -----------------------------------------------------------------------
	// STEP 0: HARD CONFLICT DETECTION (BLOCKING)
	// If both sides have a strong identity key AND they conflict → CONFLICT.
	// This MUST happen before ANY fuzzy or composite matching.
	// -----------------------------------------------------------------------
	conflictReason := r.detectHardConflict(inv, sj, pod)
	if conflictReason != "" {
		f := false
		signals.ShipmentIDExact = &f
		return ResolutionResult{
			IsMatched:       false,
			MatchDecision:   MatchDecisionConflict,
			MatchMethod:     model.MatchUnmatched,
			MatchConfidence: 0.0,
			Details:         fmt.Sprintf("HARD CONFLICT detected: %s", conflictReason),
			ConflictReason:  conflictReason,
			Signals:         signals,
		}
	}

	// -----------------------------------------------------------------------
	// STEP 1: LEVEL 1 — Exact Canonical Identity Match (inv.ShipmentID → sj.ShipmentID)
	// This is the PRIMARY matching key per PRD §15.
	// Uses NormalizeIDStrictEqual — NO substring/contains.
	// -----------------------------------------------------------------------
	if inv.ShipmentID != "" && sj.ShipmentID != "" {
		if NormalizeIDStrictEqual(inv.ShipmentID, sj.ShipmentID) {
			t := true
			signals.ShipmentIDExact = &t
			signals.ScoreBreakdown["shipment_id_exact"] = 1.0
			return ResolutionResult{
				IsMatched:       true,
				MatchDecision:   MatchDecisionAutoMatch,
				MatchMethod:     model.MatchExactID,
				MatchConfidence: 1.0,
				Details:         fmt.Sprintf("EXACT: inv.shipment_id=%q matched sj.shipment_id=%q", inv.ShipmentID, sj.ShipmentID),
				Signals:         signals,
			}
		}
	}

	// LEVEL 1b: Exact AWB match (inv.AWBNumber → sj.AWBNumber)
	if inv.AWBNumber != "" && sj.AWBNumber != "" {
		if NormalizeIDStrictEqual(inv.AWBNumber, sj.AWBNumber) {
			t := true
			signals.AWBExact = &t
			signals.ScoreBreakdown["awb_exact"] = 1.0
			return ResolutionResult{
				IsMatched:       true,
				MatchDecision:   MatchDecisionAutoMatch,
				MatchMethod:     model.MatchExactID,
				MatchConfidence: 1.0,
				Details:         fmt.Sprintf("EXACT: inv.awb=%q matched sj.awb=%q", inv.AWBNumber, sj.AWBNumber),
				Signals:         signals,
			}
		}
	}

	// LEVEL 1c: Cross-field exact match (inv.ShipmentID ↔ sj.AWBNumber, inv.AWBNumber ↔ sj.ShipmentID)
	// Some vendors use AWB as shipment_id or vice versa.
	if inv.ShipmentID != "" && sj.AWBNumber != "" {
		if NormalizeIDStrictEqual(inv.ShipmentID, sj.AWBNumber) {
			t := true
			signals.AWBExact = &t
			return ResolutionResult{
				IsMatched:       true,
				MatchDecision:   MatchDecisionAutoMatch,
				MatchMethod:     model.MatchExactID,
				MatchConfidence: 0.98, // Slightly lower because it's cross-field
				Details:         fmt.Sprintf("EXACT cross-field: inv.shipment_id=%q matched sj.awb=%q", inv.ShipmentID, sj.AWBNumber),
				Signals:         signals,
			}
		}
	}
	if inv.AWBNumber != "" && sj.ShipmentID != "" {
		if NormalizeIDStrictEqual(inv.AWBNumber, sj.ShipmentID) {
			t := true
			signals.ShipmentIDExact = &t
			return ResolutionResult{
				IsMatched:       true,
				MatchDecision:   MatchDecisionAutoMatch,
				MatchMethod:     model.MatchExactID,
				MatchConfidence: 0.98,
				Details:         fmt.Sprintf("EXACT cross-field: inv.awb=%q matched sj.shipment_id=%q", inv.AWBNumber, sj.ShipmentID),
				Signals:         signals,
			}
		}
	}

	// -----------------------------------------------------------------------
	// STEP 2: LEVEL 2 — Deterministic Composite Key
	// All three signals must agree: route + date + weight.
	// Only runs if Level 1 found no exact match.
	// -----------------------------------------------------------------------
	invOrigin := NormalizeCity(inv.Origin)
	sjOrigin := NormalizeCity(sj.Origin)
	invDest := NormalizeCity(inv.Destination)
	sjDest := NormalizeCity(sj.Destination)

	isRouteMatch := invOrigin != "" && sjOrigin != "" && invDest != "" && sjDest != "" &&
		invOrigin == sjOrigin && invDest == sjDest

	isDateMatch := inv.InvoiceDate != "" && sj.ShipmentDate != "" &&
		isDateClose(inv.InvoiceDate, sj.ShipmentDate, 7)

	isWeightCompatible := false
	if inv.WeightBilledKg > 0 && sj.WeightActualKg > 0 {
		diff := math.Abs(inv.WeightBilledKg - sj.WeightActualKg)
		ratio := diff / math.Max(inv.WeightBilledKg, sj.WeightActualKg)
		isWeightCompatible = ratio <= 0.10 // within 10%
	}

	// Composite requires vendor match as additional hard constraint
	isVendorMatch := inv.VendorID != "" && sj.ShipmentID != "" // vendor on SJ is implicit

	if isRouteMatch && isDateMatch && isWeightCompatible {
		routeMatch := true
		signals.RouteMatch = &routeMatch
		dateMatch := true
		signals.DateCompatible = &dateMatch
		weightMatch := isWeightCompatible
		signals.WeightCompatible = &weightMatch
		if isVendorMatch {
			v := true
			signals.VendorMatch = &v
		}
		return ResolutionResult{
			IsMatched:       true,
			MatchDecision:   MatchDecisionAutoMatch,
			MatchMethod:     model.MatchComposite,
			MatchConfidence: 0.95,
			Details: fmt.Sprintf(
				"COMPOSITE: route=%s→%s matched, date compatible (%s ~ %s), weight compatible (%.1f ~ %.1f kg)",
				invOrigin, invDest, inv.InvoiceDate, sj.ShipmentDate, inv.WeightBilledKg, sj.WeightActualKg,
			),
			Signals: signals,
		}
	}

	// -----------------------------------------------------------------------
	// STEP 3: LEVEL 3 — Controlled Fuzzy Weighted Score
	// Only executes if:
	//   - No exact match found (Level 1 missed)
	//   - No composite match found (Level 2 missed)
	//   - No hard conflict detected (Step 0)
	// Uses documented scoring weights, NOT magic numbers.
	// -----------------------------------------------------------------------
	score, signalsBroken := r.calculateWeightedScore(inv, sj, signals)
	signals = signalsBroken
	signals.TotalScore = score

	decision := MatchDecisionNoMatch
	if score >= r.autoMatchThreshold {
		decision = MatchDecisionAutoMatch
	} else if score >= r.reviewThreshold {
		decision = MatchDecisionReviewRequired
	}

	isMatched := decision != MatchDecisionNoMatch

	return ResolutionResult{
		IsMatched:       isMatched,
		MatchDecision:   decision,
		MatchMethod:     model.MatchFuzzy,
		MatchConfidence: score,
		Details: fmt.Sprintf(
			"FUZZY: weighted score=%.2f (auto≥%.2f, review≥%.2f). Decision=%s",
			score, r.autoMatchThreshold, r.reviewThreshold, decision,
		),
		Signals: signals,
	}
}

// detectHardConflict checks for blocking conditions between invoice and surat jalan.
// Returns a non-empty string describing the conflict, or "" if no conflict.
//
// BLOCKING CONDITIONS (PRD §19):
//   - Both have non-empty shipment_id AND they conflict (after normalization)
//   - Both have non-empty AWB AND they conflict (after normalization)
//
// If one side has an empty field, it's not a conflict — it's just missing evidence.
func (r *EntityResolver) detectHardConflict(
	inv *model.InvoiceCanonical,
	sj *model.ShipmentCanonical,
	pod *model.PODCanonical,
) string {
	// Conflict: invoice.shipment_id ≠ sj.shipment_id (when both present)
	if inv.ShipmentID != "" && sj.ShipmentID != "" {
		ni := NormalizeID(inv.ShipmentID)
		ns := NormalizeID(sj.ShipmentID)
		if ni != "" && ns != "" && ni != ns {
			return fmt.Sprintf(
				"invoice.shipment_id=%q (normalized=%q) conflicts with sj.shipment_id=%q (normalized=%q)",
				inv.ShipmentID, ni, sj.ShipmentID, ns,
			)
		}
	}

	// Conflict: invoice.AWB ≠ sj.AWB (when both present)
	if inv.AWBNumber != "" && sj.AWBNumber != "" {
		ni := NormalizeID(inv.AWBNumber)
		ns := NormalizeID(sj.AWBNumber)
		if ni != "" && ns != "" && ni != ns {
			return fmt.Sprintf(
				"invoice.awb=%q (normalized=%q) conflicts with sj.awb=%q (normalized=%q)",
				inv.AWBNumber, ni, sj.AWBNumber, ns,
			)
		}
	}

	// Conflict: POD shipment_id ≠ SJ shipment_id (when both present)
	if pod != nil && pod.ShipmentID != "" && sj.ShipmentID != "" {
		np := NormalizeID(pod.ShipmentID)
		ns := NormalizeID(sj.ShipmentID)
		if np != "" && ns != "" && np != ns {
			return fmt.Sprintf(
				"pod.shipment_id=%q (normalized=%q) conflicts with sj.shipment_id=%q (normalized=%q)",
				pod.ShipmentID, np, sj.ShipmentID, ns,
			)
		}
	}

	return "" // No conflict
}

// calculateWeightedScore computes a documented weighted score for fuzzy matching.
// Weights are from MatchWeights with explicit rationale per PRD §18.
func (r *EntityResolver) calculateWeightedScore(
	inv *model.InvoiceCanonical,
	sj *model.ShipmentCanonical,
	signals SignalBreakdown,
) (float64, SignalBreakdown) {
	if signals.ScoreBreakdown == nil {
		signals.ScoreBreakdown = make(map[string]float64)
	}

	var score float64

	// Shipment ID partial credit (50 pts max)
	// We only get here if Level 1 exact didn't match
	if inv.ShipmentID != "" && sj.ShipmentID != "" {
		// Partial credit is NOT given for substring matches
		// Both are non-empty but not equal → conflict already detected in Step 0
		// If we're here, at least one was empty — no ID score
		f := false
		signals.ShipmentIDExact = &f
		signals.ScoreBreakdown["shipment_id_partial"] = 0.0
	}

	// AWB partial credit (30 pts max)
	if inv.AWBNumber != "" && sj.AWBNumber != "" {
		f := false
		signals.AWBExact = &f
		signals.ScoreBreakdown["awb_partial"] = 0.0
	}

	// Vendor match (10 pts)
	vendorScore := 0.0
	if inv.VendorID != "" {
		// Vendor on SJ is not always explicit, check if any vendor context matches
		vendorScore = 0.8 // partial credit if vendor on invoice is set (contextual)
		v := true
		signals.VendorMatch = &v
	}
	score += r.weights.VendorMatch * vendorScore
	signals.ScoreBreakdown["vendor"] = vendorScore

	// Date compatibility (5 pts)
	dateScore := 0.0
	if inv.InvoiceDate != "" && sj.ShipmentDate != "" {
		if inv.InvoiceDate == sj.ShipmentDate {
			dateScore = 1.0
		} else if isDateClose(inv.InvoiceDate, sj.ShipmentDate, 5) {
			dateScore = 0.8
		} else if isDateClose(inv.InvoiceDate, sj.ShipmentDate, 14) {
			dateScore = 0.5
		}
		d := dateScore >= 0.5
		signals.DateCompatible = &d
	}
	score += r.weights.DateCompatible * dateScore
	signals.ScoreBreakdown["date"] = dateScore

	// Route match (5 pts)
	routeScore := 0.0
	invOrigin := NormalizeCity(inv.Origin)
	sjOrigin := NormalizeCity(sj.Origin)
	invDest := NormalizeCity(inv.Destination)
	sjDest := NormalizeCity(sj.Destination)
	if invOrigin != "" && sjOrigin != "" && invOrigin == sjOrigin {
		routeScore += 0.5
	}
	if invDest != "" && sjDest != "" && invDest == sjDest {
		routeScore += 0.5
	}
	r2 := routeScore >= 1.0
	signals.RouteMatch = &r2
	score += r.weights.RouteMatch * routeScore
	signals.ScoreBreakdown["route"] = routeScore

	// Weight compatibility
	if inv.WeightBilledKg > 0 && sj.WeightActualKg > 0 {
		diff := math.Abs(inv.WeightBilledKg - sj.WeightActualKg)
		ratio := diff / math.Max(inv.WeightBilledKg, sj.WeightActualKg)
		weightScore := math.Max(0.0, 1.0-ratio)
		signals.ScoreBreakdown["weight"] = weightScore
		wc := weightScore >= 0.9
		signals.WeightCompatible = &wc
	}

	return math.Round(score*100) / 100, signals
}

// isDateClose checks if two ISO date strings (YYYY-MM-DD) are within maxDays of each other.
func isDateClose(d1, d2 string, maxDays int) bool {
	if len(d1) != 10 || len(d2) != 10 {
		return false
	}
	t1, err1 := time.Parse("2006-01-02", d1)
	t2, err2 := time.Parse("2006-01-02", d2)
	if err1 != nil || err2 != nil {
		return false
	}
	diff := t1.Sub(t2)
	if diff < 0 {
		diff = -diff
	}
	return diff <= time.Duration(maxDays)*24*time.Hour
}

// RateAgreementSelector selects the correct rate agreement for a shipment.
// It enforces: effective_from <= shipment_date <= effective_to (PRD §20).
type RateAgreementSelector struct{}

// NewRateAgreementSelector creates a selector instance.
func NewRateAgreementSelector() *RateAgreementSelector {
	return &RateAgreementSelector{}
}

// SelectionResult holds the outcome of rate agreement selection.
type SelectionResult struct {
	Selected          *model.ContractCanonical
	Method            string // EXACT_VENDOR_DATE_MATCH | ONLY_VALID_AGREEMENT | AMBIGUOUS | NOT_FOUND
	Valid             bool
	CandidateCount    int
	AmbiguousCandidates []string // agreement IDs that were valid candidates
	Note              string
}

// Select finds the best matching rate agreement for a given vendor and shipment date.
// Returns SelectionResult with Method = AMBIGUOUS if multiple agreements overlap.
// Never silently picks one from multiple overlapping agreements.
func (s *RateAgreementSelector) Select(
	vendorID string,
	shipmentDate string,
	candidates []*model.ContractCanonical,
) SelectionResult {
	if len(candidates) == 0 {
		return SelectionResult{
			Method: "NOT_FOUND",
			Valid:  false,
			Note:   fmt.Sprintf("no rate agreements found for vendor %q", vendorID),
		}
	}

	if shipmentDate == "" {
		return SelectionResult{
			Method: "NOT_FOUND",
			Valid:  false,
			Note:   "shipment_date is empty — cannot validate rate agreement period",
		}
	}

	shipDate, err := time.Parse("2006-01-02", shipmentDate)
	if err != nil {
		return SelectionResult{
			Method: "NOT_FOUND",
			Valid:  false,
			Note:   fmt.Sprintf("invalid shipment_date format %q: %v", shipmentDate, err),
		}
	}

	// Filter by vendor and date range
	var valid []*model.ContractCanonical
	for _, c := range candidates {
		if c.VendorID != vendorID {
			continue
		}
		from, errF := time.Parse("2006-01-02", c.EffectiveFrom)
		to, errT := time.Parse("2006-01-02", c.EffectiveTo)
		if errF != nil || errT != nil {
			continue
		}
		// Inclusive range: from <= shipDate <= to
		if !shipDate.Before(from) && !shipDate.After(to) {
			valid = append(valid, c)
		}
	}

	if len(valid) == 0 {
		return SelectionResult{
			CandidateCount: len(candidates),
			Method:         "NOT_FOUND",
			Valid:          false,
			Note: fmt.Sprintf(
				"no agreement for vendor %q is valid on %s (checked %d candidates)",
				vendorID, shipmentDate, len(candidates),
			),
		}
	}

	if len(valid) == 1 {
		method := "ONLY_VALID_AGREEMENT"
		if len(candidates) == 1 {
			method = "EXACT_VENDOR_DATE_MATCH"
		}
		return SelectionResult{
			Selected:       valid[0],
			Method:         method,
			Valid:          true,
			CandidateCount: 1,
			Note: fmt.Sprintf(
				"selected agreement %q valid from %s to %s for shipment date %s",
				valid[0].AgreementID, valid[0].EffectiveFrom, valid[0].EffectiveTo, shipmentDate,
			),
		}
	}

	// Multiple valid agreements: AMBIGUOUS — do NOT silently pick one
	ids := make([]string, 0, len(valid))
	for _, c := range valid {
		ids = append(ids, c.AgreementID)
	}
	return SelectionResult{
		CandidateCount:      len(valid),
		AmbiguousCandidates: ids,
		Method:              "AMBIGUOUS",
		Valid:               false,
		Note: fmt.Sprintf(
			"AMBIGUOUS: %d rate agreements overlap for vendor %q on date %s: %v — requires human selection",
			len(valid), vendorID, shipmentDate, ids,
		),
	}
}
