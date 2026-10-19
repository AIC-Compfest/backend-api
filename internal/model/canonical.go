package model

import (
	"time"
)

// ExtractionMethod represents the extraction provenance.
type ExtractionMethod string

const (
	MethodOCR       ExtractionMethod = "ocr"
	MethodLayout    ExtractionMethod = "layout"
	MethodVLM       ExtractionMethod = "vlm"
	MethodRuleBased ExtractionMethod = "rule_based"
	MethodManual    ExtractionMethod = "manual"
)

// BoundingBox holds normalized coordinates [0.0, 1.0].
type BoundingBox struct {
	X1 float64 `json:"x1"`
	Y1 float64 `json:"y1"`
	X2 float64 `json:"x2"`
	Y2 float64 `json:"y2"`
}

// EvidenceSource points to the physical provenance of an extracted value (PRD §18).
type EvidenceSource struct {
	DocumentID string       `json:"document_id"`
	Page       int          `json:"page"`
	BBox       *BoundingBox `json:"bbox,omitempty"`
}

// ExtractedField is the canonical unit emitted by Phase 6 AI Pipeline.
type ExtractedField struct {
	Field            string           `json:"field"`
	Value            any              `json:"value"`
	Confidence       float64          `json:"confidence"`
	ConfidenceSource string           `json:"confidence_source,omitempty"`
	Source           EvidenceSource   `json:"source"`
	Method           ExtractionMethod `json:"method"`
	IsUnknown        bool             `json:"is_unknown"`
}

// ExtractionResponse represents Phase 6 payload for a document.
type ExtractionResponse struct {
	DocumentID          string           `json:"document_id"`
	DocumentType        string           `json:"document_type"`
	Fields              []ExtractedField `json:"fields"`
	OCREngineUsed       string           `json:"ocr_engine_used,omitempty"`
	FallbackTriggered   bool             `json:"fallback_triggered,omitempty"`
	Status              string           `json:"status"`
	ErrorCode           string           `json:"error_code,omitempty"`
	RequiresHumanReview bool             `json:"requires_human_review"`
}

// Helper method to convert Fields list to map for fast lookup
func (r *ExtractionResponse) FieldMap() map[string]ExtractedField {
	m := make(map[string]ExtractedField, len(r.Fields))
	for _, f := range r.Fields {
		m[f.Field] = f
	}
	return m
}

// ---------------------------------------------------------------------------
// Canonical Entities matching schemas/canonical_schema.json
// ---------------------------------------------------------------------------

// InvoiceCanonical represents extracted & normalized facts from 3PL Invoice.
type InvoiceCanonical struct {
	InvoiceID     string `json:"invoice_id"`
	InvoiceNumber string `json:"invoice_number"`
	LineNumber    int    `json:"line_number,omitempty"`
	InvoiceDate   string `json:"invoice_date"` // YYYY-MM-DD

	// PRIMARY entity resolution keys (Level 1 matching — use these FIRST).
	// Entity resolver MUST check these before falling back to InvoiceNumber.
	// ShipmentID is the shipment reference number on the invoice line item.
	ShipmentID string `json:"shipment_id,omitempty"`
	AWBNumber  string `json:"awb_number,omitempty"`

	VendorID     string `json:"vendor_id"`
	VendorName   string `json:"vendor_name,omitempty"`
	CustomerName string `json:"customer_name,omitempty"`
	ServiceType  string `json:"service_type,omitempty"`
	Origin       string `json:"origin,omitempty"`
	Destination  string `json:"destination,omitempty"`

	WeightBilledKg             float64 `json:"weight_billed_kg"`
	BaseChargeBilled           int64   `json:"base_charge_billed"`
	FuelSurchargeBilled        int64   `json:"fuel_surcharge_billed"`
	FuelSurchargePercentBilled float64 `json:"fuel_surcharge_percent_billed"`
	AccessorialChargesBilled   int64   `json:"accessorial_charges_billed"`
	TaxBilled                  int64   `json:"tax_billed"`
	BilledAmount               int64   `json:"billed_amount"`
	Currency                   string  `json:"currency"`

	// Provenance — which document segment produced this invoice record
	DocumentID string `json:"document_id,omitempty"`
	SegmentID  string `json:"segment_id,omitempty"`

	FieldEvidence map[string]EvidenceSource `json:"field_evidence,omitempty"`
}

