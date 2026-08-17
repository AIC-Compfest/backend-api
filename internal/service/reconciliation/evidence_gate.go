package reconciliation

import (
	"github.com/AIC-Compfest/backend-api/internal/model"
)

// EvidenceQualityConfig holds threshold parameters for evidence evaluation.
type EvidenceQualityConfig struct {
	MinConfidenceCritical float64 // Minimum confidence for critical financial & entity resolution fields (default: 0.65)
	MinConfidenceStandard float64 // Minimum confidence for standard fields (default: 0.50)
	PODQuantityThreshold  float64 // Minimum confidence for POD quantity matching (default: 0.60)
}

// DefaultEvidenceQualityConfig provides standard baseline thresholds.
func DefaultEvidenceQualityConfig() EvidenceQualityConfig {
	return EvidenceQualityConfig{
		MinConfidenceCritical: 0.65,
		MinConfidenceStandard: 0.50,
		PODQuantityThreshold:  0.60,
	}
}

// EvidenceQualityEvaluator assesses extraction reliability before deterministic comparison (PRD §18, §60).
type EvidenceQualityEvaluator struct {
	cfg EvidenceQualityConfig
}

// NewEvidenceQualityEvaluator creates a new evaluator instance.
func NewEvidenceQualityEvaluator(cfg ...EvidenceQualityConfig) *EvidenceQualityEvaluator {
	c := DefaultEvidenceQualityConfig()
	if len(cfg) > 0 {
		c = cfg[0]
	}
	return &EvidenceQualityEvaluator{cfg: c}
}

// EvaluationResult represents the quality outcome of an extracted field.
type EvaluationResult struct {
	IsReliable   bool
	Confidence   float64
	Reason       string
	Evidence     *model.EvidenceSource
	RequiresGate bool
}

// EvaluateField checks whether an ExtractedField satisfies quality requirements.
func (e *EvidenceQualityEvaluator) EvaluateField(f *model.ExtractedField, isCritical bool) EvaluationResult {
	if f == nil || f.Value == nil || f.IsUnknown {
		var src *model.EvidenceSource
		if f != nil {
			src = &f.Source
		}
		return EvaluationResult{
			IsReliable:   false,
			Confidence:   0.0,
			Reason:       "field value is missing or marked unknown by extraction pipeline",
			Evidence:     src,
			RequiresGate: true,
		}
	}

	threshold := e.cfg.MinConfidenceStandard
	if isCritical {
		threshold = e.cfg.MinConfidenceCritical
	}

	if f.Confidence < threshold {
		return EvaluationResult{
			IsReliable:   false,
			Confidence:   f.Confidence,
			Reason:       "confidence below quality threshold",
			Evidence:     &f.Source,
			RequiresGate: true,
		}
	}

	return EvaluationResult{
		IsReliable:   true,
		Confidence:   f.Confidence,
		Reason:       "sufficient confidence and valid evidence",
		Evidence:     &f.Source,
		RequiresGate: false,
	}
}

// EvaluatePODQuantity specifically handles the high-risk quantity_received extraction.
func (e *EvidenceQualityEvaluator) EvaluatePODQuantity(pod *model.PODCanonical, sj *model.ShipmentCanonical) (isReliable bool, reason string, src *model.EvidenceSource) {
	if pod == nil || pod.QuantityReceived == nil {
		var ev *model.EvidenceSource
		if pod != nil {
			if s, ok := pod.FieldEvidence["quantity_received"]; ok {
				ev = &s
			}
		}
		return false, "POD quantity_received is not present on physical receipt", ev
	}

	// If quantity is extracted, check if evidence exists
	if s, ok := pod.FieldEvidence["quantity_received"]; ok {
		return true, "POD quantity is explicitly extracted with valid evidence", &s
	}

	return true, "POD quantity is present", nil
}

// EvaluatePODDeliveryDate handles date quality validation.
func (e *EvidenceQualityEvaluator) EvaluatePODDeliveryDate(pod *model.PODCanonical) (isReliable bool, reason string, src *model.EvidenceSource) {
	if pod == nil || pod.DeliveryDate == "" {
		var ev *model.EvidenceSource
		if pod != nil {
			if s, ok := pod.FieldEvidence["delivery_date"]; ok {
				ev = &s
			}
		}
		return false, "POD delivery_date is missing or unextracted", ev
	}

	if s, ok := pod.FieldEvidence["delivery_date"]; ok {
		return true, "POD delivery_date is valid", &s
	}

	return true, "POD delivery_date is present", nil
}
