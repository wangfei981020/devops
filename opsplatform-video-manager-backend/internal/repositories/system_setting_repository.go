// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package repositories

import (
	"context"

	"github.com/video-manager/backend/pkg/database"
)

type SystemSettingRepository struct{}

func NewSystemSettingRepository() *SystemSettingRepository {
	return &SystemSettingRepository{}
}

func (r *SystemSettingRepository) Upsert(ctx context.Context, key, value string) error {
	query := `
		INSERT INTO system_settings (key, value, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (key)
		DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`
	_, err := database.DB.Exec(ctx, query, key, value)
	return err
}

func (r *SystemSettingRepository) Delete(ctx context.Context, key string) error {
	_, err := database.DB.Exec(ctx, `DELETE FROM system_settings WHERE key = $1`, key)
	return err
}

func (r *SystemSettingRepository) GetByPrefix(ctx context.Context, prefix string) (map[string]string, error) {
	rows, err := database.DB.Query(ctx, `SELECT key, value FROM system_settings WHERE key LIKE $1`, prefix+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var key string
		var value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		result[key] = value
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
