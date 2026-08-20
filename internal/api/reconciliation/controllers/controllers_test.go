package controllers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	apireconcile "backend-api/internal/api/reconciliation"
	"backend-api/internal/api/reconciliation/controllers"
	"backend-api/internal/model"
	reconcilesvc "backend-api/internal/service/reconciliation"
	"github.com/gin-gonic/gin"
)

func setupTestRouter() (*gin.Engine, *apireconcile.Resource) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	engine := reconcilesvc.NewEngine()
	repo := apireconcile.NewMemoryRepository()
	resource := apireconcile.NewResource(engine, repo)

	reconcileCtrl := controllers.NewReconcileController(resource)
	queueCtrl := controllers.NewQueueController(resource)
	detailCtrl := controllers.NewDetailController(resource)
	decisionCtrl := controllers.NewDecisionController(resource)
	disputeCtrl := controllers.NewDisputePackageController(resource)

	v1 := router.Group("/api/v1/reconcile")
	{
		v1.POST("", reconcileCtrl.Handle)
		v1.GET("/queue", queueCtrl.Handle)
		v1.GET("/:id", detailCtrl.Handle)
		v1.POST("/:id/decision", decisionCtrl.Handle)
		v1.GET("/:id/dispute-package", disputeCtrl.Handle)
	}

	return router, resource
}

func TestControllers_CompleteSuite(t *testing.T) {
	router, resource := setupTestRouter()

	reqPayload := apireconcile.ReconcileRequestDTO{
		Invoice: model.InvoiceCanonical{
			InvoiceID:        "INV-CTRL-001",
			InvoiceNumber:    "INV/2026/08/CTRL",
			InvoiceDate:      "2026-08-10",
			VendorID:         "3PL-LOG-001",
			ServiceType:      "REGULAR",
			Origin:           "SURABAYA",
			Destination:      "JAKARTA",
			WeightBilledKg:   84.33,
			BaseChargeBilled: 850000, // Overcharge
			BilledAmount:     943500,
			TaxBilled:        93500,
			Currency:         "IDR",
		},
		Shipment: model.ShipmentCanonical{
			SuratJalanNumber: "SJ-CTRL-001",
			ShipmentID:       "SHP-CTRL-001",
			AWBNumber:        "AWB123456",
			ShipmentDate:     "2026-08-08",
			Origin:           "SURABAYA",
			Destination:      "JAKARTA",
			WeightActualKg:   84.33,
			TotalPackages:    16,
		},
		POD: &model.PODCanonical{
			PODNumber:           "POD-CTRL-001",
			DeliveryStatus:      "DELIVERED",
			DeliveryDate:        "2026-08-10",
			SignaturePresent:    true,
			SignatureConfidence: 0.95,
			StampPresent:        true,
		},
		Contract: model.ContractCanonical{
			AgreementID:                    "CTR-3PL-LOG-001-2026",
			VendorID:                       "3PL-LOG-001",
			EffectiveFrom:                  "2026-01-01",
			EffectiveTo:                    "2026-12-31",
			BaseRate:                       480000,
			ApplicableFuelSurchargePercent: 14.0,
			DisputeWindowDays:              45,
		},
	}

	body, _ := json.Marshal(reqPayload)

	// 1. POST /api/v1/reconcile -> 200 OK
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/reconcile", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200 on /reconcile, got %d: %s", w.Code, w.Body.String())
	}

	var event model.ShipmentEvent
	if err := json.Unmarshal(w.Body.Bytes(), &event); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if event.Reconciliation.Status != model.StatusException {
		t.Errorf("Expected EXCEPTION status, got %s", event.Reconciliation.Status)
	}

	// 2. GET /api/v1/reconcile/queue -> 200 OK
	wQ := httptest.NewRecorder()
	reqQ, _ := http.NewRequest(http.MethodGet, "/api/v1/reconcile/queue?status=EXCEPTION", nil)
	router.ServeHTTP(wQ, reqQ)

	if wQ.Code != http.StatusOK {
		t.Fatalf("Expected 200 on /reconcile/queue, got %d", wQ.Code)
	}
	var qResp apireconcile.QueueResponseDTO
	if err := json.Unmarshal(wQ.Body.Bytes(), &qResp); err != nil {
		t.Fatalf("Failed to decode queue response: %v", err)
	}
	if len(qResp.Items) != 1 {
		t.Errorf("Expected 1 item in filtered queue, got %d", len(qResp.Items))
	}

	// 3. GET /api/v1/reconcile/:id -> 200 OK
	wD := httptest.NewRecorder()
	reqD, _ := http.NewRequest(http.MethodGet, "/api/v1/reconcile/"+event.EventID, nil)
	router.ServeHTTP(wD, reqD)

	if wD.Code != http.StatusOK {
		t.Fatalf("Expected 200 on /reconcile/:id, got %d", wD.Code)
	}

	// 4. POST /api/v1/reconcile/:id/decision (Validation failure: missing reason) -> 400 Bad Request
	decFail := apireconcile.DecisionRequestDTO{
		Action: "DISPUTE",
		Reason: "", // Empty reason
	}
	failBody, _ := json.Marshal(decFail)
	wDecF := httptest.NewRecorder()
	reqDecF, _ := http.NewRequest(http.MethodPost, "/api/v1/reconcile/"+event.EventID+"/decision", bytes.NewReader(failBody))
	reqDecF.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(wDecF, reqDecF)

	if wDecF.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 Bad Request for missing reason, got %d", wDecF.Code)
	}

	// 5. POST /api/v1/reconcile/:id/decision (Valid dispute) -> 200 OK
	decValid := apireconcile.DecisionRequestDTO{
		Action:       "DISPUTE",
		ReviewerID:   "AP-USER-001",
		ReviewerRole: "AP_MANAGER",
		Reason:       "Overcharged rate Rp 850,000 vs contracted Rp 480,000",
	}
	validBody, _ := json.Marshal(decValid)
	wDecV := httptest.NewRecorder()
	reqDecV, _ := http.NewRequest(http.MethodPost, "/api/v1/reconcile/"+event.EventID+"/decision", bytes.NewReader(validBody))
	reqDecV.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(wDecV, reqDecV)

	if wDecV.Code != http.StatusOK {
		t.Fatalf("Expected 200 on valid decision, got %d: %s", wDecV.Code, wDecV.Body.String())
	}

	// 6. GET /api/v1/reconcile/:id/dispute-package -> 200 OK
	wDisp := httptest.NewRecorder()
	reqDisp, _ := http.NewRequest(http.MethodGet, "/api/v1/reconcile/"+event.EventID+"/dispute-package", nil)
	router.ServeHTTP(wDisp, reqDisp)

	if wDisp.Code != http.StatusOK {
		t.Fatalf("Expected 200 on /dispute-package, got %d: %s", wDisp.Code, wDisp.Body.String())
	}
	var dispPkg apireconcile.DisputePackageDTO
	if err := json.Unmarshal(wDisp.Body.Bytes(), &dispPkg); err != nil {
		t.Fatalf("Failed to decode dispute package: %v", err)
	}
	if dispPkg.ReviewerAction != "DISPUTE" {
		t.Errorf("Expected ReviewerAction DISPUTE in package, got %s", dispPkg.ReviewerAction)
	}

	// 7. GET /api/v1/reconcile/non-existent-id -> 404 Not Found
	w404 := httptest.NewRecorder()
	req404, _ := http.NewRequest(http.MethodGet, "/api/v1/reconcile/NON-EXISTENT-ID", nil)
	router.ServeHTTP(w404, req404)
	if w404.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for unknown ID, got %d", w404.Code)
	}

	_ = resource
}
