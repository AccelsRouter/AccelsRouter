package model

// UserSpendRow is one user's consumption in quota units over the three windows
// the abnormal usage alert compares.
type UserSpendRow struct {
	UserID   int
	Daily    float64 // from todayStart
	Monthly  float64 // from monthStart, today included
	Baseline float64 // from baselineStart up to todayStart
}

// QueryUserSpend sums the hourly quota_data per user. Only users with
// consumption since todayStart or earlier this month are returned, which keeps
// the result to the users that have been active. The three times are unix
// seconds; quota_data buckets are whole hours, so a UTC midnight is a bucket edge.
//
// It uses CASE WHEN and plain SUM, which SQLite, MySQL and PostgreSQL all run.
func QueryUserSpend(todayStart, monthStart, baselineStart int64) ([]UserSpendRow, error) {
	var rows []UserSpendRow
	err := DB.Model(&QuotaData{}).
		Select("user_id AS user_id, "+
			"COALESCE(SUM(CASE WHEN created_at >= ? THEN quota ELSE 0 END), 0) AS daily, "+
			"COALESCE(SUM(CASE WHEN created_at >= ? THEN quota ELSE 0 END), 0) AS monthly, "+
			"COALESCE(SUM(CASE WHEN created_at >= ? AND created_at < ? THEN quota ELSE 0 END), 0) AS baseline",
			todayStart, monthStart, baselineStart, todayStart).
		Where("created_at >= ?", min(monthStart, baselineStart)).
		Group("user_id").
		Having("SUM(CASE WHEN created_at >= ? THEN quota ELSE 0 END) > 0", monthStart).
		Scan(&rows).Error
	return rows, err
}
