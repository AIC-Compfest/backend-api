package document

import (
	"context"
	"testing"
	"time"

	"backend-api/internal/model"
)

func TestPageGeometryUsesAIPageImageAndFallsBackToA4(t *testing.T) {
	w, h, wp, hp := pageGeometry(AIPageResult{PageImage: AIPageImage{Width: 1654, Height: 2339, DPI: 200}})
	if w != 1654 || h != 2339 {
		t.Fatalf("px = %dx%d", w, h)
	}
	if wp < 594 || wp > 596 || hp < 841 || hp > 843 {
		t.Fatalf("pts = %.1fx%.1f", wp, hp)
	}
	w, h, _, _ = pageGeometry(AIPageResult{})
	if w != 595 || h != 842 {
		t.Fatalf("fallback = %dx%d", w, h)
	}
}

func TestWriteEvidenceRowSkipsNilAndDuplicates(t *testing.T) {
	r := &Resource{db: NewResourceDB(nil)}
	seen := map[string]bool{}
	src := model.EvidenceSource{Page: 2, BBox: &model.BoundingBox{X1: 0.1, Y1: 0.1, X2: 0.2, Y2: 0.2}}
	r.writeEvidenceRow(context.Background(), "d", "s", "f", "<nil>", 0, src, time.Now(), seen)
	r.writeEvidenceRow(context.Background(), "d", "", "f", "v", 0.9, src, time.Now(), seen)
	if len(seen) != 0 {
		t.Fatalf("nil value / empty segment must not be recorded: %v", seen)
	}
	r.writeEvidenceRow(context.Background(), "d", "s", "f", "v", 0.9, src, time.Now(), seen)
	r.writeEvidenceRow(context.Background(), "d", "s", "f", "v", 0.9, src, time.Now(), seen)
	if len(seen) != 1 {
		t.Fatalf("duplicate not collapsed: %v", seen)
	}
}