// ShipmentCanonical represents extracted & normalized facts from Surat Jalan.
type ShipmentCanonical struct {
	SuratJalanNumber string  `json:"surat_jalan_number"`
	ShipmentID       string  `json:"shipment_id"`
	AWBNumber        string  `json:"awb_number,omitempty"`
	ServiceType      string  `json:"service_type,omitempty"`
	ShipmentDate     string  `json:"shipment_date"` // YYYY-MM-DD
	Origin           string  `json:"origin"`
	Destination      string  `json:"destination"`
	WeightActualKg   float64 `json:"weight_actual_kg"`
	TotalPackages    int     `json:"total_packages"`
	ItemDescription  string  `json:"item_description,omitempty"`

	// Provenance
	DocumentID string `json:"document_id,omitempty"`
	SegmentID  string `json:"segment_id,omitempty"`

	FieldEvidence map[string]EvidenceSource `json:"field_evidence,omitempty"`
}

// PODCanonical represents extracted & normalized facts from Proof of Delivery.
type PODCanonical struct {
	PODNumber           string  `json:"pod_number"`
	ShipmentID          string  `json:"shipment_id,omitempty"`
	AWBNumber           string  `json:"awb_number,omitempty"`
	DeliveryStatus      string  `json:"delivery_status"`         // DELIVERED, PARTIALLY_DELIVERED, FAILED, RETURNED, MISSING
	DeliveryDate        string  `json:"delivery_date,omitempty"` // YYYY-MM-DD
	ReceiverName        string  `json:"receiver_name,omitempty"`
	QuantityReceived    *int    `json:"quantity_received,omitempty"`
	SignaturePresent    bool    `json:"signature_present"`
	SignatureConfidence float64 `json:"signature_confidence"`
	StampPresent        bool    `json:"stamp_present"`
	Condition           string  `json:"condition,omitempty"`

	// Provenance
	DocumentID string `json:"document_id,omitempty"`
	SegmentID  string `json:"segment_id,omitempty"`

	FieldEvidence map[string]EvidenceSource `json:"field_evidence,omitempty"`
}

// ContractCanonical represents extracted & normalized rate agreement.
type ContractCanonical struct {
	AgreementID                    string             `json:"agreement_id"`
	VendorID                       string             `json:"vendor_id"`
	EffectiveFrom                  string             `json:"effective_from"` // YYYY-MM-DD
	EffectiveTo                    string             `json:"effective_to"`   // YYYY-MM-DD
	RateUnit                       string             `json:"rate_unit"`      // PER_KG, FLAT
	BaseRate                       int64              `json:"base_rate"`
	ApplicableFuelSurchargePercent float64            `json:"applicable_fuel_surcharge_percent"`
	MinimumCharge                  int64              `json:"minimum_charge,omitempty"`
	DisputeWindowDays              int                `json:"dispute_window_days"`
	RateMatrix                     []RateMatrixEntry  `json:"rate_matrix,omitempty"`
	Surcharges                     map[string]float64 `json:"surcharges,omitempty"`

	// Provenance
	DocumentID string `json:"document_id,omitempty"`
	SegmentID  string `json:"segment_id,omitempty"`

	FieldEvidence map[string]EvidenceSource `json:"field_evidence,omitempty"`
}

// RateMatrixEntry defines specific lane pricing in the contract rate card.
type RateMatrixEntry struct {
	OriginZone      string  `json:"origin_zone"`
	DestinationZone string  `json:"destination_zone"`
	ServiceType     string  `json:"service_type"`
	WeightMinKg     float64 `json:"weight_min_kg"`
	WeightMaxKg     float64 `json:"weight_max_kg"`
	BaseRate        int64   `json:"base_rate"`
	RateType        string  `json:"rate_type"` // PER_KG, FLAT_TRIP
}

// ---------------------------------------------------------------------------
// Reconciliation Result & Discrepancy Models (PRD §30–§31)
// ---------------------------------------------------------------------------

// DiscrepancySeverity level
type DiscrepancySeverity string

const (
	SeverityCritical DiscrepancySeverity = "CRITICAL"
	SeverityWarning  DiscrepancySeverity = "WARNING"
	SeverityInfo     DiscrepancySeverity = "INFO"
)

// DiscrepancyCategory taxonomy
type DiscrepancyCategory string

const (
	CategoryPricing       DiscrepancyCategory = "PRICING"
	CategoryShipment      DiscrepancyCategory = "SHIPMENT"
	CategoryDocumentation DiscrepancyCategory = "DOCUMENTATION"
	CategoryTemporal      DiscrepancyCategory = "TEMPORAL"
	CategoryDuplicate     DiscrepancyCategory = "DUPLICATE"
	CategoryConfidence    DiscrepancyCategory = "CONFIDENCE"
)

