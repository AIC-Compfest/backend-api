package contract

import "time"

// RateAgreement represents a contractual rate card record from Supabase
type RateAgreement struct {
	ID                   string                 `json:"id"`
	DocumentID           string                 `json:"document_id,omitempty"`
	AgreementID          string                 `json:"agreement_id"`
	VendorID             string                 `json:"vendor_id"`
	VendorName           string                 `json:"vendor_name"`
	EffectiveFrom        string                 `json:"effective_from"`
	EffectiveTo          string                 `json:"effective_to"`
	Currency             string                 `json:"currency"`
	FuelSurchargePercent float64                `json:"fuel_surcharge_percent"`
	MinimumCharge        float64                `json:"minimum_charge"`
	DisputeWindowDays    int                    `json:"dispute_window_days"`
	EvidenceJSON         map[string]interface{} `json:"evidence_json,omitempty"`
	CreatedAt            time.Time              `json:"created_at"`
}

// ContractSummaryDTO formatted for frontend dashboard display
type ContractSummaryDTO struct {
	ID               string  `json:"id"`
	AgreementID      string  `json:"agreement_id"`
	VendorID         string  `json:"vendor_id"`
	VendorName       string  `json:"vendor_name"`
	EffectiveFrom    string  `json:"effective_from"`
	EffectiveTo      string  `json:"effective_to"`
	EffectiveRange   string  `json:"effective_range"`
	FuelSurchargeCap string  `json:"fuel_surcharge_cap"`
	DisputeWindow    string  `json:"dispute_window"`
	MinimumCharge    float64 `json:"minimum_charge"`
	Status           string  `json:"status"`
}
