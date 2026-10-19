// Package store 负责指标历史的 SQLite 持久化与三级降采样。
// 分层：samples_raw（10s 粒度，保留 2h）→ samples_1m（7d）→ samples_10m（90d）。
package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/glebarez/sqlite" // 纯 Go SQLite 驱动，注册名 "sqlite"
)

// 保留策略
const (
	RawRetention = 2 * time.Hour
	M1Retention  = 7 * 24 * time.Hour
	M10Retention = 90 * 24 * time.Hour
)

const schema = `
CREATE TABLE IF NOT EXISTS samples_raw (
  ts INTEGER NOT NULL, metric TEXT NOT NULL, value REAL NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_raw_metric_ts ON samples_raw(metric, ts);
CREATE TABLE IF NOT EXISTS samples_1m (
  ts INTEGER NOT NULL, metric TEXT NOT NULL, avg REAL NOT NULL, max REAL NOT NULL, min REAL NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_1m_metric_ts ON samples_1m(metric, ts);
CREATE TABLE IF NOT EXISTS samples_10m (
  ts INTEGER NOT NULL, metric TEXT NOT NULL, avg REAL NOT NULL, max REAL NOT NULL, min REAL NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_10m_metric_ts ON samples_10m(metric, ts);
CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value INTEGER NOT NULL);
`

// Store 指标历史存储。
type Store struct {
	db *sql.DB
}

// Open 打开（不存在则创建）数据库，启用 WAL 与 busy_timeout。
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite 串行写，亦保证 :memory: 测试一致
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("设置 pragma 失败: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("建表失败: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// InsertBatch 单事务写入一个采集周期的全部指标。
func (s *Store) InsertBatch(ts int64, m map[string]float64) error {
	if len(m) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO samples_raw(ts, metric, value) VALUES(?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()
	for k, v := range m {
		if _, err := stmt.Exec(ts, k, v); err != nil {
			tx.Rollback()
			return fmt.Errorf("写入指标 %s 失败: %w", k, err)
		}
	}
	return tx.Commit()
}

// Point 一个时序点（raw 层 avg=max=min=value）。
type Point struct {
	Ts         int64   `json:"ts"`
	Avg        float64 `json:"avg"`
	Max        float64 `json:"max"`
	Min        float64 `json:"min"`
}

// Query 按层查询历史。layer 取 "raw" | "1m" | "10m"。
func (s *Store) Query(metric string, from, to int64, layer string) ([]Point, error) {
	var rows *sql.Rows
	var err error
	switch layer {
	case "raw":
		rows, err = s.db.Query(
			`SELECT ts, value, value, value FROM samples_raw WHERE metric=? AND ts>=? AND ts<=? ORDER BY ts`,
			metric, from, to)
	case "1m":
		rows, err = s.db.Query(
			`SELECT ts, avg, max, min FROM samples_1m WHERE metric=? AND ts>=? AND ts<=? ORDER BY ts`,
			metric, from, to)
	case "10m":
		rows, err = s.db.Query(
			`SELECT ts, avg, max, min FROM samples_10m WHERE metric=? AND ts>=? AND ts<=? ORDER BY ts`,
			metric, from, to)
	default:
		return nil, fmt.Errorf("未知降采样层: %q", layer)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Point{}
	for rows.Next() {
		var p Point
		if err := rows.Scan(&p.Ts, &p.Avg, &p.Max, &p.Min); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Downsample 执行一轮降采样与过期清理（幂等，可周期调用）。
// 聚合窗口用 meta 表游标推进，重复调用不产生重复数据。
func (s *Store) Downsample(now time.Time) error {
	nowUnix := now.Unix()

	// raw → 1m：只聚合已完整结束的一分钟桶（截止于上一分钟起点）
	cut1 := nowUnix - nowUnix%60 - 60
	if err := s.aggregate("samples_raw", "samples_1m", 60, "agg1m_upto", cut1); err != nil {
		return err
	}
	// 1m → 10m
	cut10 := nowUnix - nowUnix%600 - 600
	if err := s.aggregate("samples_1m", "samples_10m", 600, "agg10m_upto", cut10); err != nil {
		return err
	}

	// 过期清理
	prune := []struct {
		table string
		older int64
	}{
		{"samples_raw", nowUnix - int64(RawRetention.Seconds())},
		{"samples_1m", nowUnix - int64(M1Retention.Seconds())},
		{"samples_10m", nowUnix - int64(M10Retention.Seconds())},
	}
	for _, p := range prune {
		if _, err := s.db.Exec(fmt.Sprintf(`DELETE FROM %s WHERE ts < ?`, p.table), p.older); err != nil {
			return fmt.Errorf("清理 %s 失败: %w", p.table, err)
		}
	}
	return nil
}

// aggregate 把 src 表 [cursor, cutoff) 区间的数据按 bucket 秒聚合进 dst。
func (s *Store) aggregate(src, dst string, bucket int64, metaKey string, cutoff int64) error {
	cursor, err := s.getMeta(metaKey)
	if err != nil {
		return err
	}
	if cursor >= cutoff {
		return nil // 没有新桶可聚合
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	// raw 层列名是 value，降采样层是 avg/max/min
	avgCol, maxCol, minCol := "avg", "max", "min"
	if src == "samples_raw" {
		avgCol, maxCol, minCol = "value", "value", "value"
	}
	// 防重复：先删除目标区间内已有聚合行
	if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM %s WHERE ts >= ? AND ts < ?`, dst), cursor, cutoff); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`INSERT INTO %s(ts, metric, avg, max, min)
		 SELECT (ts/%d)*%d, metric, AVG(%s), MAX(%s), MIN(%s)
		 FROM %s WHERE ts >= ? AND ts < ? GROUP BY metric, (ts/%d)*%d`,
		dst, bucket, bucket, avgCol, maxCol, minCol, src, bucket, bucket),
		cursor, cutoff); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.Exec(
		`INSERT INTO meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		metaKey, cutoff); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) getMeta(key string) (int64, error) {
	var v int64
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return v, err
}
