package document

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"backend-api/internal/database"
	"backend-api/internal/model"
)

// ResourceDB handles database operations for documents and pages
type ResourceDB struct {
	dbClient *database.Client
	mu       sync.RWMutex
	memory   map[string]*Document
	byHash   map[string]string
	order    []string
	pages    map[string][]DocumentPage    // Key: docID -> pages
	segments map[string][]DocumentSegment // Key: docID -> segments
}

func (r *ResourceDB) EnsureIngestBatch(ctx context.Context, batchID string) error {
	if batchID == "" || r.dbClient == nil {
		return nil
	}
	if !isUUID(batchID) {
		batchID = model.NewUUID()
	}
	payload := map[string]interface{}{
		"id":         batchID,
		"batch_code": "UPLOAD-" + batchID,
		"source":     "MULTI_FILE",
		"status":     "PROCESSING",
	}

	var created []map[string]interface{}
	if err := r.dbClient.Insert(ctx, "ingest_batches", payload, &created); err != nil {
		// A retry of a batch must be idempotent.
		var existing []map[string]interface{}
		params := url.Values{}
		params.Set("id", "eq."+batchID)
		if queryErr := r.dbClient.Query(ctx, "ingest_batches", params, &existing); queryErr != nil || len(existing) == 0 {
			return err
		}

	}
	return nil
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

func isUUID(value string) bool {
	return uuidPattern.MatchString(value)
}

func normalizeDocumentStatus(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "QUEUED", "PENDING":
		return "RECEIVED"
	case "PROCESSING", "COMPLETED", "FAILED", "RECEIVED":
		return strings.ToUpper(strings.TrimSpace(status))
	default:
		return "RECEIVED"
	}
}

// NewResourceDB creates a new document ResourceDB instance
func NewResourceDB(dbClient *database.Client) *ResourceDB {
	return &ResourceDB{
		dbClient: dbClient,
		memory:   make(map[string]*Document),
		byHash:   make(map[string]string),
		order:    make([]string, 0),
		pages:    make(map[string][]DocumentPage),
		segments: make(map[string][]DocumentSegment),
	}
}

