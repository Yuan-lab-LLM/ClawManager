package repository

import "clawreef/internal/models"

// Aggregate directly in SQL: never derive console totals from a paginated list.
func (r *instanceRepository) SummarizeAll() (*models.InstanceSummary, error) {
	summary := &models.InstanceSummary{}
	row, err := r.sess.SQL().QueryRow(`SELECT COUNT(*), COALESCE(SUM(status = 'running'),0), COALESCE(SUM(disk_gb),0) FROM instances`)
	if err != nil {
		return nil, err
	}
	err = row.Scan(&summary.Total, &summary.Running, &summary.AllocatedStorageGB)
	return summary, err
}
