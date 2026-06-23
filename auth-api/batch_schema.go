package main

import (
	"context"
	"database/sql"
	"fmt"
)

func ensureBatchSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS batch_query_jobs (
			id BIGINT PRIMARY KEY AUTO_INCREMENT COMMENT '批量查询任务主键ID',
			hospital_code VARCHAR(64) NOT NULL COMMENT '任务所属医院编号',
			created_by VARCHAR(64) NOT NULL COMMENT '任务创建人登录账号',
			file_name VARCHAR(255) NOT NULL COMMENT '导入的原始文件名称',
			query_method VARCHAR(20) NOT NULL DEFAULT 'legacy' COMMENT '查询方式：legacy原接口、new新接口',
			status VARCHAR(20) NOT NULL DEFAULT 'pending' COMMENT '任务状态',
			total_count INT NOT NULL DEFAULT 0 COMMENT '导入数据总数',
			pending_count INT NOT NULL DEFAULT 0 COMMENT '待查询数据数量',
			success_count INT NOT NULL DEFAULT 0 COMMENT '查询成功数据数量',
			not_found_count INT NOT NULL DEFAULT 0 COMMENT '业务系统查无此人的数据数量',
			failed_count INT NOT NULL DEFAULT 0 COMMENT '查询失败数据数量',
			worker_count INT NOT NULL DEFAULT 5 COMMENT '业务接口并发查询Worker数量',
			fetch_batch_size INT NOT NULL DEFAULT 500 COMMENT '每次从数据库领取的任务数量',
			write_batch_size INT NOT NULL DEFAULT 200 COMMENT '每次批量写入查询结果的数量',
			error_message VARCHAR(1000) DEFAULT NULL COMMENT '任务级执行错误信息',
			started_at DATETIME NULL COMMENT '任务开始查询时间',
			completed_at DATETIME NULL COMMENT '任务完成或停止时间',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '任务创建时间',
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '任务最后更新时间',
			KEY idx_hospital_created (hospital_code, created_at),
			KEY idx_status (status)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='居民档案批量查询任务表'`,
		`CREATE TABLE IF NOT EXISTS batch_query_items (
			id BIGINT PRIMARY KEY AUTO_INCREMENT COMMENT '批量查询明细主键ID',
			job_id BIGINT NOT NULL COMMENT '所属批量查询任务ID',
			source_row INT NOT NULL COMMENT '身份证在Excel中的原始行号',
			id_card VARCHAR(32) NOT NULL COMMENT '待查询居民身份证号码',
			status VARCHAR(20) NOT NULL DEFAULT 'pending' COMMENT '明细状态',
			retry_count INT NOT NULL DEFAULT 0 COMMENT '当前明细已重试次数',
			error_message VARCHAR(1000) DEFAULT NULL COMMENT '查询失败原因或异常信息',
			claimed_at DATETIME NULL COMMENT '明细被Worker领取的时间',
			started_at DATETIME NULL COMMENT '明细开始调用业务接口的时间',
			completed_at DATETIME NULL COMMENT '明细查询完成时间',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '明细导入时间',
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '明细最后更新时间',
			UNIQUE KEY uk_job_row (job_id, source_row),
			KEY idx_job_status_id (job_id, status, id),
			KEY idx_job_id_card (job_id, id_card),
			CONSTRAINT fk_batch_item_job FOREIGN KEY (job_id) REFERENCES batch_query_jobs(id) ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='居民档案批量查询身份证明细表'`,
		`CREATE TABLE IF NOT EXISTS batch_query_results (
			id BIGINT PRIMARY KEY AUTO_INCREMENT COMMENT '查询结果主键ID',
			job_id BIGINT NOT NULL COMMENT '所属批量查询任务ID',
			item_id BIGINT NOT NULL COMMENT '对应的身份证查询明细ID',
			id_card VARCHAR(32) NOT NULL COMMENT '居民身份证号码',
			person_name VARCHAR(100) DEFAULT NULL COMMENT '业务接口返回的居民姓名',
			record_index INT NOT NULL DEFAULT 1 COMMENT '同一居民调阅记录序号',
			view_time VARCHAR(32) DEFAULT NULL COMMENT '档案调阅时间',
			view_org_name VARCHAR(255) DEFAULT NULL COMMENT '档案调阅机构名称',
			department VARCHAR(255) DEFAULT NULL COMMENT '档案调阅科室名称',
			view_user_name VARCHAR(100) DEFAULT NULL COMMENT '档案调阅人姓名',
			access_channel VARCHAR(100) DEFAULT NULL COMMENT '档案调阅渠道',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '查询结果写入时间',
			KEY idx_job_item (job_id, item_id),
			KEY idx_job_id_card (job_id, id_card),
			CONSTRAINT fk_batch_result_job FOREIGN KEY (job_id) REFERENCES batch_query_jobs(id) ON DELETE CASCADE,
			CONSTRAINT fk_batch_result_item FOREIGN KEY (item_id) REFERENCES batch_query_items(id) ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='居民档案批量查询调阅记录结果表'`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}

	exists, err := batchColumnExists(ctx, db, "batch_query_jobs", "not_found_count")
	if err != nil {
		return err
	}
	if !exists {
		if _, err := db.ExecContext(ctx, `
			ALTER TABLE batch_query_jobs
			ADD COLUMN not_found_count INT NOT NULL DEFAULT 0
			COMMENT '业务系统查无此人的数据数量'
			AFTER success_count
		`); err != nil {
			return fmt.Errorf("升级批量任务表失败: %w", err)
		}
	}

	exists, err = batchColumnExists(ctx, db, "batch_query_jobs", "query_method")
	if err != nil {
		return err
	}
	if !exists {
		if _, err := db.ExecContext(ctx, `
			ALTER TABLE batch_query_jobs
			ADD COLUMN query_method VARCHAR(20) NOT NULL DEFAULT 'legacy'
			COMMENT '查询方式：legacy原接口、new新接口'
			AFTER file_name
		`); err != nil {
			return fmt.Errorf("升级批量任务查询方式字段失败: %w", err)
		}
	}
	return nil
}

func batchColumnExists(ctx context.Context, db *sql.DB, tableName, columnName string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE()
			AND TABLE_NAME = ?
			AND COLUMN_NAME = ?
	`, tableName, columnName).Scan(&count)
	return count > 0, err
}
