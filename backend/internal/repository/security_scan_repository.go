package repository

import (
	"fmt"
	"time"

	"clawreef/internal/models"

	"github.com/upper/db/v4"
)

type SecurityScanRepository interface {
	GetConfig() (*models.SecurityScanConfig, error)
	UpsertConfig(config *models.SecurityScanConfig) error
	CreateJob(job *models.SecurityScanJob) error
	UpdateJob(job *models.SecurityScanJob) error
	GetJobByID(id int) (*models.SecurityScanJob, error)
	ListJobs(limit int) ([]models.SecurityScanJob, error)
	CreateJobItem(item *models.SecurityScanJobItem) error
	UpdateJobItem(item *models.SecurityScanJobItem) error
	ListJobItems(jobID int) ([]models.SecurityScanJobItem, error)
	UpsertReport(report *models.SecurityScanReport) error
	GetReportByJobID(jobID int) (*models.SecurityScanReport, error)
}

type securityScanRepository struct {
	sess db.Session
}

func NewSecurityScanRepository(sess db.Session) SecurityScanRepository {
	repo := &securityScanRepository{sess: sess}
	return repo
}

func (r *securityScanRepository) GetConfig() (*models.SecurityScanConfig, error) {
	var item models.SecurityScanConfig
	err := r.sess.Collection("security_scan_configs").Find(db.Cond{"id": 1}).One(&item)
	if err == db.ErrNoMoreRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get security scan config: %w", err)
	}
	return &item, nil
}

func (r *securityScanRepository) UpsertConfig(config *models.SecurityScanConfig) error {
	existing, err := r.GetConfig()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if existing == nil {
		config.ID = 1
		config.CreatedAt = now
		config.UpdatedAt = now
		if _, err := r.sess.Collection("security_scan_configs").Insert(config); err != nil {
			return fmt.Errorf("failed to create security scan config: %w", err)
		}
		return nil
	}
	config.ID = existing.ID
	config.CreatedAt = existing.CreatedAt
	config.UpdatedAt = now
	if err := r.sess.Collection("security_scan_configs").Find(db.Cond{"id": existing.ID}).Update(config); err != nil {
		return fmt.Errorf("failed to update security scan config: %w", err)
	}
	return nil
}

func (r *securityScanRepository) CreateJob(job *models.SecurityScanJob) error {
	ensureTimestamps(&job.CreatedAt, &job.UpdatedAt)
	res, err := r.sess.Collection("security_scan_jobs").Insert(job)
	if err != nil {
		return fmt.Errorf("failed to create security scan job: %w", err)
	}
	if id, ok := res.ID().(int64); ok {
		job.ID = int(id)
	}
	return nil
}

func (r *securityScanRepository) UpdateJob(job *models.SecurityScanJob) error {
	if job.UpdatedAt.IsZero() {
		job.UpdatedAt = time.Now().UTC()
	}
	if err := r.sess.Collection("security_scan_jobs").Find(db.Cond{"id": job.ID}).Update(job); err != nil {
		return fmt.Errorf("failed to update security scan job: %w", err)
	}
	return nil
}

func (r *securityScanRepository) GetJobByID(id int) (*models.SecurityScanJob, error) {
	var item models.SecurityScanJob
	err := r.sess.Collection("security_scan_jobs").Find(db.Cond{"id": id}).One(&item)
	if err == db.ErrNoMoreRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get security scan job: %w", err)
	}
	return &item, nil
}

func (r *securityScanRepository) ListJobs(limit int) ([]models.SecurityScanJob, error) {
	if limit <= 0 {
		limit = 20
	}
	var items []models.SecurityScanJob
	if err := r.sess.Collection("security_scan_jobs").Find().OrderBy("-created_at", "-id").Limit(limit).All(&items); err != nil {
		return nil, fmt.Errorf("failed to list security scan jobs: %w", err)
	}
	return items, nil
}

func (r *securityScanRepository) CreateJobItem(item *models.SecurityScanJobItem) error {
	ensureTimestamps(&item.CreatedAt, &item.UpdatedAt)
	res, err := r.sess.Collection("security_scan_job_items").Insert(item)
	if err != nil {
		return fmt.Errorf("failed to create security scan job item: %w", err)
	}
	if id, ok := res.ID().(int64); ok {
		item.ID = int(id)
	}
	return nil
}

func (r *securityScanRepository) UpdateJobItem(item *models.SecurityScanJobItem) error {
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = time.Now().UTC()
	}
	if err := r.sess.Collection("security_scan_job_items").Find(db.Cond{"id": item.ID}).Update(item); err != nil {
		return fmt.Errorf("failed to update security scan job item: %w", err)
	}
	return nil
}

func (r *securityScanRepository) ListJobItems(jobID int) ([]models.SecurityScanJobItem, error) {
	var items []models.SecurityScanJobItem
	if err := r.sess.Collection("security_scan_job_items").Find(db.Cond{"job_id": jobID}).OrderBy("id").All(&items); err != nil {
		return nil, fmt.Errorf("failed to list security scan job items: %w", err)
	}
	return items, nil
}

func (r *securityScanRepository) UpsertReport(report *models.SecurityScanReport) error {
	var existing models.SecurityScanReport
	err := r.sess.Collection("security_scan_reports").Find(db.Cond{"job_id": report.JobID}).One(&existing)
	if err == db.ErrNoMoreRows {
		ensureTimestamps(&report.CreatedAt, &report.UpdatedAt)
		res, err := r.sess.Collection("security_scan_reports").Insert(report)
		if err != nil {
			return fmt.Errorf("failed to create security scan report: %w", err)
		}
		if id, ok := res.ID().(int64); ok {
			report.ID = int(id)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get security scan report: %w", err)
	}
	report.ID = existing.ID
	report.CreatedAt = existing.CreatedAt
	report.UpdatedAt = time.Now().UTC()
	if err := r.sess.Collection("security_scan_reports").Find(db.Cond{"id": existing.ID}).Update(report); err != nil {
		return fmt.Errorf("failed to update security scan report: %w", err)
	}
	return nil
}

func (r *securityScanRepository) GetReportByJobID(jobID int) (*models.SecurityScanReport, error) {
	var item models.SecurityScanReport
	err := r.sess.Collection("security_scan_reports").Find(db.Cond{"job_id": jobID}).One(&item)
	if err == db.ErrNoMoreRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get security scan report: %w", err)
	}
	return &item, nil
}
