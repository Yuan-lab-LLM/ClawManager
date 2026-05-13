package services

import (
	"fmt"
	"testing"
	"time"

	"clawreef/internal/models"
)

// ---------------------------------------------------------------------------
// Stub repository
// ---------------------------------------------------------------------------

type stubInstanceUsageRepository struct {
	records []models.InstanceUsage
	created []models.InstanceUsage
}

func (r *stubInstanceUsageRepository) Create(record *models.InstanceUsage) error {
	record.ID = len(r.created) + 1
	r.created = append(r.created, *record)
	return nil
}

func (r *stubInstanceUsageRepository) GetLatestByInstanceID(instanceID int) (*models.InstanceUsage, error) {
	for i := len(r.records) - 1; i >= 0; i-- {
		if r.records[i].InstanceID == instanceID {
			return &r.records[i], nil
		}
	}
	return nil, nil
}

func (r *stubInstanceUsageRepository) ListByInstanceID(instanceID int, since time.Time, limit int) ([]models.InstanceUsage, error) {
	var out []models.InstanceUsage
	for _, rec := range r.records {
		if rec.InstanceID == instanceID && !rec.RecordedAt.Before(since) {
			out = append(out, rec)
		}
	}
	return out, nil
}

func (r *stubInstanceUsageRepository) ListLatestPerInstance() ([]models.InstanceUsage, error) {
	latest := map[int]models.InstanceUsage{}
	for _, rec := range r.records {
		if existing, ok := latest[rec.InstanceID]; !ok || rec.RecordedAt.After(existing.RecordedAt) {
			latest[rec.InstanceID] = rec
		}
	}
	var out []models.InstanceUsage
	for _, v := range latest {
		out = append(out, v)
	}
	return out, nil
}

func (r *stubInstanceUsageRepository) DeleteOlderThan(cutoff time.Time) (int64, error) {
	return 0, nil
}

// ---------------------------------------------------------------------------
// parseCPUToMillicores tests
// ---------------------------------------------------------------------------

func TestParseCPUToMillicores(t *testing.T) {
	cases := []struct {
		input string
		want  int64
	}{
		{"100m", 100},
		{"250m", 250},
		{"1", 1000},
		{"2", 2000},
		{"0.5", 500},
		{"1500000000n", 1500},
		{"500000000n", 500},
		{"", 0},
		{"garbage", 0},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("cpu=%q", tc.input), func(t *testing.T) {
			got := parseCPUToMillicores(tc.input)
			if got != tc.want {
				t.Errorf("parseCPUToMillicores(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// parseMemoryToBytes tests
// ---------------------------------------------------------------------------

func TestParseMemoryToBytes(t *testing.T) {
	cases := []struct {
		input string
		want  int64
	}{
		{"128Mi", 128 * 1024 * 1024},
		{"1Gi", 1024 * 1024 * 1024},
		{"256Ki", 256 * 1024},
		{"2Ti", 2 * 1024 * 1024 * 1024 * 1024},
		{"1000M", 1000_000_000},
		{"500K", 500_000},
		{"1048576", 1048576},
		{"", 0},
		{"garbage", 0},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("mem=%q", tc.input), func(t *testing.T) {
			got := parseMemoryToBytes(tc.input)
			if got != tc.want {
				t.Errorf("parseMemoryToBytes(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// InstanceUsageService tests
// ---------------------------------------------------------------------------

func TestGetCurrentUsage_ReturnsLatest(t *testing.T) {
	cpu := 45.0
	repo := &stubInstanceUsageRepository{
		records: []models.InstanceUsage{
			{ID: 1, InstanceID: 10, CPUUsagePercent: &cpu, RecordedAt: time.Now().Add(-time.Hour)},
		},
	}
	svc := NewInstanceUsageService(repo)

	usage, err := svc.GetCurrentUsage(10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if usage == nil || usage.ID != 1 {
		t.Fatalf("expected record ID 1, got %v", usage)
	}
}

func TestGetCurrentUsage_InvalidID(t *testing.T) {
	svc := NewInstanceUsageService(&stubInstanceUsageRepository{})
	_, err := svc.GetCurrentUsage(0)
	if err == nil {
		t.Fatal("expected error for invalid ID")
	}
}

func TestGetCurrentUsage_NoData(t *testing.T) {
	svc := NewInstanceUsageService(&stubInstanceUsageRepository{})
	usage, err := svc.GetCurrentUsage(99)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if usage != nil {
		t.Fatalf("expected nil for missing data, got %v", usage)
	}
}

func TestGetHistory_DefaultsTo24Hours(t *testing.T) {
	now := time.Now()
	cpu := 10.0
	repo := &stubInstanceUsageRepository{
		records: []models.InstanceUsage{
			{ID: 1, InstanceID: 5, CPUUsagePercent: &cpu, RecordedAt: now.Add(-12 * time.Hour)},
			{ID: 2, InstanceID: 5, CPUUsagePercent: &cpu, RecordedAt: now.Add(-48 * time.Hour)},
		},
	}
	svc := NewInstanceUsageService(repo)

	records, err := svc.GetHistory(5, 0) // 0 → defaults to 24h
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record within 24h, got %d", len(records))
	}
}

func TestGetHistory_CapsAt720Hours(t *testing.T) {
	svc := NewInstanceUsageService(&stubInstanceUsageRepository{})
	// Should not error even with huge hours value
	_, err := svc.GetHistory(1, 9999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetAllCurrentUsage_MultipleInstances(t *testing.T) {
	now := time.Now()
	cpu1, cpu2 := 30.0, 60.0
	repo := &stubInstanceUsageRepository{
		records: []models.InstanceUsage{
			{ID: 1, InstanceID: 1, CPUUsagePercent: &cpu1, RecordedAt: now.Add(-2 * time.Hour)},
			{ID: 2, InstanceID: 1, CPUUsagePercent: &cpu2, RecordedAt: now},
			{ID: 3, InstanceID: 2, CPUUsagePercent: &cpu1, RecordedAt: now},
		},
	}
	svc := NewInstanceUsageService(repo)

	records, err := svc.GetAllCurrentUsage()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 latest records (one per instance), got %d", len(records))
	}
}

func TestRecordUsage_ValidRecord(t *testing.T) {
	repo := &stubInstanceUsageRepository{}
	svc := NewInstanceUsageService(repo)

	cpu := 50.0
	err := svc.RecordUsage(&models.InstanceUsage{
		InstanceID:      1,
		CPUUsagePercent: &cpu,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.created) != 1 {
		t.Fatalf("expected 1 created record, got %d", len(repo.created))
	}
}

func TestRecordUsage_NilRecord(t *testing.T) {
	svc := NewInstanceUsageService(&stubInstanceUsageRepository{})
	if err := svc.RecordUsage(nil); err == nil {
		t.Fatal("expected error for nil record")
	}
}

func TestRecordUsage_InvalidInstanceID(t *testing.T) {
	svc := NewInstanceUsageService(&stubInstanceUsageRepository{})
	if err := svc.RecordUsage(&models.InstanceUsage{InstanceID: 0}); err == nil {
		t.Fatal("expected error for zero instance ID")
	}
}


