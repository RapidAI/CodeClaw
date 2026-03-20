package skillmarket

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ── SubmissionRepository implementation ─────────────────────────────────

func (s *Store) CreateSubmission(ctx context.Context, sub *SkillSubmission) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sm_submissions (id, email, user_id, skill_id, fingerprint, status, zip_path, error_msg, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sub.ID, sub.Email, sub.UserID, sub.SkillID, sub.Fingerprint,
		sub.Status, sub.ZipPath, sub.ErrorMsg,
		fmtTime(sub.CreatedAt), fmtTime(sub.UpdatedAt),
	)
	return err
}

func (s *Store) GetSubmissionByID(ctx context.Context, id string) (*SkillSubmission, error) {
	row := s.readDB.QueryRowContext(ctx, `
		SELECT id, email, user_id, skill_id, fingerprint, status, zip_path, error_msg, created_at, updated_at
		FROM sm_submissions WHERE id = ?`, id)

	var sub SkillSubmission
	var createdAt, updatedAt string
	err := row.Scan(
		&sub.ID, &sub.Email, &sub.UserID, &sub.SkillID, &sub.Fingerprint,
		&sub.Status, &sub.ZipPath, &sub.ErrorMsg, &createdAt, &updatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	sub.CreatedAt = parseTime(createdAt)
	sub.UpdatedAt = parseTime(updatedAt)
	return &sub, nil
}

func (s *Store) UpdateSubmissionStatus(ctx context.Context, id, status, errorMsg, skillID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE sm_submissions SET status = ?, error_msg = ?, skill_id = ?, updated_at = ?
		WHERE id = ?`, status, errorMsg, skillID, time.Now().Format(timeFmt), id)
	return err
}

// CountRecentSubmissions 统计指定 email 在最近 withinHours 小时内的有效提交数（排除 failed）。
func (s *Store) CountRecentSubmissions(ctx context.Context, email string, withinHours int) (int, error) {
	since := time.Now().Add(-time.Duration(withinHours) * time.Hour).Format(timeFmt)
	var count int
	err := s.readDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sm_submissions
		WHERE email = ? AND created_at >= ? AND status IN ('pending','processing','success')`,
		email, since,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count recent submissions: %w", err)
	}
	return count, nil
}

// CountRecentDailySubmissions 统计指定 email 在今天（UTC）的有效提交数。
func (s *Store) CountRecentDailySubmissions(ctx context.Context, email string) (int, error) {
	today := time.Now().UTC().Truncate(24 * time.Hour).Format(timeFmt)
	var count int
	err := s.readDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sm_submissions
		WHERE email = ? AND created_at >= ? AND status IN ('pending','processing','success')`,
		email, today,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count daily submissions: %w", err)
	}
	return count, nil
}