// DiscrepancyCode taxonomy matching PRD §31
type DiscrepancyCode string

const (
	// Pricing
	CodeRateOvercharge        DiscrepancyCode = "RATE_OVERCHARGE"
	CodeWrongWeightTier       DiscrepancyCode = "WRONG_WEIGHT_TIER"
	CodeWrongZone             DiscrepancyCode = "WRONG_ZONE"
	CodeWrongFuelSurcharge    DiscrepancyCode = "WRONG_FUEL_SURCHARGE"
	CodeWrongAccessorial      DiscrepancyCode = "WRONG_ACCESSORIAL"
	CodeUnauthorizedFee       DiscrepancyCode = "UNAUTHORIZED_FEE"
	CodeMinimumChargeViolated DiscrepancyCode = "MINIMUM_CHARGE_VIOLATED"
	CodeUnapprovedSurcharge   DiscrepancyCode = "UNAPPROVED_SURCHARGE"
	CodeTaxCalculationError   DiscrepancyCode = "TAX_CALCULATION_ERROR"

	// Shipment
	CodeShipmentNotFound     DiscrepancyCode = "SHIPMENT_NOT_FOUND"
	CodeSuratJalanMissing    DiscrepancyCode = "SURAT_JALAN_MISSING"
	CodeDuplicateShipment    DiscrepancyCode = "DUPLICATE_SHIPMENT"
	CodeWrongDestination     DiscrepancyCode = "WRONG_DESTINATION"
	CodeWrongWeight          DiscrepancyCode = "WRONG_WEIGHT"
	CodeVolumeWeightMismatch DiscrepancyCode = "VOLUME_WEIGHT_MISMATCH"
	CodeWrongService         DiscrepancyCode = "WRONG_SERVICE"
	CodeQuantityMismatch     DiscrepancyCode = "QUANTITY_MISMATCH"

	// Documentation
	CodePODMissing         DiscrepancyCode = "POD_MISSING"
	CodeSignatureMissing   DiscrepancyCode = "SIGNATURE_MISSING"
	CodePODDateMismatch    DiscrepancyCode = "POD_DATE_MISMATCH"
	CodeIncompleteDocument DiscrepancyCode = "INCOMPLETE_DOCUMENT"

	// Temporal & Duplicate
	CodeOutsideContractValidity DiscrepancyCode = "OUTSIDE_CONTRACT_VALIDITY"
	CodeDisputeWindowNearExpiry DiscrepancyCode = "DISPUTE_WINDOW_NEAR_EXPIRY"
	CodeDuplicateInvoice        DiscrepancyCode = "DUPLICATE_INVOICE"

	// Confidence / Evidence Quality Gate
	CodeInsufficientEvidence DiscrepancyCode = "INSUFFICIENT_EVIDENCE"
	CodeLowOCRConfidence     DiscrepancyCode = "LOW_OCR_CONFIDENCE"
	CodeAmbiguousMatch       DiscrepancyCode = "AMBIGUOUS_MATCH"
)

// DiscrepancyItem represents one distinct discrepancy with evidence traceability.
type DiscrepancyItem struct {
	Code            DiscrepancyCode     `json:"code"`
	Category        DiscrepancyCategory `json:"category"`
	Description     string              `json:"description"`
	Severity        DiscrepancySeverity `json:"severity"`
	BilledValue     any                 `json:"billed_value,omitempty"`
	ExpectedValue   any                 `json:"expected_value,omitempty"`
	DeltaAmount     *int64              `json:"delta_amount,omitempty"`
	EvidencePointer *EvidenceSource     `json:"evidence_pointer,omitempty"`
	RemediationHint string              `json:"remediation_hint,omitempty"`
}

// CalculationTrace provides a mathematically reproducible audit log of pricing.
type CalculationTrace struct {
	BaseRateUnit      int64   `json:"base_rate_unit"`
	WeightUsedKg      float64 `json:"weight_used_kg"`
	WeightTier        string  `json:"weight_tier"`
	BaseAmount        int64   `json:"base_amount"`
	FuelPercent       float64 `json:"fuel_percent"`
	FuelAmount        int64   `json:"fuel_amount"`
	AccessorialAmount int64   `json:"accessorial_amount"`
	MinimumCharge     int64   `json:"minimum_charge"`
	PreTaxAmount      int64   `json:"pre_tax_amount"`
	TaxRatePercent    float64 `json:"tax_rate_percent"`
	TaxAmount         int64   `json:"tax_amount"`
	ExpectedTotal     int64   `json:"expected_total"`
	BilledTotal       int64   `json:"billed_total"`
	VarianceAmount    int64   `json:"variance_amount"`
	TraceLog          string  `json:"trace_log"`
}