// FindBySHA256 searches for an existing document by its SHA-256 checksum in Supabase
func (r *ResourceDB) FindBySHA256(ctx context.Context, hash string) (*Document, error) {
	if r.dbClient != nil {
		var results []Document
		params := url.Values{}
		params.Set("sha256_hash", "eq."+hash)
		params.Set("limit", "1")

		err := r.dbClient.Query(ctx, "documents", params, &results)
		if err == nil {
			if len(results) > 0 {
				return &results[0], nil
			}
			return nil, nil
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	id, exists := r.byHash[hash]
	if !exists {
		return nil, nil
	}
	return r.memory[id], nil
}

// FindByID retrieves a document by its UUID from Supabase
func (r *ResourceDB) FindByID(ctx context.Context, id string) (*Document, error) {
	if r.dbClient != nil {
		var results []Document
		params := url.Values{}
		params.Set("id", "eq."+id)
		params.Set("limit", "1")

		err := r.dbClient.Query(ctx, "documents", params, &results)
		if err == nil {
			if len(results) > 0 {
				return &results[0], nil
			}
			return nil, fmt.Errorf("document '%s' not found", id)
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	doc, exists := r.memory[id]
	if !exists {
		return nil, fmt.Errorf("document '%s' not found", id)
	}
	return doc, nil
}

// ListByIngestBatch returns the physical documents that form one user upload.
// It is intentionally scoped by batch ID so finalization cannot accidentally
// combine unrelated uploads merely because they share a vendor.
func (r *ResourceDB) ListByIngestBatch(ctx context.Context, batchID string) ([]Document, error) {
	if batchID == "" {
		return nil, fmt.Errorf("ingest batch id is required")
	}
	if r.dbClient != nil {
		var docs []Document
		params := url.Values{"ingest_batch_id": []string{"eq." + batchID}, "order": []string{"created_at.asc"}}
		if err := r.dbClient.Query(ctx, "documents", params, &docs); err != nil {
			return nil, err
		}
		return docs, nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	docs := make([]Document, 0)
	for _, id := range r.order {
		if doc := r.memory[id]; doc != nil && doc.IngestBatchID == batchID {
			docs = append(docs, *doc)
		}
	}
	return docs, nil
}

// Create inserts a new document record into Supabase DB and local cache
func (r *ResourceDB) Create(ctx context.Context, doc *Document) error {
	if doc.ID == "" {
		doc.ID = model.NewUUID()
	}
	if doc.CreatedAt.IsZero() {
		doc.CreatedAt = time.Now().UTC()
	}
	now := time.Now().UTC()
	doc.UpdatedAt = &now

	if r.dbClient != nil {
		payload := map[string]interface{}{
			"id":                        doc.ID,
			"filename":                  doc.Filename,
			"file_size":                 doc.FileSize,
			"mime_type":                 doc.MimeType,
			"sha256_hash":               doc.Sha256Hash,
			"storage_path":              doc.StoragePath,
			"status":                    normalizeDocumentStatus(doc.Status),
			"page_count":                doc.PageCount,
			"ingest_batch_id":           doc.IngestBatchID,
			"status_approval":           doc.StatusApproval,
			"classification":            doc.Classification,
			"classification_confidence": doc.ClassificationConfidence,
			"created_at":                doc.CreatedAt,
			"updated_at":                doc.UpdatedAt,
		}
		var created []Document
		err := r.dbClient.Insert(ctx, "documents", payload, &created)
		if err != nil {
			fmt.Printf("[ResourceDB.Create] Error inserting document: %v\n", err)
			return err
		}
		if len(created) > 0 {
			*doc = created[0]
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.memory[doc.ID] = doc
	r.byHash[doc.Sha256Hash] = doc.ID
	r.order = append([]string{doc.ID}, r.order...)
	return nil
}

// UpdateStatus updates the processing status and classification of a document
func (r *ResourceDB) UpdateStatus(ctx context.Context, id string, status string, classification string, confidence float64) error {
	now := time.Now().UTC()
	payload := map[string]interface{}{
		"status":                    status,
		"classification":            classification,
		"classification_confidence": confidence,
		"updated_at":                now,
	}

	if r.dbClient != nil {
		var updated []Document
		params := url.Values{}
		params.Set("id", "eq."+id)
		_ = r.dbClient.Update(ctx, "documents", params, payload, &updated)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if doc, exists := r.memory[id]; exists {
		doc.Status = status
		doc.Classification = classification
		doc.ClassificationConfidence = confidence
		doc.UpdatedAt = &now
	}

	return nil
}

// UpdateApprovalStatus updates the approval status of a document
func (r *ResourceDB) UpdateApprovalStatus(ctx context.Context, id string, statusApproval string) error {
	now := time.Now().UTC()
	if r.dbClient != nil {
		if err := r.dbClient.Update(ctx, "documents", url.Values{"id": []string{"eq." + id}}, map[string]interface{}{
			"status_approval": statusApproval,
			"updated_at":      now,
		}, nil); err != nil {
			return err
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if doc, exists := r.memory[id]; exists {
		doc.StatusApproval = statusApproval
		doc.UpdatedAt = &now
	}

	return nil
}

// List retrieves paginated documents from Supabase DB or cache
func (r *ResourceDB) List(ctx context.Context, page, pageSize int, statusFilter string) ([]Document, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	if r.dbClient != nil {
		params := url.Values{}
		params.Set("order", "created_at.desc")
		params.Set("limit", fmt.Sprintf("%d", pageSize))
		params.Set("offset", fmt.Sprintf("%d", offset))

		if statusFilter != "" {
			params.Set("status", "eq."+statusFilter)
		}

		var docs []Document
		err := r.dbClient.Query(ctx, "documents", params, &docs)
		if err == nil {
			countParams := url.Values{}
			if statusFilter != "" {
				countParams.Set("status", "eq."+statusFilter)
			}
			total, _ := r.dbClient.Count(ctx, "documents", countParams)
			if total == 0 && len(docs) > 0 {
				total = int64(len(docs))
			}
			return docs, total, nil
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	var filtered []Document
	for _, id := range r.order {
		doc := r.memory[id]
		if doc != nil {
			if statusFilter == "" || doc.Status == statusFilter {
				filtered = append(filtered, *doc)
			}
		}
	}

	total := int64(len(filtered))
	if offset >= len(filtered) {
		return []Document{}, total, nil
	}

	end := offset + pageSize
	if end > len(filtered) {
		end = len(filtered)
	}

	return filtered[offset:end], total, nil
}

// InsertPage persists an individual page record into document_pages table in Supabase
func (r *ResourceDB) InsertPage(ctx context.Context, p *DocumentPage) error {
	if p.ID == "" {
		p.ID = model.NewUUID()
	}

	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now().UTC()
	}
	if p.Width <= 0 {
		p.Width = 595
	}
	if p.Height <= 0 {
		p.Height = 842
	}
	if p.PageWidthPts <= 0 {
		p.PageWidthPts = float64(p.Width)
	}
	if p.PageHeightPts <= 0 {
		p.PageHeightPts = float64(p.Height)
	}

	if r.dbClient != nil {
		payload := map[string]interface{}{
			"id":               p.ID,
			"document_id":      p.DocumentID,
			"page_number":      p.PageNumber,
			"width":            p.Width,
			"height":           p.Height,
			"ocr_text":         p.OCRText,
			"ocr_confidence":   p.OCRConfidence,
			"layout_json":      p.LayoutJSON,
			"segment_id":       nullableUUID(p.SegmentID),
			"ocr_result":       p.OCRResult,
			"ocr_engine_used":  p.OCREngineUsed,
			"page_width_pts":   p.PageWidthPts,
			"page_height_pts":  p.PageHeightPts,
			"ocr_completed_at": p.OCRCompletedAt,
			"created_at":       p.CreatedAt,
		}
		_ = p

		var created []DocumentPage
		err := r.dbClient.Insert(ctx, "document_pages", payload, &created)
		if err != nil {
			fmt.Printf("[ResourceDB.InsertPage] Error inserting page %d: %v\n", p.PageNumber, err)
			return err
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.pages[p.DocumentID] = append(r.pages[p.DocumentID], *p)
	return nil
}

// ListPages returns all pages associated with a document
func (r *ResourceDB) ListPages(ctx context.Context, docID string) ([]DocumentPage, error) {
	if r.dbClient != nil {
		var pages []DocumentPage
		params := url.Values{}
		params.Set("document_id", "eq."+docID)
		params.Set("order", "page_number.asc")
		if err := r.dbClient.Query(ctx, "document_pages", params, &pages); err == nil && len(pages) > 0 {
			return pages, nil
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	if pages, exists := r.pages[docID]; exists {
		return pages, nil
	}
	return []DocumentPage{}, nil
}

// GetPage retrieves a specific page by document ID and page number
func (r *ResourceDB) GetPage(ctx context.Context, docID string, pageNumber int) (*DocumentPage, error) {
	if r.dbClient != nil {
		var pages []DocumentPage
		params := url.Values{}
		params.Set("document_id", "eq."+docID)
		params.Set("page_number", fmt.Sprintf("eq.%d", pageNumber))
		params.Set("limit", "1")
		if err := r.dbClient.Query(ctx, "document_pages", params, &pages); err == nil && len(pages) > 0 {
			return &pages[0], nil
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	if pages, exists := r.pages[docID]; exists {
		for _, p := range pages {
			if p.PageNumber == pageNumber {
				return &p, nil
			}
		}
	}
	return nil, fmt.Errorf("page %d of document '%s' not found", pageNumber, docID)
}

// UpdatePageStatus updates the in-memory processing status and OCR completion of a document page
func (r *ResourceDB) UpdatePageStatus(ctx context.Context, docID string, pageNumber int, status string, errMsg string) error {
	now := time.Now().UTC()
	if r.dbClient != nil {
		params := url.Values{}
		params.Set("document_id", "eq."+docID)
		params.Set("page_number", fmt.Sprintf("eq.%d", pageNumber))
		payload := map[string]interface{}{
			"ocr_completed_at": now,
		}
		var updated []DocumentPage
		_ = r.dbClient.Update(ctx, "document_pages", params, payload, &updated)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if pages, exists := r.pages[docID]; exists {
		for i := range pages {
			if pages[i].PageNumber == pageNumber {
				pages[i].OCRCompletedAt = &now
				break
			}
		}
	}
	return nil
}

// InsertExtraction saves extracted bounding boxes and OCR fields into document_extractions table
func (r *ResourceDB) InsertExtraction(ctx context.Context, docID string, extractedJSON map[string]interface{}, confidence float64) error {
	if r.dbClient != nil {
		payload := map[string]interface{}{
			"id":                 model.NewUUID(),
			"document_id":        docID,
			"schema_version":     "0.1.0",
			"extracted_json":     extractedJSON,
			"overall_confidence": confidence,
			"extraction_method":  "ocr_layout_ai",
			"created_at":         time.Now().UTC(),
		}
		var out []map[string]interface{}
		_ = r.dbClient.Insert(ctx, "document_extractions", payload, &out)
	}
	return nil
}

// InsertInvoice saves structured invoice data into invoices table in Supabase
func (r *ResourceDB) InsertInvoice(ctx context.Context, payload map[string]interface{}) error {
	if r.dbClient != nil {
		var out []map[string]interface{}
		_ = r.dbClient.Insert(ctx, "invoices", payload, &out)
	}
	return nil
}

// InsertSuratJalan saves structured Surat Jalan data into surat_jalans table in Supabase
func (r *ResourceDB) InsertSuratJalan(ctx context.Context, payload map[string]interface{}) error {
	if r.dbClient != nil {
		var out []map[string]interface{}
		_ = r.dbClient.Insert(ctx, "surat_jalans", payload, &out)
	}
	return nil
}

// InsertPOD saves structured Proof of Delivery data into pods table in Supabase
func (r *ResourceDB) InsertPOD(ctx context.Context, payload map[string]interface{}) error {
	if r.dbClient != nil {
		var out []map[string]interface{}
		_ = r.dbClient.Insert(ctx, "pods", payload, &out)
	}
	return nil
}

// InsertRateAgreement saves structured Rate Agreement data into rate_agreements table in Supabase
func (r *ResourceDB) InsertRateAgreement(ctx context.Context, payload map[string]interface{}) error {
	if r.dbClient != nil {
		var out []map[string]interface{}
		_ = r.dbClient.Insert(ctx, "rate_agreements", payload, &out)
	}
	return nil
}

// InsertSegment saves a document segment record into Supabase DB and local cache
func (r *ResourceDB) InsertSegment(ctx context.Context, seg *DocumentSegment) error {
	if seg.ID == "" {
		seg.ID = model.NewUUID()
	}
	if seg.CreatedAt.IsZero() {
		seg.CreatedAt = time.Now().UTC()
	}
	if seg.PageCount == 0 && seg.EndPage >= seg.StartPage {
		seg.PageCount = seg.EndPage - seg.StartPage + 1
	}

	if r.dbClient != nil {
		// Note: page_count is a GENERATED column in PostgreSQL, so omit it from insert payload
		payload := map[string]interface{}{
			"id":                        seg.ID,
			"document_id":               seg.DocumentID,
			"segment_number":            seg.SegmentNumber,
			"document_type":             seg.DocumentType,
			"start_page":                seg.StartPage,
			"end_page":                  seg.EndPage,
			"classification_confidence": seg.ClassificationConfidence,
			"classification_method":     seg.ClassificationMethod,
			"extraction_status":         normalizeSegmentStatus(seg.ExtractionStatus),
			"requires_human_review":     seg.RequiresHumanReview,
			"created_at":                seg.CreatedAt,
			"updated_at":                seg.CreatedAt,
		}

		var created []DocumentSegment
		err := r.dbClient.Insert(ctx, "document_segments", payload, &created)
		if err != nil {
			fmt.Printf("[ResourceDB.InsertSegment] Error inserting segment %d: %v\n", seg.SegmentNumber, err)
			return err
		}

	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.segments[seg.DocumentID] = append(r.segments[seg.DocumentID], *seg)
	return nil
}

func normalizeSegmentStatus(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "PROCESSING", "COMPLETED", "FAILED", "REVIEW", "PENDING":
		return strings.ToUpper(strings.TrimSpace(status))
	case "EXTRACTED", "SUCCEEDED":
		return "COMPLETED"
	default:
		return "PENDING"
	}
}

// ListSegments returns all segments associated with a document ordered by segment_number
func (r *ResourceDB) ListSegments(ctx context.Context, docID string) ([]DocumentSegment, error) {
	if r.dbClient != nil {
		var segs []DocumentSegment
		params := url.Values{}
		params.Set("document_id", "eq."+docID)
		params.Set("order", "segment_number.asc")
		if err := r.dbClient.Query(ctx, "document_segments", params, &segs); err == nil && len(segs) > 0 {
			return segs, nil
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	if segs, exists := r.segments[docID]; exists {
		return segs, nil
	}
	return []DocumentSegment{}, nil
}

// InsertFieldEvidence saves individual extracted field evidence with BBox into field_evidence table
func (r *ResourceDB) InsertFieldEvidence(ctx context.Context, payload map[string]interface{}) error {
	if r.dbClient != nil {
		var out []map[string]interface{}
		if err := r.dbClient.Insert(ctx, "field_evidence", payload, &out); err != nil {
			fmt.Printf("[ResourceDB.InsertFieldEvidence] Error inserting %v: %v\n", payload["field_name"], err)
			return err
		}
	}
	return nil
}

// FieldEvidence is a persisted field with its normalized bbox on a page of the document.
type FieldEvidence struct {
	ID              string   `json:"id"`
	SegmentID       string   `json:"segment_id"`
	DocumentID      string   `json:"document_id"`
	PageNumber      int      `json:"page_number"`
	FieldName       string   `json:"field_name"`
	FieldValue      string   `json:"field_value"`
	Confidence      float64  `json:"confidence"`
	BBoxX1          *float64 `json:"bbox_x1"`
	BBoxY1          *float64 `json:"bbox_y1"`
	BBoxX2          *float64 `json:"bbox_x2"`
	BBoxY2          *float64 `json:"bbox_y2"`
	CoordinateSpace string   `json:"coordinate_space"`
}

// ListFieldEvidence returns evidence rows for a document, optionally filtered by page.
func (r *ResourceDB) ListFieldEvidence(ctx context.Context, docID string, page int) ([]FieldEvidence, error) {
	if r.dbClient == nil {
		return []FieldEvidence{}, nil
	}
	var runs []struct {
		ID string `json:"id"`
	}
	runParams := url.Values{"document_id": []string{"eq." + docID}, "select": []string{"id"}}
	if err := r.dbClient.Query(ctx, "ai_pipeline_runs", runParams, &runs); err != nil {
		return nil, fmt.Errorf("load AI pipeline runs: %w", err)
	}
	if len(runs) == 0 {
		return []FieldEvidence{}, nil
	}
	runIDs := make([]string, 0, len(runs))
	for _, run := range runs {
		runIDs = append(runIDs, run.ID)
	}
	var extractions []struct {
		ID string `json:"id"`
	}
	extractionParams := url.Values{"pipeline_run_id": []string{"in.(" + strings.Join(runIDs, ",") + ")"}, "select": []string{"id"}}
	if err := r.dbClient.Query(ctx, "ai_extraction_runs", extractionParams, &extractions); err != nil {
		return nil, fmt.Errorf("load AI extraction runs: %w", err)
	}
	if len(extractions) == 0 {
		return []FieldEvidence{}, nil
	}
	extractionIDs := make([]string, 0, len(extractions))
	for _, extraction := range extractions {
		extractionIDs = append(extractionIDs, extraction.ID)
	}
	var rows []struct {
		ID           string          `json:"id"`
		FieldName    string          `json:"field_name"`
		PredictedRaw string          `json:"predicted_raw_text"`
		Score        float64         `json:"score"`
		EvidencePage int             `json:"evidence_page"`
		EvidenceBBox json.RawMessage `json:"evidence_bbox"`
	}
	predictionParams := url.Values{"extraction_run_id": []string{"in.(" + strings.Join(extractionIDs, ",") + ")"}, "select": []string{"id,field_name,predicted_raw_text,score,evidence_page,evidence_bbox"}, "order": []string{"field_name.asc"}}
	if page > 0 {
		predictionParams.Set("evidence_page", fmt.Sprintf("eq.%d", page))
	}
	if err := r.dbClient.Query(ctx, "ai_field_predictions", predictionParams, &rows); err != nil {
		return nil, fmt.Errorf("load AI field predictions: %w", err)
	}
	result := make([]FieldEvidence, 0, len(rows))
	for _, row := range rows {
		result = append(result, FieldEvidence{
			ID: row.ID, DocumentID: docID, PageNumber: row.EvidencePage,
			FieldName: row.FieldName, FieldValue: row.PredictedRaw,
			Confidence: row.Score, CoordinateSpace: "page_image_normalized",
		})
	}
	return result, nil
}

func (r *ResourceDB) GetProfile(ctx context.Context, docID string) (*DocumentProfile, error) {
	doc, err := r.FindByID(ctx, docID)
	if err != nil {
		return nil, err
	}
	profile := &DocumentProfile{Document: doc, Evidence: []FieldEvidence{}, Predictions: []map[string]interface{}{}, Decisions: []map[string]interface{}{}}
	profile.Pages, err = r.ListPages(ctx, docID)
	if err != nil {
		return nil, err
	}
	profile.Segments, err = r.ListSegments(ctx, docID)
	if err != nil {
		return nil, err
	}
	if r.dbClient == nil {
		return profile, nil
	}

	var runs []map[string]interface{}
	if err := r.dbClient.Query(ctx, "ai_pipeline_runs", url.Values{
		"document_id": []string{"eq." + docID},
		"order":       []string{"started_at.desc"},
		"limit":       []string{"1"},
	}, &runs); err != nil {
		return nil, fmt.Errorf("load profile pipeline: %w", err)
	}
	if len(runs) == 0 {
		return profile, nil
	}
	profile.Pipeline = runs[0]
	runID := stringValue(runs[0]["id"])
	var extractions []map[string]interface{}
	if err := r.dbClient.Query(ctx, "ai_extraction_runs", url.Values{
		"pipeline_run_id": []string{"eq." + runID},
		"select":          []string{"id,document_type,model_set_id,status,extraction_json,created_at"},
	}, &extractions); err != nil {
		return nil, fmt.Errorf("load profile extraction runs: %w", err)
	}
	if len(extractions) == 0 {
		return profile, nil
	}
	extractionIDs := make([]string, 0, len(extractions))
	for _, extraction := range extractions {
		if id := stringValue(extraction["id"]); id != "" {
			extractionIDs = append(extractionIDs, id)
		}
	}
	if len(extractionIDs) == 0 {
		return profile, nil
	}
	var predictions []map[string]interface{}
	if err := r.dbClient.Query(ctx, "ai_field_predictions", url.Values{
		"extraction_run_id": []string{"in.(" + strings.Join(extractionIDs, ",") + ")"},
		"order":             []string{"field_name.asc"},
	}, &predictions); err != nil {
		return nil, fmt.Errorf("load profile predictions: %w", err)
	}
	profile.Predictions = predictions
	documentTypes := make(map[string]string, len(extractions))
	for _, extraction := range extractions {
		documentTypes[stringValue(extraction["id"])] = stringValue(extraction["document_type"])
	}
	predictionIDs := make([]string, 0, len(predictions))
	for _, prediction := range predictions {
		docType := documentTypes[stringValue(prediction["extraction_run_id"])]
		prediction["document_type"] = docType
		prediction["registry_visible"] = isRegistryField(docType, stringValue(prediction["field_name"]))
		metadata, _ := prediction["decoder_metadata"].(map[string]interface{})
		prediction["review_status"] = valueOrDefault(stringValue(prediction["prediction_status"]), "UNKNOWN")
		prediction["low_confidence"] = boolValue(metadata["low_confidence"])
		prediction["source_kind"] = valueOrDefault(stringValue(metadata["source_kind"]), "rule")
		prediction["alternatives"] = objectOrEmpty(metadata["alternatives"])
		if id := stringValue(prediction["id"]); id != "" {
			predictionIDs = append(predictionIDs, id)
		}
	}
	if len(predictionIDs) > 0 {
		if err := r.dbClient.Query(ctx, "ai_field_decisions", url.Values{
			"prediction_id": []string{"in.(" + strings.Join(predictionIDs, ",") + ")"},
			"order":         []string{"created_at.asc"},
		}, &profile.Decisions); err != nil {
			return nil, fmt.Errorf("load profile decisions: %w", err)
		}
	}
	profile.Evidence, err = r.ListFieldEvidence(ctx, docID, 0)
	if err != nil {
		return nil, err
	}
	return profile, nil
}

func (r *ResourceDB) ReviewField(ctx context.Context, documentID string, request FieldReviewRequest) error {
	if r.dbClient == nil {
		return fmt.Errorf("database is not configured")
	}
	if request.PredictionID == "" {
		return fmt.Errorf("prediction_id is required")
	}
	action := strings.ToUpper(strings.TrimSpace(request.Action))
	switch action {
	case "ACCEPT_PREDICTION", "CORRECT_VALUE", "MARK_UNKNOWN", "REJECT":
	default:
		return fmt.Errorf("unsupported review action %q", request.Action)
	}

	var predictions []map[string]interface{}
	if err := r.dbClient.Query(ctx, "ai_field_predictions", url.Values{
		"id":     []string{"eq." + request.PredictionID},
		"select": []string{"id,extraction_run_id,field_name,predicted_value,predicted_raw_text,evidence_page,evidence_bbox"},
		"limit":  []string{"1"},
	}, &predictions); err != nil {
		return fmt.Errorf("load prediction: %w", err)
	}
	if len(predictions) == 0 {
		return fmt.Errorf("prediction %q not found", request.PredictionID)
	}
	fieldName := stringValue(predictions[0]["field_name"])
	// An empty candidate is an explicit extraction failure, not something a
	// reviewer can approve. Without this guard a `<nil>` value could be marked
	// approved and later enter deterministic reconciliation as business data.
	if action == "ACCEPT_PREDICTION" && isNilValue(predictions[0]["predicted_value"]) {
		return fmt.Errorf("cannot approve an empty prediction; submit CORRECT_VALUE instead")
	}
	if action == "CORRECT_VALUE" && isNilValue(request.ReviewedValue) {
		return fmt.Errorf("reviewed_value is required when correcting a prediction")
	}
	var extractions []map[string]interface{}
	if err := r.dbClient.Query(ctx, "ai_extraction_runs", url.Values{
		"id":     []string{"eq." + stringValue(predictions[0]["extraction_run_id"])},
		"select": []string{"pipeline_run_id,document_type"},
		"limit":  []string{"1"},
	}, &extractions); err != nil {
		return fmt.Errorf("load extraction run: %w", err)
	}
	if len(extractions) == 0 {
		return fmt.Errorf("extraction run for prediction not found")
	}
	docType := stringValue(extractions[0]["document_type"])
	if !isRegistryField(docType, fieldName) {
		return fmt.Errorf("field %q is not reviewable for document type %q", fieldName, docType)
	}
	if action == "CORRECT_VALUE" {
		if err := validateReviewValue(predictions[0]["predicted_value"], request.ReviewedValue); err != nil {
			return err
		}
	}
	var runs []map[string]interface{}
	if err := r.dbClient.Query(ctx, "ai_pipeline_runs", url.Values{
		"id":     []string{"eq." + stringValue(extractions[0]["pipeline_run_id"])},
		"select": []string{"document_id"},
		"limit":  []string{"1"},
	}, &runs); err != nil {
		return fmt.Errorf("load pipeline run: %w", err)
	}
	if len(runs) == 0 || stringValue(runs[0]["document_id"]) != documentID {
		return fmt.Errorf("prediction does not belong to document")
	}

	reviewID := model.NewUUID()
	reviewPayload := map[string]interface{}{
		"id": reviewID, "prediction_id": request.PredictionID, "action": action,
		"reviewed_value": request.ReviewedValue, "reason": request.Reason,
		"reviewer_external_id": request.ReviewerExternalID,
		"review_metadata":      map[string]interface{}{}, "created_at": time.Now().UTC(),
	}
	var out []map[string]interface{}
	if err := r.dbClient.Insert(ctx, "ai_field_reviews", reviewPayload, &out); err != nil {
		return fmt.Errorf("insert field review: %w", err)
	}
	status := map[string]string{
		"ACCEPT_PREDICTION": "APPROVED",
		"CORRECT_VALUE":     "CORRECTED",
		"MARK_UNKNOWN":      "UNKNOWN",
		"REJECT":            "REJECTED",
	}[action]
	finalValue := request.ReviewedValue
	if action == "ACCEPT_PREDICTION" {
		// An approval promotes the immutable predicted value.  It must not erase
		// the value from the current decision projection.
		finalValue = predictions[0]["predicted_value"]
	}
	finalRawText := stringValue(predictions[0]["predicted_raw_text"])
	if finalValue != nil {
		finalRawText = payloadSummary(finalValue)
	}
	if err := r.dbClient.Update(ctx, "ai_field_decisions", url.Values{
		"prediction_id": []string{"eq." + request.PredictionID},
	}, map[string]interface{}{
		"review_id": reviewID, "decision_status": status, "final_value": finalValue,
		"final_raw_text": finalRawText, "final_page": predictions[0]["evidence_page"],
		"final_bbox": predictions[0]["evidence_bbox"], "decided_at": time.Now().UTC(),
	}, nil); err != nil {
		return fmt.Errorf("update field decision: %w", err)
	}
	// Document-level approval is a projection of field-level human decisions;
	// no model score can mark a document approved by itself.
	_ = r.syncDocumentApprovalStatus(ctx, documentID)
	return nil
}

func (r *ResourceDB) syncDocumentApprovalStatus(ctx context.Context, documentID string) error {
	profile, err := r.GetProfile(ctx, documentID)
	if err != nil || len(profile.Predictions) == 0 {
		return err
	}
	visibleCount := 0
	for _, prediction := range profile.Predictions {
		if !boolValue(prediction["registry_visible"]) {
			continue
		}
		visibleCount++
		var decision map[string]interface{}
		for _, candidate := range profile.Decisions {
			if stringValue(candidate["prediction_id"]) == stringValue(prediction["id"]) {
				decision = candidate
				break
			}
		}
		status := stringValue(decision["decision_status"])
		if status != "APPROVED" && status != "CORRECTED" {
			return r.UpdateApprovalStatus(ctx, documentID, "UNDER_REVIEW")
		}
	}
	if visibleCount == 0 {
		return r.UpdateApprovalStatus(ctx, documentID, "UNDER_REVIEW")
	}
	return r.UpdateApprovalStatus(ctx, documentID, "APPROVED")
}

// EnsureVendorExists checks if vendor_id is in vendors table, if not inserts it automatically
func (r *ResourceDB) EnsureVendorExists(ctx context.Context, vendorID, vendorName string) {
	if vendorID == "" || r.dbClient == nil {
		return
	}
	if vendorName == "" {
		vendorName = vendorID
	}

	var existing []map[string]interface{}
	params := url.Values{}
	params.Set("vendor_id", "eq."+vendorID)
	params.Set("limit", "1")
	if err := r.dbClient.Query(ctx, "vendors", params, &existing); err == nil && len(existing) > 0 {
		return
	}

	now := time.Now().UTC()
	payload := map[string]interface{}{
		"id":          model.NewUUID(),
		"vendor_id":   vendorID,
		"vendor_name": vendorName,
		"is_active":   true,
		"created_at":  now,
		"updated_at":  now,
	}
	var out []map[string]interface{}
	_ = r.dbClient.Insert(ctx, "vendors", payload, &out)
	fmt.Printf("[Auto-Vendor Pipeline] Registered new vendor in DB: %s (%s)\n", vendorID, vendorName)
}

func (r *ResourceDB) PersistAIResult(ctx context.Context, documentID, ingestBatchID, filename, sha256Hash, mimeType string, result *AIExtractionResult) error {
	if r.dbClient == nil || result == nil {
		return nil
	}
	now := time.Now().UTC()
	runID := model.NewUUID()
	status := "SUCCEEDED"
	if strings.EqualFold(result.Status, "failed") || result.ErrorCode != "" {
		status = "FAILED"
	}
	raw := result.RawPayload
	if len(raw) == 0 {
		raw, _ = json.Marshal(result)
	}
	var rawJSON interface{}
	if err := json.Unmarshal(raw, &rawJSON); err != nil {
		rawJSON = map[string]interface{}{}
	}
	var evidenceJSON interface{}
	if len(result.Evidence) > 0 && json.Unmarshal(result.Evidence, &evidenceJSON) == nil {
		// The explicit evidence projection is used for page primitives while
		// config_snapshot retains the complete response for replay.
	}
	runPayload := map[string]interface{}{
		"id": runID, "document_id": documentID, "ingest_batch_id": nullableUUID(ingestBatchID),
		"run_type": "FULL", "status": status, "source_filename": filename,
		"source_sha256": sha256Hash, "mime_type": mimeType,
		"pipeline_version": valueOrDefault(result.PipelineVersion, "backend-ai-contract-v1"),
		"request_metadata": map[string]interface{}{}, "config_snapshot": rawJSON,
		"error_code": result.ErrorCode, "started_at": now, "completed_at": now, "created_at": now,
	}
	var runs []map[string]interface{}
	if err := r.dbClient.Insert(ctx, "ai_pipeline_runs", runPayload, &runs); err != nil {
		return fmt.Errorf("insert ai pipeline run: %w", err)
	}
	steps := []string{"INGESTION", "OCR_LAYOUT", "CLASSIFICATION", "SEGMENTATION", "ML_PREDICTION", "DETERMINISTIC_VALIDATION"}
	for _, step := range steps {
		output := map[string]interface{}{}
		switch step {
		case "CLASSIFICATION":
			output = result.Classification
		case "SEGMENTATION":
			output = map[string]interface{}{"segments": result.Segments}
		case "ML_PREDICTION":
			output = result.MLExtraction
		case "DETERMINISTIC_VALIDATION":
			output = result.ArithmeticValidation
		}
		if output == nil {
			output = map[string]interface{}{}
		}
		payload := map[string]interface{}{
			"id": model.NewUUID(), "pipeline_run_id": runID, "step_name": step,
			"status": "SUCCEEDED", "implementation_version": result.PipelineVersion,
			"output_json": output, "metrics_json": map[string]interface{}{},
			"started_at": now, "completed_at": now, "created_at": now,
		}
		var out []map[string]interface{}
		if err := r.dbClient.Insert(ctx, "ai_pipeline_steps", payload, &out); err != nil {
			return fmt.Errorf("insert AI step %s: %w", step, err)
		}
	}
	segmentIDs, err := r.segmentIDs(ctx, documentID)
	if err != nil {
		return err
	}
	persistedTableKeys := map[string]bool{}
	for _, page := range result.Pages {
		pageRaw, _ := json.Marshal(page)
		pageJSON := interface{}(map[string]interface{}{})
		_ = json.Unmarshal(pageRaw, &pageJSON)
		pageSource := rawJSON
		if evidenceJSON != nil {
			pageSource = evidenceJSON
		}
		if evidencePage, ok := rawPageByNumber(pageSource, page.PageNumber); ok {
			pageJSON = evidencePage
		}
		payload := map[string]interface{}{
			"id": model.NewUUID(), "pipeline_run_id": runID, "document_id": documentID,
			"page_number": page.PageNumber, "source_kind": sourceKind(mimeType),
			"ocr_engine": page.EngineUsed, "coordinate_space": valueOrDefault(page.CoordSpace, "page_image_normalized"),
			"page_width": page.PageImage.Width, "page_height": page.PageImage.Height,
			"quality_json": objectOrEmpty(page.Quality), "raw_evidence": objectOrEmpty(pageJSON), "created_at": now,
		}
		var out []map[string]interface{}
		if err := r.dbClient.Insert(ctx, "ai_evidence_pages", payload, &out); err != nil {
			return fmt.Errorf("insert evidence page %d: %w", page.PageNumber, err)
		}
		if len(out) > 0 {
			pageID := stringValue(out[0]["id"])
			if rawPage, ok := rawPageByNumber(pageSource, page.PageNumber); ok {
				if lines, ok := rawPage["lines"].([]interface{}); ok {
					for index, lineValue := range lines {
						line, ok := lineValue.(map[string]interface{})
						if !ok {
							continue
						}
						linePayload := map[string]interface{}{
							"id": model.NewUUID(), "evidence_page_id": pageID,
							"line_number": index, "text": stringValue(line["text"]),
							"confidence": nullableConfidence(line["confidence"]), "raw_line": line,
						}
						if bbox, ok := line["bbox"].([]interface{}); ok {
							addBBox(linePayload, bbox)
						}
						if err := r.dbClient.Insert(ctx, "ai_evidence_lines", linePayload, nil); err != nil {
							return fmt.Errorf("insert evidence line page %d line %d: %w", page.PageNumber, index, err)
						}
					}
					if tables, ok := rawPage["tables"].([]interface{}); ok {
						for index, tableValue := range tables {
							table, ok := tableValue.(map[string]interface{})
							if !ok {
								continue
							}
							tableKey := valueOrDefault(stringValue(table["table_id"]), fmt.Sprintf("page-%d-table-%d", page.PageNumber, index+1))
							if persistedTableKeys[tableKey] {
								continue
							}
							payload := map[string]interface{}{
								"id": model.NewUUID(), "pipeline_run_id": runID, "table_key": tableKey,
								"page_start": page.PageNumber, "page_end": page.PageNumber,
								"role": "evidence", "layout_kind": "docling_table", "source": "docling",
								"confidence": nullableConfidence(table["confidence"]), "bbox": table["bbox_norm"],
								"columns_json": arrayOrEmpty(table["headers"]), "rows_json": arrayOrEmpty(table["rows"]),
								"validation_json": objectOrEmpty(table["validation"]), "raw_table": objectOrEmpty(table),
								"created_at": now,
							}
							if err := r.dbClient.Insert(ctx, "ai_evidence_tables", payload, nil); err != nil {
								return fmt.Errorf("insert evidence page table %s: %w", tableKey, err)
							}
							persistedTableKeys[tableKey] = true
						}
					}
				}
			}
		}
	}
	for index, table := range result.TableGroups {
		tableKey := valueOrDefault(stringValue(table["table_key"]), fmt.Sprintf("table-%d", index+1))
		if persistedTableKeys[tableKey] {
			continue
		}
		payload := map[string]interface{}{
			"id": model.NewUUID(), "pipeline_run_id": runID, "table_key": tableKey,
			"page_start": positiveInt(table["page_start"]), "page_end": positiveInt(table["page_end"]),
			"role": stringValue(table["role"]), "layout_kind": stringValue(table["layout_kind"]),
			"source": stringValue(table["source"]), "confidence": nullableConfidence(table["confidence"]),
			"bbox": table["bbox"], "columns_json": arrayOrEmpty(table["columns"]), "rows_json": arrayOrEmpty(table["rows"]),
			"validation_json": objectOrEmpty(table["validation"]), "raw_table": objectOrEmpty(table), "created_at": now,
		}
		var out []map[string]interface{}
		if err := r.dbClient.Insert(ctx, "ai_evidence_tables", payload, &out); err != nil {
			return fmt.Errorf("insert evidence table %s: %w", tableKey, err)
		}
		persistedTableKeys[tableKey] = true
	}
	for i, segment := range result.Segments {
		if i >= len(segmentIDs) {
			break
		}
		payload := map[string]interface{}{
			"id": model.NewUUID(), "pipeline_run_id": runID, "segment_id": segmentIDs[i],
			"document_type":         normalizeDocumentType(segment.DocumentType),
			"confidence":            clampConfidence(segment.ClassificationConfidence),
			"requires_human_review": segment.RequiresHumanReview,
			"probabilities":         map[string]interface{}{}, "classification_method": segment.ClassificationMethod,
			"created_at": now,
		}
		var out []map[string]interface{}
		if err := r.dbClient.Insert(ctx, "ai_classification_predictions", payload, &out); err != nil {
			return fmt.Errorf("insert segment classification %d: %w", segment.SegmentNumber, err)
		}
	}
	for i, extraction := range result.SegmentExtractions {
		var segmentID string
		if i < len(segmentIDs) {
			segmentID = segmentIDs[i]
		}
		ml := extraction.MLExtraction
		if ml == nil {
			ml = result.MLExtraction
		}
		if err := r.persistExtraction(ctx, runID, segmentID, extraction.DocumentType, extraction.ExtractedFields, ml, now); err != nil {
			return err
		}
	}
	if len(result.SegmentExtractions) == 0 {
		docType := normalizeDocumentType(classificationType(result.Classification))
		if err := r.persistExtraction(ctx, runID, "", docType, result.ExtractedFields, result.MLExtraction, now); err != nil {
			return err
		}
	}
	if len(result.ArithmeticValidation) > 0 {
		if err := r.persistValidation(ctx, runID, result.ArithmeticValidation, now); err != nil {
			return err
		}
	}
	if err := r.dbClient.Update(
		ctx,
		"documents",
		url.Values{"id": {"eq." + documentID}},
		map[string]interface{}{"latest_ai_pipeline_run_id": runID},
		nil,
	); err != nil {
		return fmt.Errorf("link latest AI pipeline run: %w", err)
	}
	return nil
}

func (r *ResourceDB) persistExtraction(ctx context.Context, pipelineRunID, segmentID, documentType string, extracted interface{}, ml map[string]interface{}, now time.Time) error {
	if documentType == "" {
		documentType = "UNKNOWN"
	}
	candidateRunID := model.NewUUID()
	candidatePayload := map[string]interface{}{
		"id": candidateRunID, "pipeline_run_id": pipelineRunID, "segment_id": nullableUUID(segmentID),
		"candidate_pipeline_version": valueOrDefault(stringValue(ml["model_set"]), "candidate-v1"),
		"report_json":                objectOrEmpty(ml), "candidate_cap_config": map[string]interface{}{}, "created_at": now,
	}
	var out []map[string]interface{}
	if err := r.dbClient.Insert(ctx, "ai_candidate_generation_runs", candidatePayload, &out); err != nil {
		return fmt.Errorf("insert candidate generation run: %w", err)
	}
	extractionRunID := model.NewUUID()
	extractionPayload := map[string]interface{}{
		"id": extractionRunID, "pipeline_run_id": pipelineRunID, "segment_id": nullableUUID(segmentID),
		"document_type": documentType, "model_set_id": stringValue(ml["model_set"]),
		"status": extractionStatus(ml), "extraction_json": objectOrEmpty(ml), "created_at": now,
	}
	if err := r.dbClient.Insert(ctx, "ai_extraction_runs", extractionPayload, &out); err != nil {
		return fmt.Errorf("insert extraction run: %w", err)
	}
	mlFields := map[string]map[string]interface{}{}
	if fields, ok := ml["fields"].([]interface{}); ok {
		for _, item := range fields {
			if field, ok := item.(map[string]interface{}); ok {
				if name := stringValue(field["field"]); name != "" {
					mlFields[name] = field
				}
			}
		}
	}
	fields := fieldMaps(extracted)
	for _, field := range fields {
		name := stringValue(field["field"])
		if name == "" {
			continue
		}
		info := mlFields[name]
		score := clampConfidence(floatValue(field["confidence"]))
		if field["confidence"] == nil && info["score"] != nil {
			score = clampConfidence(floatValue(info["score"]))
		}
		valueJSON, _ := json.Marshal(field["value"])
		var value interface{}
		_ = json.Unmarshal(valueJSON, &value)
		isUnknown := isNilValue(value)
		status := strings.ToUpper(stringValue(field["review_status"]))
		if status == "" {
			status = strings.ToUpper(stringValue(info["status"]))
		}
		if isUnknown {
			status = "UNKNOWN"
			score = 0
		} else if status != "ACCEPTED" && status != "REVIEW" {
			if score >= 0.9 {
				status = "ACCEPTED"
			} else {
				status = "REVIEW"
			}
		}
		if boolValue(field["is_unknown"]) && value == nil {
			value = nil
			status = "UNKNOWN"
			score = 0
		}
		sourceRawText := stringValue(field["raw_text"])
		if sourceRawText == "" {
			sourceRawText = stringValue(info["raw_text"])
		}
		rawText := payloadSummary(value)
		if value == nil {
			rawText = ""
		}
		page := int(floatValue(info["page"]))
		if page <= 0 {
			if source, ok := field["source"].(map[string]interface{}); ok {
				page = int(floatValue(source["page"]))
			}
		}
		if page <= 0 {
			page = 1
		}
		bbox := info["bbox_norm"]
		if bbox == nil {
			if source, ok := field["source"].(map[string]interface{}); ok {
				if sourceBox, ok := source["bbox"].(map[string]interface{}); ok {
					bbox = []interface{}{sourceBox["x1"], sourceBox["y1"], sourceBox["x2"], sourceBox["y2"]}
				}
			}
		}
		candidateID := model.NewUUID()
		candidatePayload := map[string]interface{}{
			"id": candidateID, "candidate_run_id": candidateRunID, "document_type": documentType,
			"field_name": name, "candidate_type": "FIELD_VALUE", "page_number": page,
			"raw_text": sourceRawText, "parsed_value": value, "normalized_value": rawText,
			"confidence": score, "bbox": bbox, "raw_candidate": objectOrEmpty(info),
			"features": map[string]interface{}{}, "context": map[string]interface{}{}, "created_at": now,
		}
		if err := r.dbClient.Insert(ctx, "ai_field_candidates", candidatePayload, &out); err != nil {
			return fmt.Errorf("insert candidate for %s: %w", name, err)
		}
		predictionID := model.NewUUID()
		metadata := objectOrEmpty(field["decoder_metadata"])
		if metadataMap, ok := metadata.(map[string]interface{}); ok {
			metadataMap["source_raw_text"] = sourceRawText
			metadataMap["review_status"] = status
			metadataMap["low_confidence"] = boolValue(field["low_confidence"])
			metadataMap["source_kind"] = valueOrDefault(stringValue(field["source_kind"]), "rule")
			metadataMap["alternatives"] = arrayOrEmpty(field["alternatives"])
			if len(info) > 0 {
				metadataMap["ml"] = info
			}
		}
		predictionPayload := map[string]interface{}{
			"id": predictionID, "extraction_run_id": extractionRunID, "field_name": name,
			"prediction_status": status, "predicted_value": value, "predicted_raw_text": rawText,
			"score": score, "selected_candidate_id": candidateID, "evidence_page": page,
			"evidence_bbox": bbox, "decoder_metadata": metadata, "created_at": now,
		}
		if err := r.dbClient.Insert(ctx, "ai_field_predictions", predictionPayload, &out); err != nil {
			return fmt.Errorf("insert prediction for %s: %w", name, err)
		}
		scorePayload := map[string]interface{}{
			"id": model.NewUUID(), "prediction_id": predictionID, "candidate_id": candidateID,
			"score": score, "rank": 1, "score_components": map[string]interface{}{},
		}
		if err := r.dbClient.Insert(ctx, "ai_prediction_candidate_scores", scorePayload, &out); err != nil {
			return fmt.Errorf("insert candidate score for %s: %w", name, err)
		}
		// A model score is never a business approval.  Every generated mapping
		// enters the HITL queue, including high-confidence LightGBM output.
		decisionStatus := "PENDING_REVIEW"

		decisionPayload := map[string]interface{}{
			"id": model.NewUUID(), "prediction_id": predictionID, "decision_status": decisionStatus,
			"final_value": value, "final_raw_text": rawText, "final_page": page,
			"final_bbox": bbox, "created_at": now,
		}
		if err := r.dbClient.Insert(ctx, "ai_field_decisions", decisionPayload, &out); err != nil {
			return fmt.Errorf("insert field decision for %s: %w", name, err)
		}
	}
	return nil
}

func fieldMaps(value interface{}) []map[string]interface{} {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var fields []map[string]interface{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil
	}
	return fields
}

func boolValue(value interface{}) bool {
	result, _ := value.(bool)
	return result
}

func (r *ResourceDB) persistValidation(ctx context.Context, pipelineRunID string, validation map[string]interface{}, now time.Time) error {
	rulePayload := map[string]interface{}{
		"id": model.NewUUID(), "rule_set_name": "ai-service-arithmetic",
		"rule_set_version": "runtime-v1", "config_json": map[string]interface{}{},
		"is_active": true, "created_at": now,
	}
	var out []map[string]interface{}
	if err := r.dbClient.Insert(ctx, "ai_validation_rule_sets", rulePayload, &out); err != nil {
		var existing []map[string]interface{}
		params := url.Values{"rule_set_name": []string{"eq.ai-service-arithmetic"}, "rule_set_version": []string{"eq.runtime-v1"}, "select": []string{"id"}}
		if queryErr := r.dbClient.Query(ctx, "ai_validation_rule_sets", params, &existing); queryErr != nil || len(existing) == 0 {
			return fmt.Errorf("insert validation rule set: %w", err)
		}
		rulePayload["id"] = existing[0]["id"]
	}
	runPayload := map[string]interface{}{
		"id": model.NewUUID(), "pipeline_run_id": pipelineRunID, "rule_set_id": rulePayload["id"],
		"status": "SUCCEEDED", "overall_result": validationResult(validation),
		"result_json": validation, "created_at": now,
	}
	if err := r.dbClient.Insert(ctx, "ai_validation_runs", runPayload, &out); err != nil {
		return fmt.Errorf("insert validation run: %w", err)
	}
	return nil
}

func classificationType(value map[string]interface{}) string {
	for _, key := range []string{"document_type", "type", "label"} {
		if v := stringValue(value[key]); v != "" {
			return v
		}
	}
	return "UNKNOWN"
}

func extractionStatus(value map[string]interface{}) string {
	switch strings.ToLower(stringValue(value["status"])) {
	case "ok", "success", "succeeded":
		return "SUCCEEDED"
	case "no_model_set", "no_model_for_doc_type":
		return "NO_MODEL"
	case "disabled":
		return "DISABLED"
	case "error":
		return "FAILED"
	default:
		return "PARTIAL"
	}
}

func stringValue(value interface{}) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func floatValue(value interface{}) float64 {
	switch number := value.(type) {
	case float64:
		return number
	case float32:
		return float64(number)
	case int:
		return float64(number)
	case json.Number:
		value, _ := number.Float64()
		return value
	default:
		return 0
	}
}

func validationResult(value map[string]interface{}) string {
	switch strings.ToUpper(stringValue(value["overall_result"])) {
	case "PASS", "FAIL", "WARNING":
		return strings.ToUpper(stringValue(value["overall_result"]))
	default:
		return "UNKNOWN"
	}
}

func rawPageByNumber(raw interface{}, pageNumber int) (map[string]interface{}, bool) {
	root, ok := raw.(map[string]interface{})
	if !ok {
		return nil, false
	}
	pages, ok := root["pages"].([]interface{})
	if !ok {
		return nil, false
	}
	for _, value := range pages {
		page, ok := value.(map[string]interface{})
		if ok && int(floatValue(page["page_number"])) == pageNumber {
			return page, true
		}
	}
	return nil, false
}

func nullableConfidence(value interface{}) interface{} {
	if value == nil {
		return nil
	}
	return clampConfidence(floatValue(value))
}

func addBBox(payload map[string]interface{}, bbox []interface{}) {
	if len(bbox) < 4 {
		return
	}
	payload["bbox_x1"] = floatValue(bbox[0])
	payload["bbox_y1"] = floatValue(bbox[1])
	payload["bbox_x2"] = floatValue(bbox[2])
	payload["bbox_y2"] = floatValue(bbox[3])
}

func positiveInt(value interface{}) int {
	result := int(floatValue(value))
	if result < 1 {
		return 1
	}
	return result
}

func objectOrEmpty(value interface{}) interface{} {
	if value == nil {
		return map[string]interface{}{}
	}
	if typed, ok := value.(map[string]interface{}); ok && typed == nil {
		return map[string]interface{}{}
	}
	return value
}

func payloadSummary(value interface{}) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case map[string]interface{}:
		if typed["kind"] == "table" {
			headers, _ := typed["headers"].([]interface{})
			rows, _ := typed["rows"].([]interface{})
			pages, _ := typed["pages"].([]interface{})
			page := 0
			if len(pages) > 0 {
				page = int(floatValue(pages[0]))
			}
			if page > 0 {
				return fmt.Sprintf("Tabel p%d · %d baris × %d kolom", page, len(rows), len(headers))
			}
			return fmt.Sprintf("Tabel · %d baris × %d kolom", len(rows), len(headers))
		}
	case []interface{}:
		if len(typed) > 0 {
			if _, ok := typed[0].(map[string]interface{}); ok {
				return fmt.Sprintf("%d baris", len(typed))
			}
		}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func isRegistryField(documentType, fieldName string) bool {
	for _, field := range FieldRegistry[documentType] {
		if field.Name == fieldName {
			return true
		}
	}
	return false
}

func validateReviewValue(predicted, reviewed interface{}) error {
	if scalar, ok := reviewed.(string); ok {
		if len([]rune(scalar)) > 160 || strings.ContainsAny(scalar, "\r\n") {
			return fmt.Errorf("scalar review values must be at most 160 characters and contain no newline")
		}
	}
	prediction, isTable := predicted.(map[string]interface{})
	if !isTable || prediction["kind"] != "table" {
		return nil
	}
	value, ok := reviewed.(map[string]interface{})
	if !ok || value["kind"] != "table" {
		return fmt.Errorf("table corrections must preserve the structured table payload")
	}
	headers, _ := value["headers"].([]interface{})
	rows, _ := value["rows"].([]interface{})
	if len(headers) == 0 {
		return fmt.Errorf("table corrections require headers")
	}
	for index, raw := range rows {
		row, ok := raw.(map[string]interface{})
		if !ok {
			return fmt.Errorf("table row %d must be an object", index+1)
		}
		cells, ok := row["cells"].([]interface{})
		if !ok || len(cells) != len(headers) {
			return fmt.Errorf("table row %d must contain one cell per header", index+1)
		}
	}
	return nil
}

func arrayOrEmpty(value interface{}) interface{} {
	if value == nil {
		return []interface{}{}
	}
	return value
}

func (r *ResourceDB) segmentIDs(ctx context.Context, documentID string) ([]string, error) {
	var segments []DocumentSegment
	params := url.Values{"document_id": []string{"eq." + documentID}, "order": []string{"segment_number.asc"}}
	if err := r.dbClient.Query(ctx, "document_segments", params, &segments); err != nil {
		return nil, fmt.Errorf("load persisted segments: %w", err)
	}
	ids := make([]string, 0, len(segments))
	for _, segment := range segments {
		ids = append(ids, segment.ID)
	}
	return ids, nil
}

func nullableUUID(value string) interface{} {
	if isUUID(value) {
		return value
	}
	return nil
}

func valueOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func clampConfidence(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func sourceKind(mimeType string) string {
	switch strings.ToLower(mimeType) {
	case "application/pdf":
		return "PDF"
	case "image/png":
		return "PNG"
	case "image/jpeg", "image/jpg":
		return "JPEG"
	default:
		return "OTHER"
	}
}
