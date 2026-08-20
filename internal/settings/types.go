package settings

import "time"

// PolicySettings represents financial & reconciliation policy configurations
type PolicySettings struct {
	ID                       string    `json:"id"`
	MaterialityThresholdIDR  float64   `json:"materiality_threshold_idr"`
	MaterialityPercentage    float64   `json:"materiality_percentage"`
	WeightToleranceKg        float64   `json:"weight_tolerance_kg"`
	DisputeAlertWindowDays   int       `json:"dispute_alert_window_days"`
	AutoApproveCleanMatch    bool      `json:"auto_approve_clean_match"`
	EnableStrictZoneMatching bool      `json:"enable_strict_zone_matching"`
	UpdatedAt                time.Time `json:"updated_at"`
	UpdatedBy                string    `json:"updated_by,omitempty"`
}

// UpdateSettingsRequest payload
type UpdateSettingsRequest struct {
	MaterialityThresholdIDR  *float64 `json:"materiality_threshold_idr"`
	MaterialityPercentage    *float64 `json:"materiality_percentage"`
	WeightToleranceKg        *float64 `json:"weight_tolerance_kg"`
	DisputeAlertWindowDays   *int     `json:"dispute_alert_window_days"`
	AutoApproveCleanMatch    *bool    `json:"auto_approve_clean_match"`
	EnableStrictZoneMatching *bool    `json:"enable_strict_zone_matching"`
}