// PricingResult holds the deterministic calculation output.
type PricingResult struct {
	ExpectedBaseCharge        int64            `json:"expected_base_charge"`
	ExpectedFuelSurcharge     int64            `json:"expected_fuel_surcharge"`
	ExpectedAccessorialCharge int64            `json:"expected_accessorial_charges"`
	ExpectedTax               int64            `json:"expected_tax"`
	ExpectedTotalCharge       int64            `json:"expected_total_charge"`
	DifferenceAmount          int64            `json:"difference_amount"` // Billed - Expected
	RelativeDifferenceRatio   float64          `json:"relative_difference_ratio"`
	IsMaterial                bool             `json:"is_material"`
	Trace                     CalculationTrace `json:"trace"`
}

// ReconciliationStatus represents the final transaction reconciliation decision.
type ReconciliationStatus string

const (
	StatusMatch                ReconciliationStatus = "MATCH"
	StatusException            ReconciliationStatus = "EXCEPTION"
	StatusMissing              ReconciliationStatus = "MISSING"
	StatusDuplicate            ReconciliationStatus = "DUPLICATE"
	StatusInsufficientEvidence ReconciliationStatus = "INSUFFICIENT_EVIDENCE"
	StatusApproved             ReconciliationStatus = "APPROVED"
	StatusDisputed             ReconciliationStatus = "DISPUTED"
	StatusRejected             ReconciliationStatus = "REJECTED"
	StatusResolved             ReconciliationStatus = "RESOLVED"
)

// MatchMethod tracks the entity resolution tier used.
type MatchMethod string

const (
	MatchExactID   MatchMethod = "MATCHED_BY_EXACT_ID"
	MatchComposite MatchMethod = "MATCHED_BY_COMPOSITE"
	MatchFuzzy     MatchMethod = "MATCHED_BY_FUZZY"
	MatchUnmatched MatchMethod = "UNMATCHED"
)

// ReconciliationCheck represents the result of one independent check.
type ReconciliationCheck struct {
	CheckCode   string          `json:"check_code"`
	Status      string          `json:"status"` // PASS, FAIL, UNCERTAIN
	Expected    any             `json:"expected"`
	Actual      any             `json:"actual"`
	Difference  any             `json:"difference,omitempty"`
	Confidence  float64         `json:"confidence"`
	Evidence    *EvidenceSource `json:"evidence,omitempty"`
	Description string          `json:"description,omitempty"`
}

// ShipmentEvent represents the master canonical result (PRD §13, canonical_schema.json).
type ShipmentEvent struct {
	EventID         string                 `json:"event_id"`
	ShipmentID      string                 `json:"shipment_id"`
	VendorID        string                 `json:"vendor_id"`
	MatchMethod     MatchMethod            `json:"match_method"`
	MatchConfidence float64                `json:"match_confidence"`
	Invoice         InvoiceCanonical       `json:"invoice"`
	Shipment        ShipmentCanonical      `json:"shipment"`
	POD             PODCanonical           `json:"pod"`
	Contract        ContractCanonical      `json:"contract"`
	Pricing         PricingResult          `json:"pricing"`
	Reconciliation  ReconciliationDecision `json:"reconciliation"`
	Checks          []ReconciliationCheck  `json:"checks,omitempty"`
	AuditTrail      []AuditTrailItem       `json:"audit_trail,omitempty"`
}

// ReconciliationDecision matches the reconciliation block in canonical_schema.json
type ReconciliationDecision struct {
	Status                 ReconciliationStatus `json:"status"`
	ReasonCodes            []string             `json:"reason_codes"`
	Discrepancies          []DiscrepancyItem    `json:"discrepancies"`
	RequiresHumanReview    bool                 `json:"requires_human_review"`
	PriorityScore          float64              `json:"priority_score"`
	DisputeDeadline        string               `json:"dispute_deadline,omitempty"`
	DaysRemainingToDispute int                  `json:"days_remaining_to_dispute"`
}

// AuditTrailItem records human or system lifecycle changes.
type AuditTrailItem struct {
	Timestamp      time.Time `json:"timestamp"`
	Action         string    `json:"action"`
	ActorID        string    `json:"actor_id"`
	ActorRole      string    `json:"actor_role"`
	PreviousStatus string    `json:"previous_status,omitempty"`
	NewStatus      string    `json:"new_status"`
	Notes          string    `json:"notes,omitempty"`
}
